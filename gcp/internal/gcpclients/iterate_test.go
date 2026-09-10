// SPDX-License-Identifier: Apache-2.0

package gcpclients

import (
	"errors"
	"strings"
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// iterate_test.go — THE DRAINING SEAM, which is where this module decides what a
// PARTIAL answer from the provider means.
//
// The package was shipped untested on the ground that what it holds is "a loop
// that copies an iterator into a slice". That was wrong, and the two defects it
// hid say why: the loop also decides whether a scope the provider could not read
// makes the walk incomplete, and whether a detail read that failed is an error.
// Neither decision needs a credential or a network to test — every function here
// takes its input as a function parameter.
//
// The tests are IN-PACKAGE because the draining helpers are unexported. What is
// genuinely untestable offline is only the client construction, which opens real
// connections.

// pageIter is a canned iterator: it yields its items and then reports done, and
// it can carry a page signal that changes as pages turn.
type pageIter[T any] struct {
	items   []T
	err     error // returned instead of Done, after the items run out
	at      int
	pages   [][]string // the partial-read signal visible at each item index
	current []string
}

func (p *pageIter[T]) next() (T, error) {
	var zero T
	if p.at < len(p.items) {
		if p.at < len(p.pages) {
			p.current = p.pages[p.at]
		}
		item := p.items[p.at]
		p.at++
		return item, nil
	}
	if p.err != nil {
		return zero, p.err
	}
	return zero, iterator.Done
}

func (p *pageIter[T]) signal() []string { return p.current }

func TestDrainReadsAnIteratorToCompletion(t *testing.T) {
	it := &pageIter[string]{items: []string{"a", "b", "c"}}
	got, err := drain(it.next, nil)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("drain returned %v", got)
	}
}

// A drain that fails returns the items it ALREADY READ beside the error. An
// aggregated list commonly fails on a later page, and discarding the earlier
// pages would turn a partial answer into no answer.
func TestDrainKeepsWhatItReadWhenTheIteratorFails(t *testing.T) {
	boom := errors.New("transport failure")
	it := &pageIter[string]{items: []string{"a", "b"}, err: boom}
	got, err := drain(it.next, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("drain: got %v, want the iterator's own error", err)
	}
	if len(got) != 2 {
		t.Errorf("drain discarded the %d items it had already read; got %d", 2, len(got))
	}
}

// THE PARTIAL-READ ARM. A page that reports a scope the provider could not read
// makes the whole drain partial, and the error names the scope.
func TestDrainReportsAPartialReadNamingTheScope(t *testing.T) {
	it := &pageIter[string]{
		items: []string{"a", "b"},
		pages: [][]string{nil, {"europe-west1"}},
	}
	got, err := drain(it.next, it.signal)
	if err == nil {
		t.Fatal("a page reporting an unread scope drained clean; the walk would assert COMPLETE " +
			"having never seen that scope, and the next full-replace collect would delete it")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Fatalf("the error does not classify as a partial read: %v", err)
	}
	if !strings.Contains(err.Error(), "europe-west1") {
		t.Errorf("the error does not name the scope: %v", err)
	}
	// The items it DID read are kept: a partial answer is still an answer.
	if len(got) != 2 {
		t.Errorf("a partial read discarded the items it had: got %d, want 2", len(got))
	}
}

// THE SAME-RUN CONTROL. Without it, a drain that reported partial on every page
// would pass the case above. A page carrying no signal drains clean.
func TestDrainWithNoSignalOnAnyPageIsNotPartial(t *testing.T) {
	it := &pageIter[string]{items: []string{"a", "b"}, pages: [][]string{nil, nil}}
	_, err := drain(it.next, it.signal)
	if err != nil {
		t.Fatalf("a drain with no partial-read signal reported %v", err)
	}
}

func TestDrainDedupesAndOrdersTheScopesItNames(t *testing.T) {
	it := &pageIter[string]{
		items: []string{"a", "b", "c"},
		pages: [][]string{{"europe-west1"}, {"europe-west1"}, {"asia-east1", "europe-west1"}},
	}
	_, err := drain(it.next, it.signal)
	if err == nil {
		t.Fatal("no partial read was reported")
	}
	// One mention each, in a stable order, so two runs of an unchanged project
	// produce the same reason.
	if got := strings.Count(err.Error(), "europe-west1"); got != 1 {
		t.Errorf("the scope is named %d times, want 1: %v", got, err)
	}
	if !strings.Contains(err.Error(), "asia-east1, europe-west1") {
		t.Errorf("the scopes are not in a stable sorted order: %v", err)
	}
}

// A page signal AND a hard failure: the failure wins, because it is the stronger
// statement, and the partial scopes are still named so nothing is lost.
func TestDrainReportsBothAFailureAndThePartialScopesItSaw(t *testing.T) {
	boom := errors.New("transport failure")
	it := &pageIter[string]{
		items: []string{"a"}, err: boom,
		pages: [][]string{{"europe-west1"}},
	}
	_, err := drain(it.next, it.signal)
	if !errors.Is(err, boom) {
		t.Fatalf("the hard failure was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "europe-west1") {
		t.Errorf("the partial scope seen before the failure was lost: %v", err)
	}
}

// --------------------------------------------------------------- drainScoped

type scopedPair struct {
	key   string
	items []string
	code  string
}

func scopedIter(pairs ...scopedPair) func() (scopedPair, error) {
	at := 0
	return func() (scopedPair, error) {
		if at < len(pairs) {
			p := pairs[at]
			at++
			return p, nil
		}
		return scopedPair{}, iterator.Done
	}
}

func scopeOf(p scopedPair) ([]string, string) {
	if warningIsPartialRead(p.code) {
		return p.items, p.key
	}
	return p.items, ""
}

// THE DEFECT THIS FILE EXISTS FOR. An aggregated list reports an unreachable
// zone as a WARNING on that zone's entry rather than as an error. The first
// version skipped the scope and returned a clean nil, so the walk asserted
// COMPLETE having never seen that zone.
func TestDrainScopedReportsAnUnreachableScopeRatherThanSkippingIt(t *testing.T) {
	got, err := drainScoped(scopedIter(
		scopedPair{key: "zones/us-central1-a", items: []string{"vm-1"}},
		scopedPair{key: "zones/europe-west1-b", code: "UNREACHABLE"},
		scopedPair{key: "zones/asia-east1-a", items: []string{"vm-2"}},
	), scopeOf)

	if err == nil {
		t.Fatal("an UNREACHABLE scope drained clean; the walk would assert COMPLETE having never " +
			"seen that zone, and the next full-replace collect would delete its resources")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Fatalf("the error does not classify as a partial read: %v", err)
	}
	if !strings.Contains(err.Error(), "zones/europe-west1-b") {
		t.Errorf("the error does not name the unreachable scope: %v", err)
	}
	// EVERY OTHER SCOPE'S ITEMS ARE KEPT. One unreachable zone out of dozens must
	// not cost the other zones' resources.
	if strings.Join(got, ",") != "vm-1,vm-2" {
		t.Errorf("the reachable scopes' items were lost: %v", got)
	}
}

// The same-run control: with every scope reachable the drain is clean, so the
// error above is the signal deciding rather than an instrument that always fires.
func TestDrainScopedWithEveryScopeReachableIsClean(t *testing.T) {
	got, err := drainScoped(scopedIter(
		scopedPair{key: "zones/us-central1-a", items: []string{"vm-1"}},
		scopedPair{key: "zones/asia-east1-a", items: []string{"vm-2"}},
	), scopeOf)
	if err != nil {
		t.Fatalf("a fully reachable aggregated list reported %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d items, want 2", len(got))
	}
}

// A scope carrying an ORDINARY warning is not a partial read. The provider uses
// the same field for routine notices, and treating every one as data loss would
// make the walk permanently incomplete and the signal worthless.
func TestDrainScopedIgnoresAnOrdinaryWarning(t *testing.T) {
	_, err := drainScoped(scopedIter(
		scopedPair{key: "zones/us-central1-a", items: []string{"vm-1"}, code: "NO_RESULTS_ON_PAGE"},
	), scopeOf)
	if err != nil {
		t.Fatalf("a routine warning was reported as a partial read: %v", err)
	}
}

func TestDrainScopedKeepsWhatItReadWhenTheIteratorFails(t *testing.T) {
	boom := errors.New("transport failure")
	at := 0
	next := func() (scopedPair, error) {
		if at == 0 {
			at++
			return scopedPair{key: "zones/us-central1-a", items: []string{"vm-1"}}, nil
		}
		return scopedPair{}, boom
	}
	got, err := drainScoped(next, scopeOf)
	if !errors.Is(err, boom) {
		t.Fatalf("drainScoped: got %v, want the iterator's own error", err)
	}
	if len(got) != 1 {
		t.Errorf("drainScoped discarded the scope it had already read; got %d items", len(got))
	}
}

// ------------------------------------------------------- warningIsPartialRead

// The two codes that mean part of the answer is missing, and a sample of the
// many that do not. The provider's own documentation for each is quoted in the
// source; this pins that only those two are treated as data loss.
func TestWarningIsPartialRead(t *testing.T) {
	for _, code := range []string{"UNREACHABLE", "PARTIAL_SUCCESS"} {
		if !warningIsPartialRead(code) {
			t.Errorf("%q is a partial-read code and was not treated as one", code)
		}
	}
	for _, code := range []string{
		"", "NO_RESULTS_ON_PAGE", "DEPRECATED_RESOURCE_USED", "LARGE_DEPLOYMENT_WARNING",
		"NOT_CRITICAL_ERROR", "CLEANUP_FAILED", "UNDEFINED_CODE",
	} {
		if warningIsPartialRead(code) {
			t.Errorf("%q is a routine warning and was treated as data loss", code)
		}
	}
}

func TestComputeWarningScopesReadsTheCodeAndTheScopeData(t *testing.T) {
	scopes := computeWarningScopes(&computepb.Warning{
		Code: new("UNREACHABLE"),
		Data: []*computepb.Data{{Key: new("scope"), Value: new("zones/europe-west1-b")}},
	})
	if len(scopes) != 1 || scopes[0] != "zones/europe-west1-b" {
		t.Errorf("the scope carried in the warning data was not read: %v", scopes)
	}

	// With no scope datum the code alone still reports a partial read, named by
	// the code, because losing the fact to a missing label would be worse than
	// naming it vaguely.
	scopes = computeWarningScopes(&computepb.Warning{Code: new("PARTIAL_SUCCESS")})
	if len(scopes) != 1 {
		t.Fatalf("a partial-read warning with no scope datum reported %v", scopes)
	}

	// The controls.
	if got := computeWarningScopes(nil); got != nil {
		t.Errorf("a nil warning reported %v", got)
	}
	if got := computeWarningScopes(&computepb.Warning{Code: new("NO_RESULTS_ON_PAGE")}); got != nil {
		t.Errorf("a routine warning reported %v", got)
	}
}

// ------------------------------------------------------------------ closeAll

func TestCloseAllReportsEveryFailureAndSkipsNilClosers(t *testing.T) {
	first := errors.New("first client")
	second := errors.New("second client")
	err := closeAll([]func() error{
		func() error { return nil },
		nil,
		func() error { return first },
		func() error { return second },
	})
	if err == nil {
		t.Fatal("closeAll swallowed two close failures")
	}
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Errorf("closeAll lost one of the failures: %v", err)
	}
}

func TestCloseAllOnCleanClosersReturnsNil(t *testing.T) {
	if err := closeAll([]func() error{func() error { return nil }, nil}); err != nil {
		t.Errorf("closeAll on clean closers returned %v", err)
	}
}
