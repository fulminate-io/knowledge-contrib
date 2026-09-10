// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// walk_test.go — the completeness assertion in all four cells, every error arm,
// the two filters, and the determinism the carry-forward rides.

// pagedGroups builds a two-page recording for the log group "/g", which is the
// group every table in this suite names.
func pagedGroups(first, second []recordedEvent) []recordedGroup {
	return []recordedGroup{{
		LogGroup: "/g",
		Pages: []recordedPage{
			{NextToken: "page-2", Events: first},
			{Events: second},
		},
	}}
}

// ev builds one recorded event on the log stream "s", which is the stream every
// table in this suite records against.
func ev(id string, tsMillis int64, message string) recordedEvent {
	ts := tsMillis
	return recordedEvent{EventID: id, Timestamp: &ts, LogStreamName: "s", Message: message}
}

// TestWalkCompleteWhenPagingExhaustsTheCursor is cell (1): an unbounded run
// that follows NextToken to the end asserts a complete walk.
func TestWalkCompleteWhenPagingExhaustsTheCursor(t *testing.T) {
	groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one")},
		[]recordedEvent{ev("2", 1772366780000, "alpha two")})
	client := newFakeClientFor(t, groups)
	result, err := collectorWithFake(client).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("the walk asserted INCOMPLETE (%q) after exhausting the cursor", result.Complete.Reason())
	}
	if client.calls != 2 {
		t.Errorf("the walk made %d requests, want 2; pagination must follow the next token", client.calls)
	}
}

// TestWalkCompleteOverABoundedWindow is cell (2), and it is the one a
// conservative implementation gets wrong. The requested window IS the source
// for this collect, so enumerating all of it is a COMPLETE walk. Reporting
// incomplete would disable the deletion phase permanently, with no red anywhere.
func TestWalkCompleteOverABoundedWindow(t *testing.T) {
	groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one")},
		[]recordedEvent{ev("2", 1772366780000, "alpha two")})
	params := Params{
		LogGroups: []string{"/g"},
		StartTime: "2026-03-01T12:00:00Z",
		EndTime:   "2026-03-01T12:30:00Z",
	}
	result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("a walk over a bounded window asserted INCOMPLETE (%q); a narrower window is a smaller "+
			"source, not a partial walk", result.Complete.Reason())
	}
}

// TestWalkIncompleteWhenTheMaxEntriesCapCuts is cell (3): a bound really did
// stop the walk short of its source.
func TestWalkIncompleteWhenTheMaxEntriesCapCuts(t *testing.T) {
	groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one"), ev("2", 1772366775000, "alpha two")},
		[]recordedEvent{ev("3", 1772366780000, "alpha three")})
	one := 1
	params := Params{LogGroups: []string{"/g"}, MaxEntries: &one}
	result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a walk cut off at its entry bound asserted COMPLETE")
	}
	if !strings.Contains(result.Complete.Reason(), "max_entries") {
		t.Errorf("the incompleteness reason %q does not name the bound that cut the walk", result.Complete.Reason())
	}
}

// TestTheCapAlsoCutsBetweenLogGroups covers the cap reached before a later
// named group is walked at all, which is a different code path from the cap
// reached inside one group's pages.
func TestTheCapAlsoCutsBetweenLogGroups(t *testing.T) {
	groups := []recordedGroup{
		{LogGroup: "/a", Pages: []recordedPage{{Events: []recordedEvent{ev("1", 1772366774000, "alpha one")}}}},
		{LogGroup: "/b", Pages: []recordedPage{{Events: []recordedEvent{ev("2", 1772366780000, "beta one")}}}},
	}
	one := 1
	params := Params{LogGroups: []string{"/a", "/b"}, MaxEntries: &one}
	result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a walk that never reached its second log group asserted COMPLETE")
	}
	if !strings.Contains(result.Complete.Reason(), "/b") {
		t.Errorf("the reason %q does not name the log group that was never walked", result.Complete.Reason())
	}
}

// TestWalkFailsWholeOnAPageError is cell (4), in both positions: on the first
// page and on a LATER page after entries have already been read. The built-in
// adapter returns the partial result on the later-page arm; here that would
// assert a complete walk over a partial page set.
func TestWalkFailsWholeOnAPageError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		errAtCall int
	}{
		{"on the first page", 1},
		{"on a later page, after entries were read", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one")},
				[]recordedEvent{ev("2", 1772366780000, "alpha two")})
			client := newFakeClientFor(t, groups)
			client.err = errors.New("throttled")
			client.errAtCall = tc.errAtCall

			result, err := collectorWithFake(client).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{})
			if err == nil {
				t.Fatalf("the walk returned %d nodes instead of an error", len(result.Nodes))
			}
			if len(result.Nodes) != 0 || len(result.Edges) != 0 {
				t.Errorf("the failed walk returned %d nodes and %d edges; it must write nothing",
					len(result.Nodes), len(result.Edges))
			}
			if !strings.Contains(err.Error(), "/g") || !strings.Contains(err.Error(), "throttled") {
				t.Errorf("the error names neither the log group nor the cause: %v", err)
			}
		})
	}
}

// TestWalkRefusesAnEventWithNoTimestamp is the error arm carried through the
// walk rather than asserted on the converter alone.
func TestWalkRefusesAnEventWithNoTimestamp(t *testing.T) {
	groups := []recordedGroup{{
		LogGroup: "/g",
		Pages:    []recordedPage{{Events: []recordedEvent{{EventID: "e-9", Message: "x", LogStreamName: "s"}}}},
	}}
	result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("an event with no timestamp was walked")
	}
	if len(result.Nodes) != 0 {
		t.Errorf("the refused walk returned %d nodes", len(result.Nodes))
	}
}

// TestAnEmptyLogGroupProducesAnEmptyCompleteWalk covers the first real run of a
// new collector: an empty result is a complete walk that found nothing, and it
// carries EMPTY slices rather than nil so the envelope validates.
func TestAnEmptyLogGroupProducesAnEmptyCompleteWalk(t *testing.T) {
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{}}}}
	result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("an empty log group failed the walk: %v", err)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("an empty walk asserted INCOMPLETE (%q)", result.Complete.Reason())
	}
	if len(result.Nodes) != 0 || len(result.Edges) != 0 {
		t.Errorf("an empty log group produced %d nodes and %d edges", len(result.Nodes), len(result.Edges))
	}
}

func chunkIDsOf(result framework.Result) []string { return idsOfType(result, nodeLogChunk) }
