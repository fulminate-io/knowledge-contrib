// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// walk.go — THE RUNNER: bounded fan-out over the subcollectors, then the
// in-walk resolvers, then one merged result.
//
// WHY BOUNDED AND NOT SERIAL. The walk makes one or more ARM list calls per
// Azure service and there are three dozen services; serially that is three
// dozen round-trip latencies in sequence for a subscription that may hold
// nothing at all. WHY BOUNDED AND NOT UNLIMITED: every call goes to one
// management endpoint under one subscription's throttling budget, and the SDK's
// own retryer already backs off on 429, so an unbounded fan-out converts a
// latency win into a throttling storm the retryer then serializes anyway.
//
// AN ERROR NEVER ABORTS THE WALK AND IS NEVER SWALLOWED. A subcollector that
// fails is recorded and its siblings run on; the walk then asserts an
// INCOMPLETE result naming every failure. That is the only honest outcome:
// dropping the failure would assert a complete walk over a subscription this
// run could not read, and the completeness assertion is what arms the server's
// deletion phase, so a partial walk claiming completeness would name every
// resource it failed to read as deleted.

// defaultConcurrency is how many subcollectors run at once. Ten is the degree
// the in-tree cloud runner settled on for the same shape of work against the
// same kind of throttled management API.
const defaultConcurrency = 10

// subCollector is one Azure service's slice of the walk.
type subCollector interface {
	// Name identifies this subcollector in a failure reason. It is stable and
	// operator-facing: a walk that reports itself incomplete names these.
	Name() string
	// Collect makes this service's list calls and returns what it found.
	Collect(ctx context.Context) (subResult, error)
}

// subResult is one subcollector's output.
type subResult struct {
	resources []resource
	edges     []edge
}

// walkOutcome is the merged output of one whole walk: every resource, every
// edge, and the subcollectors that failed.
type walkOutcome struct {
	resources []resource
	edges     []edge
	// failures is one entry per failed subcollector, in subcollector order, so
	// the reason string a second identical run produces is identical.
	failures []subFailure
}

// subFailure records one subcollector's failure. It carries the name as well as
// the error so the incomplete-walk reason names something an operator can act
// on rather than only an SDK message.
type subFailure struct {
	name string
	err  error
}

func (f subFailure) String() string { return f.name + ": " + f.err.Error() }

// runSubCollectors fans the subcollectors out at a bounded degree and merges
// their results IN SUBCOLLECTOR ORDER rather than in completion order, so two
// identical collects produce byte-identical node and edge sequences. A walk
// whose output order depended on which goroutine finished first would look like
// a changed subscription on every second collect.
func runSubCollectors(ctx context.Context, subs []subCollector, concurrency int) walkOutcome {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	results := make([]subResult, len(subs))
	errs := make([]error, len(subs))

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, s := range subs {
		wg.Add(1)
		go func(idx int, sc subCollector) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// The context is checked HERE rather than only before launch: a
			// cancellation that lands while this goroutine waited on the
			// semaphore should not start a fresh round of ARM calls.
			if err := ctx.Err(); err != nil {
				errs[idx] = err
				return
			}
			res, err := sc.Collect(ctx)
			results[idx] = res
			errs[idx] = err
		}(i, s)
	}
	wg.Wait()

	var out walkOutcome
	for i, s := range subs {
		if errs[i] != nil {
			out.failures = append(out.failures, subFailure{name: s.Name(), err: errs[i]})
			// The partial result is KEPT. A subcollector that paged three pages
			// and failed on the fourth found three pages of real resources, and
			// discarding them would lose data the run already paid for; the
			// incomplete assertion is what keeps that honest.
			slog.Warn("azure collector: subcollector failed", "subcollector", s.Name(), "error", errs[i])
		}
		out.resources = append(out.resources, results[i].resources...)
		out.edges = append(out.edges, results[i].edges...)
	}
	return out
}

// incompleteReason renders the walk's failures as the reason an incomplete
// assertion carries. Failures are already in subcollector order, so the string
// is deterministic.
func (o walkOutcome) incompleteReason() string {
	if len(o.failures) == 0 {
		return ""
	}
	parts := make([]string, 0, len(o.failures))
	for _, f := range o.failures {
		parts = append(parts, f.String())
	}
	return fmt.Sprintf("%d of the subscription's subcollectors failed and their resources are missing from this walk: %s",
		len(o.failures), joinSemicolons(parts))
}

func joinSemicolons(parts []string) string {
	var out strings.Builder
	for i, p := range parts {
		if i > 0 {
			out.WriteString("; ")
		}
		out.WriteString(p)
	}
	return out.String()
}

// dedupeResources collapses resources emitted more than once to the FIRST
// emission and returns them in emission order.
//
// WHY DUPLICATES EXIST AT ALL: two subcollectors legitimately reach the same
// proxy. A key vault named only by a function app's setting and by an app
// service's setting is one proxy discovered twice, and each subcollector
// dedupes only within itself because they run concurrently and share nothing.
// First-emission-wins keeps the output stable under the deterministic merge
// order above.
func dedupeResources(in []resource) []resource {
	seen := make(map[string]struct{}, len(in))
	out := make([]resource, 0, len(in))
	for _, r := range in {
		if _, dup := seen[r.id]; dup {
			continue
		}
		seen[r.id] = struct{}{}
		out = append(out, r)
	}
	return out
}

// dedupeEdges collapses edges identical in all four identity fields — endpoints,
// relationship and evidence — to one. Two subcollectors reaching the same
// relationship from opposite ends is a real shape (a disk's BOUND_TO is emitted
// by the disk walk and by its VM's storage profile), and the same edge twice is
// noise rather than information.
//
// EVIDENCE IS PART OF THE KEY ON PURPOSE: two ASSUMES_ROLE edges between the
// same pair, one carrying principal_type and one carrying role_source, are
// DIFFERENT facts and both survive.
func dedupeEdges(in []edge) []edge {
	type key struct{ from, to, rel, ev, method string }
	seen := make(map[key]struct{}, len(in))
	out := make([]edge, 0, len(in))
	for _, e := range in {
		k := key{e.from, e.to, e.relation, encodeEvidence(e.metadata), e.method}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, e)
	}
	return out
}

// resourceIndex indexes resources by ARM resource type, for the resolvers. The
// slices keep emission order, so a resolver walking one is deterministic.
func resourceIndex(rs []resource) map[string][]resource {
	idx := make(map[string][]resource)
	for _, r := range rs {
		idx[r.resourceType] = append(idx[r.resourceType], r)
	}
	return idx
}
