// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"
)

// consolidate_test.go — the merge, and the two invariants this collector's
// deliberate divergence from the built-in re-pointing exists to hold.

// goStackFragments builds n Go stack fragments one second apart, which is
// within every temporal group this suite exercises.
func goStackFragments(n int, base time.Time) []*LogTemplate {
	const gap = time.Second
	out := make([]*LogTemplate, 0, n)
	for i := range n {
		pattern := "goroutine " + string(rune('1'+i)) + " [running]:"
		ts := base.Add(time.Duration(i) * gap)
		tpl := &LogTemplate{
			ID: templateID(pattern), Pattern: pattern, Severity: SeverityError,
			Count: 1, FirstSeen: ts, LastSeen: ts,
		}
		tpl.Alias = TemplateAliasFor(tpl)
		out = append(out, tpl)
	}
	return out
}

// TestGoStackGroupOfThreeMergesAndTwoDoesNot pins the group-size boundary the
// consolidator's own selection sets.
func TestGoStackGroupOfThreeMergesAndTwoDoesNot(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	two := goStackFragments(2, base)
	if got := (&goStackConsolidator{}).Consolidate(two); len(got.Templates) != 2 || len(got.Absorptions) != 0 {
		t.Errorf("a group of two produced %d templates and %d absorptions, want 2 and 0 — it passes through untouched",
			len(got.Templates), len(got.Absorptions))
	}

	three := goStackFragments(3, base)
	got := (&goStackConsolidator{}).Consolidate(three)
	if len(got.Templates) != 1 {
		t.Fatalf("a group of three produced %d templates, want 1 merged survivor", len(got.Templates))
	}
	if got.Templates[0].Severity != SeverityCritical {
		t.Errorf("the merged template's severity is %q, want %q", got.Templates[0].Severity, SeverityCritical)
	}
	if got.Templates[0].Count != 3 {
		t.Errorf("the merged template's count is %d, want the sum 3", got.Templates[0].Count)
	}
}

// TestMergedTemplateHasAnID is HALF THE DIVERGENCE from the built-in path,
// whose merged template is built with no id — which lands it under the empty
// key, makes it unusable as a re-pointing target, and orphans every fragment it
// absorbed.
func TestMergedTemplateHasAnID(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got := (&goStackConsolidator{}).Consolidate(goStackFragments(3, base))
	merged := got.Templates[0]
	if merged.ID == "" {
		t.Fatal("the merged template has no id")
	}
	if merged.ID != templateID(merged.Pattern) {
		t.Errorf("the merged id %q is not the hash of its pattern; it must follow the same rule every id follows", merged.ID)
	}
	if merged.Alias == "" {
		t.Error("the merged template has no alias, so its node would have an empty symbol name")
	}
}

// TestNoOrphanedEntryAndNoDanglingContainsEdge asserts the two invariants the
// divergence exists to hold, over the whole emission path rather than over the
// re-pointing alone.
func TestNoOrphanedEntryAndNoDanglingContainsEdge(t *testing.T) {
	entries := goStackEntries()
	nodes, edges, err := buildGraph(entries, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	ids := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		ids[n.ID] = struct{}{}
	}
	contains := 0
	for _, e := range edges {
		if e.Type != edgeContains {
			continue
		}
		contains++
		if _, ok := ids[e.FromID]; !ok {
			t.Errorf("a CONTAINS edge starts at template %q, which is not a node in this graph", e.FromID)
		}
		if _, ok := ids[e.ToID]; !ok {
			t.Errorf("a CONTAINS edge ends at chunk %q, which is not a node in this graph", e.ToID)
		}
	}
	if contains == 0 {
		t.Fatal("the fixture produced no CONTAINS edge at all; the invariant would hold vacuously")
	}
}

// TestPythonTracebackMergesOnItsOwnWindow covers the second consolidator and
// the header rule that lets it fire with fewer than three other fragments.
func TestPythonTracebackMergesOnItsOwnWindow(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mk := func(pattern string, offset time.Duration) *LogTemplate {
		ts := base.Add(offset)
		return &LogTemplate{ID: templateID(pattern), Pattern: pattern, Severity: SeverityInfo, Count: 1, FirstSeen: ts, LastSeen: ts}
	}
	group := []*LogTemplate{
		mk("Traceback (most recent call last):", 0),
		mk(`  File "/app/main.py", line 12, in handler`, time.Second),
		mk("app.errors.TimeoutError: upstream did not answer", 2*time.Second),
	}
	got := (&pythonTracebackConsolidator{}).Consolidate(group)
	if len(got.Templates) != 1 {
		t.Fatalf("produced %d templates, want 1 merged survivor", len(got.Templates))
	}
	merged := got.Templates[0]
	if merged.Severity != SeverityError {
		t.Errorf("merged severity %q, want %q", merged.Severity, SeverityError)
	}
	if merged.ID == "" || merged.ID != templateID(merged.Pattern) {
		t.Errorf("merged id %q is not the hash of its pattern %q", merged.ID, merged.Pattern)
	}
	if len(got.Absorptions) != 1 || len(got.Absorptions[0].Fragments) != 3 {
		t.Errorf("absorptions = %+v, want one absorption naming all three fragments", got.Absorptions)
	}
}

// TestTwoMergedSurvivorsOnOnePatternAreFoldedIntoOne covers the id collision
// two separate crashes in one collect would otherwise produce: a template id is
// a hash of its pattern, and both merges name the same pattern.
func TestTwoMergedSurvivorsOnOnePatternAreFoldedIntoOne(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	first := goStackFragments(3, base)
	// A second burst an hour later, well outside the thirty-second window, so
	// it forms its own group and its own merged survivor.
	second := goStackFragments(3, base.Add(time.Hour))
	for i, tpl := range second {
		tpl.Pattern += " second"
		tpl.ID = templateID(tpl.Pattern)
		second[i] = tpl
	}

	got := (&goStackConsolidator{}).Consolidate(append(append([]*LogTemplate{}, first...), second...))
	merged := 0
	seen := map[string]int{}
	for _, tpl := range got.Templates {
		seen[tpl.ID]++
		if tpl.Pattern == goStackMergedPattern {
			merged++
		}
	}
	if merged != 1 {
		t.Errorf("two bursts produced %d merged templates under one pattern; two nodes would share one id", merged)
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("template id %q appears %d times among the survivors", id, n)
		}
	}
	for _, tpl := range got.Templates {
		if tpl.Pattern == goStackMergedPattern && tpl.Count != 6 {
			t.Errorf("the folded survivor's count is %d, want the sum 6", tpl.Count)
		}
	}
}

// TestAGroupWithNeitherTraceShapeConsolidatesNothing states the zero rather
// than leaving it unexplained: a log group carrying no stack dump and no
// traceback exercises neither consolidator.
func TestAGroupWithNeitherTraceShapeConsolidatesNothing(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	plain := []*LogTemplate{
		{ID: templateID("served request"), Pattern: "served request", FirstSeen: base, LastSeen: base},
		{ID: templateID("cache miss"), Pattern: "cache miss", FirstSeen: base, LastSeen: base},
		{ID: templateID("disk usage high"), Pattern: "disk usage high", FirstSeen: base, LastSeen: base},
	}
	after, absorbedBy := runConsolidators(DefaultConsolidators(), plain)
	if len(after) != len(plain) {
		t.Errorf("consolidation changed %d templates into %d", len(plain), len(after))
	}
	if len(absorbedBy) != 0 {
		t.Errorf("consolidation recorded %d absorptions over templates carrying neither trace shape", len(absorbedBy))
	}
}
