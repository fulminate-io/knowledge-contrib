// SPDX-License-Identifier: Apache-2.0

// Package gcpclients is the PRODUCTION WIRING: it builds the provider's SDK
// clients from Application Default Credentials and adapts each one's own
// enumeration shape into the flat list the converters consume.
//
// IT HOLDS NO DOMAIN LOGIC, AND IT IS NOT GLUE EITHER. Every decision about what
// a resource becomes — its id, its type, its metadata, its edges — lives in a
// converter this package never calls. What lives HERE is the decision nobody
// else can take: what a PARTIAL answer from the provider means.
//
// THE PROVIDER REPORTS A PARTIAL ANSWER ON FIVE DIFFERENT CHANNELS, and every
// one of them is a way of saying "some of what you asked for is missing":
//
//   - a compute aggregated list marks the individual SCOPE with a warning;
//   - a compute flat list marks the PAGE with the same warning shape;
//   - a gapic list response carries an `Unreachable` list of locations;
//   - a REST list response carries `Unreachable`, `FailedLocation` or `Warnings`;
//   - a cluster list carries `MissingZones`.
//
// ALL FIVE MEAN THE SAME THING TO THE WALK. The module already models "I was not
// allowed to look" as an outcome distinct from "there is nothing there", because
// a walk that asserts COMPLETE lets the receiving server treat what the walk did
// not carry as gone. "I could not reach that zone" is the same fact arriving on
// a different channel, so it produces the same third outcome: the items that WERE
// read are kept, and the enumeration reports [collect.ErrPartial] naming the
// scopes, which makes the walk incomplete.
//
// The one thing here that genuinely cannot be tested offline is client
// CONSTRUCTION, which opens real connections. Everything else takes its input as
// a function parameter and is driven by this package's own tests.
package gcpclients

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// scopeSignal reports the scopes the provider said it could not read on the page
// the iterator is currently sitting on. A list whose response carries no such
// field passes nil.
type scopeSignal func() []string

// unread accumulates the scopes a provider reported as unread, deduped and
// ordered, so two runs of an unchanged project produce the same reason.
type unread struct {
	seen map[string]bool
	all  []string
}

func (u *unread) add(scopes []string) {
	for _, scope := range scopes {
		if scope == "" {
			continue
		}
		if u.seen == nil {
			u.seen = map[string]bool{}
		}
		if u.seen[scope] {
			continue
		}
		u.seen[scope] = true
		u.all = append(u.all, scope)
	}
}

func (u *unread) empty() bool { return len(u.all) == 0 }

// err builds the partial-read error, or nil when nothing was unread.
func (u *unread) err() error {
	if u.empty() {
		return nil
	}
	scopes := slices.Clone(u.all)
	slices.Sort(scopes)
	return fmt.Errorf("%w: the provider did not read %s",
		collect.ErrPartial, strings.Join(scopes, ", "))
}

// join folds a partial read into whatever else went wrong.
//
// A HARD FAILURE WINS BUT DOES NOT ERASE THE PARTIAL SCOPES: the failure is the
// stronger statement, and the scopes seen before it are still facts about what
// this run did not see.
func (u *unread) join(err error) error {
	partial := u.err()
	switch {
	case err == nil:
		return partial
	case partial == nil:
		return err
	default:
		return fmt.Errorf("%w (and %w)", err, partial)
	}
}

// drain reads a paginated iterator to completion, accumulating the partial-read
// signal each page carries.
//
// THE PARTIAL RESULT IS RETURNED BESIDE THE ERROR in every arm, and the caller
// decides. An enumeration commonly denies or loses ONE scope out of dozens, and
// a drain that threw away the pages it had already read would turn a small loss
// into an empty enumeration.
func drain[T any](next func() (T, error), signal scopeSignal) ([]T, error) {
	var (
		out    []T
		unread unread
	)
	for {
		item, err := next()
		if signal != nil {
			// Read AFTER the call, because the iterator updates its page
			// response as part of fetching, and read on the terminating call too
			// so a signal carried by the last page is not lost.
			unread.add(signal())
		}
		if errors.Is(err, iterator.Done) {
			return out, unread.err()
		}
		if err != nil {
			return out, unread.join(err)
		}
		out = append(out, item)
	}
}

// drainScoped reads an AGGREGATED iterator, which yields one entry per zone or
// region rather than one per resource, and flattens it.
//
// A SCOPE THE PROVIDER COULD NOT READ IS REPORTED, NOT SKIPPED. This is where
// the first version of this package was wrong: it skipped the scope and returned
// a clean nil, so the walk asserted a complete enumeration having never seen that
// zone, and the next full-replace collect deleted everything in it. The items
// from every OTHER scope are still returned, because one unreachable zone must
// not cost the rest of the project.
func drainScoped[Pair any, T any](
	next func() (Pair, error),
	scopeOf func(Pair) (items []T, unreadScope string),
) ([]T, error) {
	var (
		out    []T
		unread unread
	)
	for {
		pair, err := next()
		if errors.Is(err, iterator.Done) {
			return out, unread.err()
		}
		if err != nil {
			return out, unread.join(err)
		}
		items, unreadScope := scopeOf(pair)
		unread.add([]string{unreadScope})
		out = append(out, items...)
	}
}

// The two compute warning codes that mean part of the answer is missing, quoted
// from the provider's own documentation of the code vocabulary:
//
//	UNREACHABLE      "A given scope cannot be reached."
//	PARTIAL_SUCCESS  "Success is reported, but some results may be missing due to errors"
//
// EVERY OTHER CODE IN THAT VOCABULARY IS ROUTINE — a deprecation notice, an empty
// page, a quota remark — and treating them as data loss would make the walk
// permanently incomplete and the signal worth nothing.
const (
	warningUnreachable    = "UNREACHABLE"
	warningPartialSuccess = "PARTIAL_SUCCESS"
)

// warningIsPartialRead reports whether a compute warning code says part of the
// answer is missing.
func warningIsPartialRead(code string) bool {
	return code == warningUnreachable || code == warningPartialSuccess
}

// computeWarningScopes reads the scopes a compute warning says were not read.
//
// The warning carries the scope as a key/value datum. Where it carries none, the
// CODE is reported instead: naming the loss vaguely is better than losing the
// fact to a missing label.
func computeWarningScopes(warning *computepb.Warning) []string {
	if warning == nil || !warningIsPartialRead(warning.GetCode()) {
		return nil
	}
	var scopes []string
	for _, datum := range warning.GetData() {
		if datum.GetKey() == "scope" && datum.GetValue() != "" {
			scopes = append(scopes, datum.GetValue())
		}
	}
	if len(scopes) == 0 {
		return []string{warning.GetCode()}
	}
	return scopes
}

// computeWarningSignal builds the page signal for a compute FLAT list, whose
// page response carries the same warning shape an aggregated list puts on each
// scope. The response is read through a closure because the iterator exposes it
// as a field rather than a method.
func computeWarningSignal[R interface{ GetWarning() *computepb.Warning }](response func() any) scopeSignal {
	return func() []string {
		resp, ok := response().(R)
		if !ok {
			// No page has been fetched yet, or the iterator returned a shape
			// this code does not know. Reporting nothing is right for the first
			// and is not a silent degrade for the second: a response type that
			// changed would fail to compile at the call site's type argument.
			return nil
		}
		return computeWarningScopes(resp.GetWarning())
	}
}

// unreachableSignal builds the page signal for a list whose response carries an
// explicit `Unreachable` list of locations, which is how every non-compute
// service in this collector reports the same fact.
func unreachableSignal[R interface{ GetUnreachable() []string }](response func() any) scopeSignal {
	return func() []string {
		resp, ok := response().(R)
		if !ok {
			return nil
		}
		return resp.GetUnreachable()
	}
}

// partialRead builds the error for a signal that arrives as a whole list at once
// rather than page by page, which is how the single-call and callback-paged
// reads report it.
func partialRead(scopes []string) error {
	var unread unread
	unread.add(scopes)
	return unread.err()
}

// closeAll closes every handle a walk opened, reporting the first failure. It is
// used from a deferred release, where an error can only be reported rather than
// acted on, so it collects rather than stopping at the first.
func closeAll(closers []func() error) error {
	var errs []error
	for _, closeFn := range closers {
		if closeFn == nil {
			continue
		}
		if err := closeFn(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("closing gcp clients: %w", errors.Join(errs...))
}
