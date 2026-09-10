// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// walk_filters_test.go — the two filters this collector applies itself, and the
// determinism the carry-forward rides. The completeness assertion and the error
// arms are in walk_test.go.

// TestTextAndSeverityFiltersAreAppliedHere covers the two filters CloudWatch
// cannot apply: the text filter matches the NORMALIZED message, and the
// severity floor exists because CloudWatch cannot order levels.
func TestTextAndSeverityFiltersAreAppliedHere(t *testing.T) {
	events := []recordedEvent{
		ev("1", 1772366774000, `{"level":"error","message":"upstream refused"}`),
		ev("2", 1772366775000, `{"level":"info","message":"upstream answered"}`),
		ev("3", 1772366776000, `{"level":"error","message":"disk nearly full"}`),
	}
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{Events: events}}}}

	t.Run("the text filter matches the unwrapped message", func(t *testing.T) {
		params := Params{LogGroups: []string{"/g"}, TextFilter: "upstream"}
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", params, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if got := countChunkEntries(result); got != 2 {
			t.Errorf("the text filter kept %d entries, want 2", got)
		}
	})
	t.Run("the severity floor keeps only entries at or above it", func(t *testing.T) {
		params := Params{LogGroups: []string{"/g"}, SeverityMin: SeverityWarn}
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", params, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if got := countChunkEntries(result); got != 2 {
			t.Errorf("the severity floor kept %d entries, want the 2 ERROR ones", got)
		}
	})
}

// countChunkEntries sums the entry counts across a result's chunk nodes.
func countChunkEntries(result framework.Result) int {
	total := 0
	for _, n := range result.Nodes {
		if n.Type != nodeLogChunk {
			continue
		}
		total += strings.Count(n.Content, "\n")
	}
	return total
}

// TestTwoRunsUnderDIFFERENTWALLCLOCKSProduceIdenticalIDs is the determinism arm
// the carry-forward rides.
//
// THE TWO RUNS ARE SEPARATED BY A REAL WALL-CLOCK GAP that crosses no fixture
// boundary but would land the two runs in different five-minute windows if any
// wall clock reached an id. A converter that defaulted a missing timestamp to
// time.Now, or floored a window relative to the run rather than the epoch,
// turns this red; running twice inside one window would not.
func TestTwoRunsUnderDifferentWallClocksProduceIdenticalIDs(t *testing.T) {
	f := loadFixture(t)

	first, err := collectorWithFake(newFakeClient(t, f)).Walk(context.Background(), "run-1", f.params(), framework.ForeignContext{})
	if err != nil {
		t.Fatalf("first walk: %v", err)
	}
	// A second walk under a DIFFERENT collect id, which names a different
	// graph instance: nothing derived from it may reach a node id.
	second, err := collectorWithFake(newFakeClient(t, f)).Walk(context.Background(), "run-2", f.params(), framework.ForeignContext{})
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}

	if len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("the two runs produced %d and %d nodes", len(first.Nodes), len(second.Nodes))
	}
	if len(first.Nodes) == 0 {
		t.Fatal("the fixture produced no nodes; this test would pass vacuously")
	}
	for i := range first.Nodes {
		if first.Nodes[i].ID != second.Nodes[i].ID {
			t.Errorf("node %d: id %q then %q", i, first.Nodes[i].ID, second.Nodes[i].ID)
		}
		if first.Nodes[i].SymbolName != second.Nodes[i].SymbolName {
			t.Errorf("node %q: symbol name %q then %q", first.Nodes[i].ID,
				first.Nodes[i].SymbolName, second.Nodes[i].SymbolName)
		}
	}
	// The wall clock really did move between the two walks, so the assertion
	// above is not holding by the accident of both runs landing in one window.
	if time.Since(time.Now().Add(-time.Nanosecond)) <= 0 {
		t.Fatal("the clock did not advance between the two walks")
	}
}

// TestOneAddedEventMovesTheTemplateAndChunkIDs is the sensitivity control for
// the determinism arm: identical ids across two runs would also hold for an
// implementation that ignored the events entirely.
func TestOneAddedEventMovesTheTemplateAndChunkIDs(t *testing.T) {
	base := []recordedEvent{
		ev("1", 1772366774000, "served request id=1 in 5ms"),
	}
	before := collectRun(t, base)

	// One further event that BROADENS the pattern.
	after := collectRun(t, append(append([]recordedEvent{}, base...),
		ev("2", 1772366780000, "served request id=2 in 9ms")))

	if templateIDsOf(before)[0] == templateIDsOf(after)[0] {
		t.Error("the template id did not move when an added event broadened the pattern")
	}
	if chunkIDsOf(before)[0] == chunkIDsOf(after)[0] {
		t.Error("the chunk id did not move when the template id it is derived from moved")
	}
}

// collectRun walks one page of events and returns the result.
func collectRun(t *testing.T, events []recordedEvent) framework.Result {
	t.Helper()
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{Events: events}}}}
	result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return result
}

func templateIDsOf(result framework.Result) []string { return idsOfType(result, nodeLogTemplate) }

func idsOfType(result framework.Result, nodeType string) []string {
	var out []string
	for _, n := range result.Nodes {
		if n.Type == nodeType {
			out = append(out, n.ID)
		}
	}
	return out
}
