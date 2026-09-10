// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/logging"
)

// collect_test.go — the read: the bound, the truncation assertion, the
// mid-stream failure posture, and the client-side predicates.

// TestTheBoundStopsTheReadAndTruncationIsAsserted is the pair the completeness
// assertion rests on: a read the bound stopped has NOT enumerated its source and
// must say so, and a read that reached the end must not.
func TestTheBoundStopsTheReadAndTruncationIsAsserted(t *testing.T) {
	entries := recordedGCPEntries()

	bounded, err := drainEntries(&fakeNexter{entries: entries}, "p", logQuery{MaxEntries: 2})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(bounded.Entries) != 2 {
		t.Errorf("the bound of 2 returned %d entries", len(bounded.Entries))
	}
	if !bounded.Truncated {
		t.Errorf("a read stopped by the bound did not assert truncation")
	}

	// SAME-RUN CONTROL: a read that exhausts the iterator asserts completeness.
	whole, err := drainEntries(&fakeNexter{entries: entries}, "p", logQuery{MaxEntries: len(entries) + 10})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if whole.Truncated {
		t.Errorf("an exhausted read asserted truncation")
	}
	if len(whole.Entries) != len(entries) {
		t.Errorf("the exhausted read returned %d of %d entries", len(whole.Entries), len(entries))
	}
}

// TestACollectNamingNoBoundDrainsPastFiveThousandEntries is the arm that makes
// "no bound means unbounded" observable at the seam where the bound is decided.
//
// COLLECTOR TRAFFIC CARRIES NO CAP. The client and a collector speak MCP to each
// other and none of it reaches a language model's context, so the reason a cap
// exists elsewhere does not apply here — and a module-side default that silently
// stopped at some round number would drop collected data for a caller who asked
// for none of it, while asserting an incomplete walk they did not cause.
//
// FIVE THOUSAND IS NOT AN ARBITRARY FIXTURE SIZE: it is the default this module
// used to impose, so a run that stops there is the specific regression this arm
// exists to catch.
func TestACollectNamingNoBoundDrainsPastFiveThousandEntries(t *testing.T) {
	const served = 6000
	entries := make([]*logging.Entry, 0, served)
	for range served {
		entries = append(entries, gcpEntry(0, logging.Info, "request served", "", nil, nil))
	}

	q, err := (params{Project: "p"}).toQuery()
	if err != nil {
		t.Fatalf("the minimal params were refused: %v", err)
	}
	if q.MaxEntries > 0 {
		t.Fatalf("params naming no bound produced the bound %d; an omitted bound means unbounded", q.MaxEntries)
	}

	got, err := drainEntries(&fakeNexter{entries: entries}, "p", q)
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(got.Entries) != served {
		t.Fatalf("an unbounded read returned %d of %d entries", len(got.Entries), served)
	}
	if got.Truncated {
		t.Errorf("an unbounded read that exhausted the source asserted truncation")
	}
}

// TestACallerNamedBoundIsHonoredWithNoCeiling is the other half: the bound stays
// available as a FILTER the caller chose, at any size. A ceiling would be the
// module refusing traffic on the caller's behalf, which is the thing the ruling
// forbids; an explicit bound is the caller bounding their own request.
func TestACallerNamedBoundIsHonoredWithNoCeiling(t *testing.T) {
	for _, bound := range []int{1, 5000, 200001, 10000000} {
		q, err := (params{Project: "p", MaxEntries: new(bound)}).toQuery()
		if err != nil {
			t.Fatalf("a bound of %d was refused: %v", bound, err)
		}
		if q.MaxEntries != bound {
			t.Errorf("a bound of %d became %d", bound, q.MaxEntries)
		}
	}
}

// TestABoundEqualToTheEntryCountIsNotTruncated is the boundary cell: the bound
// is reached exactly, and there is nothing left, so asserting truncation would
// disable the server's deletion phase for no reason.
func TestABoundEqualToTheEntryCountIsNotTruncated(t *testing.T) {
	entries := recordedGCPEntries()
	got, err := drainEntries(&fakeNexter{entries: entries}, "p", logQuery{MaxEntries: len(entries)})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(got.Entries) != len(entries) {
		t.Fatalf("got %d entries, want %d", len(got.Entries), len(entries))
	}
	if got.Truncated {
		t.Errorf("a read that took exactly the bound and had nothing left asserted truncation")
	}
}

// TestAMidStreamFailureIsAFailure is the posture this module does NOT inherit.
// The knowledge client's own Cloud Logging adapter flushes what it has and
// returns nil here, so a read that died part way through reports success — and
// because the result carries a completeness assertion the server acts on, a
// partial read reported as complete is a deletion of real data.
func TestAMidStreamFailureIsAFailure(t *testing.T) {
	boom := errors.New("the stream broke")
	got, err := drainEntries(
		&fakeNexter{entries: recordedGCPEntries(), failAfter: 3, failWith: boom},
		"my-project", logQuery{})

	if err == nil {
		t.Fatalf("a mid-stream failure after 3 entries returned no error; got %d entries", len(got.Entries))
	}
	if len(got.Entries) != 0 {
		t.Errorf("a failed read returned %d entries; the partial must be discarded", len(got.Entries))
	}
	if !errors.Is(err, boom) {
		t.Errorf("the cause was not wrapped: %v", err)
	}
	for _, want := range []string{"my-project", "after 3 entries", "DISCARDED"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q: %v", want, err)
		}
	}
}

// TestAFailureOnTheFirstEntryIsAlsoAFailure is the arm that would pass even
// under the inherited posture, kept so the row above reads as a posture rather
// than as an accident of where the failure fell.
func TestAFailureOnTheFirstEntryIsAlsoAFailure(t *testing.T) {
	_, err := drainEntries(&fakeNexter{failAfter: 0, failWith: errors.New("x"), entries: nil}, "p", logQuery{})
	if err != nil {
		t.Fatalf("an empty iterator with failAfter 0 should exhaust cleanly, got %v", err)
	}
	_, err = drainEntries(
		&fakeNexter{entries: recordedGCPEntries(), failAfter: 1, failWith: errors.New("x")}, "p", logQuery{})
	if err == nil {
		t.Fatalf("a failure on the second read returned no error")
	}
}

// TestAPeekFailurePastTheBoundAssertsTruncationRatherThanFailing covers the
// third arm of the truncation measurement. The caller already has every entry it
// asked for, so the collect succeeds — but the walk cannot claim it saw the whole
// source, and between over- and under-reporting completeness the over-report is
// the one that lets the server delete rows this collect did not carry.
func TestAPeekFailurePastTheBoundAssertsTruncationRatherThanFailing(t *testing.T) {
	entries := recordedGCPEntries()
	got, err := drainEntries(
		&fakeNexter{entries: entries, failAfter: 2, failWith: errors.New("broke while peeking")},
		"p", logQuery{MaxEntries: 2})
	if err != nil {
		t.Fatalf("a peek failure failed the whole collect: %v", err)
	}
	if len(got.Entries) != 2 {
		t.Errorf("got %d entries, want the bound of 2", len(got.Entries))
	}
	if !got.Truncated {
		t.Errorf("a read that could not tell whether more remained asserted completeness")
	}
}

// TestAnEmptyReadSucceeds is the zero-entry input class: no entries is a real
// answer, not a failure.
func TestAnEmptyReadSucceeds(t *testing.T) {
	got, err := drainEntries(&fakeNexter{}, "p", logQuery{})
	if err != nil {
		t.Fatalf("an empty read failed: %v", err)
	}
	if len(got.Entries) != 0 || got.Truncated {
		t.Errorf("an empty read returned %d entries, truncated=%v", len(got.Entries), got.Truncated)
	}
}

// TestTheClientSideSeverityFilterCatchesWhatReclassificationMoved is the reason
// the severity predicate is applied again after normalization: the server-side
// filter selected on the entry's own severity, and the GKE reclassification can
// then move it BELOW what the caller asked for.
func TestTheClientSideSeverityFilterCatchesWhatReclassificationMoved(t *testing.T) {
	// An entry Cloud Logging calls ERROR whose body declares itself INFO.
	entries := []*logging.Entry{gcpEntry(0, logging.Error, "level=info request served", "", nil, nil)}
	got, err := drainEntries(&fakeNexter{entries: entries}, "p", logQuery{SeverityMin: severityError})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("an entry reclassified below the caller's floor was returned: %+v", got.Entries)
	}

	// SAME-RUN CONTROL: a real error at the same floor is returned.
	real := []*logging.Entry{gcpEntry(0, logging.Error, "connect failed", "", nil, nil)}
	kept, err := drainEntries(&fakeNexter{entries: real}, "p", logQuery{SeverityMin: severityError})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(kept.Entries) != 1 {
		t.Errorf("a real error at the floor was dropped")
	}
}

// TestTheTextFilterIsCaseInsensitive covers the predicate an operator types.
func TestTheTextFilterIsCaseInsensitive(t *testing.T) {
	entries := []*logging.Entry{
		gcpEntry(0, logging.Info, "Connection TIMEOUT on upstream", "", nil, nil),
		gcpEntry(0, logging.Info, "all good", "", nil, nil),
	}
	got, err := drainEntries(&fakeNexter{entries: entries}, "p", logQuery{TextFilter: "timeout"})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("got %d entries, want the one matching case-insensitively", len(got.Entries))
	}
}

// TestFilteredEntriesDoNotCountAgainstTheBound is the interaction between the
// two: a bound of N must return N MATCHING entries rather than stopping after N
// entries were read and most were filtered out.
func TestFilteredEntriesDoNotCountAgainstTheBound(t *testing.T) {
	var entries []*logging.Entry
	for i := range 10 {
		msg := "noise"
		if i%2 == 0 {
			msg = "wanted line"
		}
		entries = append(entries, gcpEntry(0, logging.Info, msg, "", nil, nil))
	}
	got, err := drainEntries(&fakeNexter{entries: entries}, "p",
		logQuery{TextFilter: "wanted", MaxEntries: 3})
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("got %d matching entries, want the bound of 3", len(got.Entries))
	}
	if !got.Truncated {
		t.Errorf("the bound stopped the read but truncation was not asserted")
	}
}
