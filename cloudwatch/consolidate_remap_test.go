// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// consolidate_remap_test.go — the RE-POINTING half of the consolidation stage:
// which survivor a dropped fragment ends up on, and the two invariants that
// choice exists to hold. The merge itself is in consolidate_test.go.

// TestTheAbsorberWinsTheRemap is the OTHER HALF: the re-pointing follows the
// actual merge rather than a total order, which is what the returned absorption
// relation is for.
func TestTheAbsorberWinsTheRemap(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	fragments := goStackFragments(3, base)

	// A survivor that is NEWER than the merged template, so a re-pointing by
	// the total order alone would choose it and this cell would red.
	newer := &LogTemplate{
		ID: templateID("unrelated later line"), Pattern: "unrelated later line",
		Severity: SeverityInfo, Count: 1,
		FirstSeen: base.Add(time.Hour), LastSeen: base.Add(time.Hour),
	}
	input := append(append([]*LogTemplate{}, fragments...), newer)

	after, absorbedBy := runConsolidators(DefaultConsolidators(), input)
	remap := buildTemplateRemap(input, after, absorbedBy)

	var merged *LogTemplate
	for _, tpl := range after {
		if tpl.Pattern == goStackMergedPattern {
			merged = tpl
		}
	}
	if merged == nil {
		t.Fatalf("no merged survivor among %d templates", len(after))
	}
	for _, f := range fragments {
		if remap[f.ID] != merged.ID {
			t.Errorf("fragment %s re-points at %s, want the ABSORBER %s (the total order would have chosen %s)",
				f.ID, remap[f.ID], merged.ID, newer.ID)
		}
	}
}

// TestConsolidationRemapIsDeterministic runs the same input repeatedly. The
// built-in re-pointing chooses by the last key of a randomized map walk, so it
// is this cell that a faithful reproduction of it would fail.
func TestConsolidationRemapIsDeterministic(t *testing.T) {
	const runs = 30
	entries := goStackEntries()

	first, _, err := buildGraph(entries, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	firstIDs := nodeIDSet(first)
	for i := range runs {
		nodes, _, err := buildGraph(entries, CloudContext{})
		if err != nil {
			t.Fatalf("buildGraph run %d: %v", i, err)
		}
		got := nodeIDSet(nodes)
		if len(got) != len(firstIDs) {
			t.Fatalf("run %d produced %d nodes, the first produced %d", i, len(got), len(firstIDs))
		}
		for id := range firstIDs {
			if _, ok := got[id]; !ok {
				t.Fatalf("run %d did not produce node %q", i, id)
			}
		}
	}
	// A KNOWN POSITIVE in the same battery: the input really does exercise the
	// consolidator, so a run that consolidated nothing is not what made the
	// determinism hold.
	merged := 0
	for _, n := range first {
		if n.Type == nodeLogTemplate && n.Metadata["pattern"] == goStackMergedPattern {
			merged++
		}
	}
	if merged != 1 {
		t.Fatalf("the fixture produced %d merged templates, want exactly 1; without one this test is vacuous", merged)
	}
}

// TestTieBreakIsTotalAndOrdered covers the fallback used for a fragment dropped
// with NO absorber, at each level of the order in turn.
func TestTieBreakIsTotalAndOrdered(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mk := func(pattern string, first, last time.Time) *LogTemplate {
		return &LogTemplate{ID: templateID(pattern), Pattern: pattern, FirstSeen: first, LastSeen: last}
	}

	t.Run("LastSeen descending wins first", func(t *testing.T) {
		older := mk("a", base, base)
		newer := mk("b", base, base.Add(time.Hour))
		if got := pickRemapFallback([]*LogTemplate{older, newer}); got != newer.ID {
			t.Errorf("picked %s, want the template with the later LastSeen %s", got, newer.ID)
		}
	})
	t.Run("FirstSeen descending breaks a LastSeen tie", func(t *testing.T) {
		earlier := mk("a", base, base.Add(time.Hour))
		later := mk("b", base.Add(time.Minute), base.Add(time.Hour))
		if got := pickRemapFallback([]*LogTemplate{earlier, later}); got != later.ID {
			t.Errorf("picked %s, want the template with the later FirstSeen %s", got, later.ID)
		}
	})
	t.Run("ID ascending breaks a full tie", func(t *testing.T) {
		a := mk("alpha", base, base)
		b := mk("beta", base, base)
		want := min(a.ID, b.ID)
		// BOTH SLICE ORDERS, which is what makes this level observable at all.
		// With one order a comparison that always reports "not less" leaves the
		// slice untouched and happens to yield the wanted element; running both
		// orders means only a real ordering can satisfy them together.
		for _, order := range [][]*LogTemplate{{a, b}, {b, a}} {
			if got := pickRemapFallback(order); got != want {
				t.Errorf("with %s first, picked %s, want the lower id %s; without this level the order is not total",
					order[0].Pattern, got, want)
			}
		}
	})
	t.Run("no survivor yields no fallback", func(t *testing.T) {
		if got := pickRemapFallback(nil); got != "" {
			t.Errorf("picked %q from an empty survivor set", got)
		}
	})
}

// goStackEntries is a fixture whose messages produce enough Go stack fragments
// to merge, beside ordinary lines that must not.
func goStackEntries() []LogEntry {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	labels := map[string]string{"log_group": "/app", "service": "app"}
	messages := []string{
		"goroutine 1 [running]:",
		"goroutine 2 [select]:",
		"goroutine 3 [chan receive]:",
		"goroutine 4 [IO wait]:",
		"served request ok",
		"cache miss",
	}
	out := make([]LogEntry, 0, len(messages))
	for i, m := range messages {
		out = append(out, LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Severity:  SeverityError,
			Message:   m,
			Labels:    labels,
		})
	}
	return out
}

// nodeIDSet indexes emitted nodes by id.
func nodeIDSet(nodes []framework.Node) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		out[n.ID] = struct{}{}
	}
	return out
}
