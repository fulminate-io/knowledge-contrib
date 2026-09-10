// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// walk.go — THE FAN-OUT, and the completeness assertion it produces.
//
// One account walk is forty service walks, each paginating its own API to
// exhaustion. They are independent, so they run concurrently under a bounded
// semaphore rather than serially: serial would multiply forty round-trip
// latencies for no reason, and unbounded would open forty concurrent connection
// pools against one credential and invite throttling from the very API this is
// enumerating.
//
// AN ERRORING SERVICE DOES NOT ABORT THE ACCOUNT, and that is the whole reason
// walk_complete exists. A denied permission on one service, or a throttle that
// outlasts the SDK's retries, is a partial enumeration: the rest of the account
// is still worth carrying, and the result says so by asserting INCOMPLETE. What
// is never acceptable is carrying the partial result while asserting a complete
// walk, because the server's deletion phase then treats everything the failed
// service would have named as gone.
//
// STDOUT IS THE PROTOCOL STREAM when this collector is spawned over stdio, so
// every diagnostic below goes to stderr through slog's default handler.

// defaultConcurrency bounds how many service walks run at once. It matches the
// built-in collector's own default, which is the only calibration this project
// has against a real account.
const defaultConcurrency = 10

// serviceWalk is one subcollector: a name for diagnostics and the function that
// enumerates that service.
type serviceWalk struct {
	name string
	run  func(ctx context.Context, w *walkContext) error
}

// walkContext is what every service walk is handed. It carries the clients, the
// account identity the nodes are stamped with, and the sink they write to.
type walkContext struct {
	clients *Clients
	account string
	region  string
	sink    *sink
	// derived is the raw material the service walks record for the second pass:
	// the rules, peerings, trust policies and cluster issuers that no single walk
	// can turn into edges on its own. See derive.go.
	derived derived
	// subreads collects the failures of the PER-RESOURCE reads that hang below a
	// service walk. See subreadFailures.
	subreads subreadFailures
}

// subreadFailures is the failure set for the reads that happen BELOW a service
// walk, and it exists because those reads have no other way to reach the
// completeness verdict.
//
// THE PROBLEM IT SOLVES. A service walk returns one error for the whole service,
// which is the right granularity for "ELB could not be listed". But a walk that
// lists sixty balancers and then reads each one's listeners has sixty more reads
// under it, and failing the whole ELB walk because ONE balancer's listeners were
// denied would throw away fifty-nine balancers' worth of a good enumeration. The
// tempting third option — read what you can and say nothing — is the one that is
// not available: every registered custom family is on FULL-REPLACE deletion, so a
// walk that asserts COMPLETE tells the server every edge it did not name is gone.
// A denied listener read would then delete that balancer's certificate edges on
// the next collect, silently, with walk_complete asserting the account was fully
// enumerated.
//
// SO THE RESULT SHIPS PARTIAL AND SAYS SO. The sub-read records here, the walk
// keeps everything it did read, and the account's verdict is INCOMPLETE naming
// the operation that failed. Deletion is disabled for that collect, which is
// exactly the outcome the hazard calls for.
//
// IT IS AGGREGATED BY OPERATION, not by resource, and that is a deliberate bound
// on the DIAGNOSTIC rather than on the traffic. An account whose IAM policy
// denies DescribeListeners denies it for every balancer, so a per-resource list
// would put one line per balancer into a one-line reason string. Aggregating
// keeps the reason proportional to the number of distinct reads while still
// naming the read, the count and one concrete target to start from. Nothing here
// caps what the collector READS.
//
// THE ZERO VALUE IS READY TO USE, so a walkContext composed by a test needs no
// constructor and cannot forget one.
type subreadFailures struct {
	mu sync.Mutex
	// byOp is keyed by the operation name, e.g. "elbv2.DescribeListeners".
	byOp map[string]*subreadFailure
}

// subreadFailure is one operation's accumulated failure: how many of its reads
// failed, the first target that failed, and that target's error.
type subreadFailure struct {
	count      int
	firstTgt   string
	firstError error
}

// record notes that one per-resource read failed. A nil error is not a failure
// and is ignored, so a caller may hand its error in unconditionally.
//
// THE TARGET IS WHAT THE READ WAS ABOUT — a load balancer ARN, a domain name, a
// task definition — because "DescribeListeners failed" without one sends the
// operator to the console with nothing to look up.
func (w *walkContext) recordSubreadFailure(operation, target string, err error) {
	if err == nil {
		return
	}
	w.subreads.mu.Lock()
	defer w.subreads.mu.Unlock()
	if w.subreads.byOp == nil {
		w.subreads.byOp = map[string]*subreadFailure{}
	}
	f, ok := w.subreads.byOp[operation]
	if !ok {
		f = &subreadFailure{firstTgt: target, firstError: err}
		w.subreads.byOp[operation] = f
	}
	f.count++
}

// errors renders the recorded failures as one error per OPERATION, sorted by
// operation name so two runs that failed identically produce the same reason.
func (s *subreadFailures) errors() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byOp) == 0 {
		return nil
	}
	ops := make([]string, 0, len(s.byOp))
	for op := range s.byOp {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	out := make([]error, 0, len(ops))
	for _, op := range ops {
		f := s.byOp[op]
		out = append(out, fmt.Errorf("%s: %d per-resource read(s) failed, first on %s: %w",
			op, f.count, f.firstTgt, f.firstError))
	}
	return out
}

// runServiceWalks runs every walk under a bounded semaphore and returns the
// failures, in a STABLE order.
//
// THE ERRORS ARE SORTED BY SERVICE NAME because completion order is not
// reproducible, and the incomplete-walk reason is built from them: an unsorted
// reason string would differ between two runs that failed identically.
func runServiceWalks(ctx context.Context, w *walkContext, walks []serviceWalk) []error {
	sem := make(chan struct{}, defaultConcurrency)
	var (
		mu   sync.Mutex
		errs = map[string]error{}
		wg   sync.WaitGroup
	)
	for _, sw := range walks {
		// THE CONTEXT IS CHECKED BEFORE THE SEMAPHORE, so a cancelled collect
		// stops queueing work instead of running every remaining service walk to
		// completion and discarding it.
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(sw serviceWalk) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := sw.run(ctx, w); err != nil {
				slog.Warn("aws collector: service walk failed", "service", sw.name, "error", err)
				mu.Lock()
				errs[sw.name] = err
				mu.Unlock()
			}
		}(sw)
	}
	wg.Wait()

	names := make([]string, 0, len(errs))
	for name := range errs {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]error, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Errorf("%s: %w", name, errs[name]))
	}
	// THE PER-RESOURCE READS JOIN THE SAME FAILURE SET, which is the whole point
	// of recording them: a sub-read failure that did not reach this list would
	// leave the account asserting a COMPLETE enumeration it did not perform. They
	// are appended after the service failures and are internally sorted, so the
	// order is stable across runs.
	out = append(out, w.subreads.errors()...)
	// A CANCELLED COLLECT IS ALWAYS INCOMPLETE, even when every walk that got to
	// run succeeded: the walks that never started enumerated nothing, and
	// asserting a complete walk over them is the assertion this whole file
	// exists to prevent.
	if ctx.Err() != nil {
		out = append(out, fmt.Errorf("the collect was cancelled before every service was walked: %w", ctx.Err()))
	}
	return out
}

// incompleteReason renders the walk failures as the one-line reason an
// incomplete assertion carries. It names every failed read rather than a
// count, because the operator's next question is which one.
//
// "READS" RATHER THAN "SERVICE WALKS": the failures reaching here are of two
// kinds, a whole service that could not be listed and a per-resource read below
// one that could not be read, and both are reasons the enumeration is partial.
func incompleteReason(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of the account's reads failed: %v", len(errs), errors.Join(errs...))
}

// paginate drives a NextToken loop to exhaustion and calls visit for each page.
//
// EXHAUSTION IS THE POINT AND IT IS THE RECURRING DEFECT: an AWS list call
// returns one page and a token, and a walk that reads the first page reports a
// truncated account that looks exactly like a small one. Every list in this
// package goes through here so no service walk can quietly stop at page one.
//
// THE CONTEXT REACHES EVERY CALL, so a cancelled collect stops paginating
// instead of draining a large account nobody is waiting for. It is NOT retried
// here: the SDK's standard retryer already handles 429 and 5xx with backoff, and
// a second retry loop around it turns one throttled call into a much longer one.
func paginate[Page any](
	ctx context.Context,
	call func(ctx context.Context, token *string) (Page, error),
	nextToken func(Page) *string,
	visit func(Page) error,
) error {
	var token *string
	// A GUARD ON PAGE COUNT, not on time. An API that returned the same token
	// forever would otherwise spin here; the bound is far above any real
	// account's page count, so it can only fire on that defect.
	const maxPages = 10000
	for range maxPages {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := call(ctx, token)
		if err != nil {
			return err
		}
		if err := visit(page); err != nil {
			return err
		}
		next := nextToken(page)
		if next == nil || *next == "" {
			return nil
		}
		// A token identical to the one just sent is a non-advancing API, which
		// would otherwise be an infinite loop with a plausible-looking result.
		if token != nil && *token == *next {
			return fmt.Errorf("pagination did not advance: the service returned the same continuation token twice")
		}
		token = next
	}
	return fmt.Errorf("pagination did not terminate after %d pages", maxPages)
}
