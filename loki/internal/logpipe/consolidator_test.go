// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// consolidator_test.go — the consolidators and THE SETTLED DIVERGENCE.
//
// The divergence has two parts and each has its own cell. PART ONE: when a
// merged template absorbed the fragments, the remap targets THAT template even
// when an unrelated survivor has a greater LastSeen — the case a pure total
// order gets wrong, because a merged template takes the maximum LastSeen of its
// group. PART TWO: with no absorber, the target is the greatest survivor under
// LastSeen descending, then FirstSeen descending, then id ascending, and each
// tie-break level is asserted separately.
//
// The determinism cell runs the whole produce path thirty times and requires
// one id set. The built-in pipeline fails that cell: its remap picks the last
// key of a randomized map walk, and its merged templates carry no id at all.

func tpl(pattern string, sev string, first, last time.Duration) *Template {
	t := &Template{
		Pattern:   pattern,
		Severity:  sev,
		Count:     1,
		FirstSeen: at(first),
		LastSeen:  at(last),
	}
	t.ID = TemplateID(pattern)
	t.Alias = TemplateAliasFor(t)
	return t
}

// goFrame is a line the Go stack matcher recognizes.
func goFrame(n int) string {
	return fmt.Sprintf("github.com/example/pkg.Handler%d(0x140000b6000, 0x14)", n)
}

func TestGoStackConsolidatorMergesOnlyGroupsOfThreeOrMore(t *testing.T) {
	t.Run("fewer than three fragments pass through untouched", func(t *testing.T) {
		in := []*Template{
			tpl(goFrame(1), SeverityError, 0, 0),
			tpl(goFrame(2), SeverityError, time.Second, time.Second),
			tpl("service started ok", SeverityInfo, 0, 0),
		}
		out := (&goStackConsolidator{}).Consolidate(in)
		if len(out.Templates) != 3 {
			t.Fatalf("templates = %d, want 3; two fragments are not a crash", len(out.Templates))
		}
		if out.Absorbed != nil {
			t.Fatalf("absorbed = %v, want nil", out.Absorbed)
		}
		// AND IN THE ORDER THEY ARRIVED. The per-group check would also merge
		// nothing here, so the ORDER is the only thing that distinguishes the
		// too-few-fragments early return from its absence: without it the set
		// comes back partitioned, fragments last.
		for i := range in {
			if out.Templates[i] != in[i] {
				t.Fatalf("template %d came back as %q, want %q; a set too small to be a crash is returned unchanged",
					i, out.Templates[i].Pattern, in[i].Pattern)
			}
		}
	})

	t.Run("a group of two inside a larger fragment set passes through", func(t *testing.T) {
		// Three fragments overall, so the pass runs, but the third is an hour
		// later and forms its own group of one; the first two are a group of
		// two. Neither group reaches three, so nothing merges.
		in := []*Template{
			tpl(goFrame(1), SeverityError, 0, 0),
			tpl(goFrame(2), SeverityError, time.Second, time.Second),
			tpl(goFrame(3), SeverityError, time.Hour, time.Hour),
		}
		out := (&goStackConsolidator{}).Consolidate(in)
		if len(out.Templates) != 3 {
			t.Fatalf("templates = %d, want 3", len(out.Templates))
		}
		if len(out.Absorbed) != 0 {
			t.Fatalf("absorbed = %v, want nothing", out.Absorbed)
		}
	})

	t.Run("three fragments in one window merge and report what they absorbed", func(t *testing.T) {
		frags := []*Template{
			tpl(goFrame(1), SeverityError, 0, 0),
			tpl(goFrame(2), SeverityError, time.Second, time.Second),
			tpl(goFrame(3), SeverityError, 2*time.Second, 2*time.Second),
		}
		in := append([]*Template{tpl("service started ok", SeverityInfo, 0, 0)}, frags...)
		out := (&goStackConsolidator{}).Consolidate(in)
		if len(out.Templates) != 2 {
			t.Fatalf("templates = %d, want 2 (the survivor and the merged crash)", len(out.Templates))
		}
		mergedID := TemplateID("Go runtime crash (goroutine dump)")
		got, ok := out.Absorbed[mergedID]
		if !ok {
			t.Fatalf("the merged template %q reported no absorbed fragments; absorbed = %v", mergedID, out.Absorbed)
		}
		if len(got) != 3 {
			t.Fatalf("absorbed %d fragments, want 3", len(got))
		}
	})
}

// TestMergedTemplateCarriesARealID is the half of the divergence that makes the
// absorber usable as a remap target at all. A merged template with no id lands
// in the surviving set under the empty-string key, so a guard rejecting an
// empty replacement rejects the very template that took the fragments.
func TestMergedTemplateCarriesARealID(t *testing.T) {
	group := []*Template{
		tpl(goFrame(1), SeverityError, 0, 0),
		tpl(goFrame(2), SeverityError, time.Second, time.Second),
		tpl(goFrame(3), SeverityError, 2*time.Second, 2*time.Second),
	}
	merged := mergeGoStackGroup(group)
	if merged.ID == "" {
		t.Fatal("the merged template carries no id")
	}
	if want := TemplateID(merged.Pattern); merged.ID != want {
		t.Fatalf("the merged id %q is not the id of its own pattern %q (%q)", merged.ID, merged.Pattern, want)
	}
	if merged.Count != 3 {
		t.Fatalf("count = %d, want 3 (the sum of the group)", merged.Count)
	}
	if merged.Severity != SeverityCritical {
		t.Fatalf("severity = %q, want %q", merged.Severity, SeverityCritical)
	}
}

// TestRemapPartOneTargetsTheAbsorberOverAGreaterSurvivor is part one, and the
// control is in the same run: the unrelated survivor has a STRICTLY GREATER
// LastSeen than the absorber, so a pure total order would pick it.
func TestRemapPartOneTargetsTheAbsorberOverAGreaterSurvivor(t *testing.T) {
	fragment := tpl(goFrame(1), SeverityError, 0, 0)
	absorber := tpl("Go runtime crash (goroutine dump)", SeverityCritical, 0, 2*time.Second)
	later := tpl("nightly compaction finished", SeverityInfo, time.Hour, time.Hour)

	before := templatesByID([]*Template{fragment, later})
	after := templatesByID([]*Template{absorber, later})
	absorbed := map[string][]string{absorber.ID: {fragment.ID}}

	remap, err := buildTemplateRemap(before, after, absorbed)
	if err != nil {
		t.Fatalf("buildTemplateRemap: %v", err)
	}
	if got := remap[fragment.ID]; got != absorber.ID {
		t.Fatalf("the fragment remapped to %q, want the absorber %q", got, absorber.ID)
	}

	// THE CONTROL: the same call with no absorption reported picks `later`,
	// which is what proves the absorber won on the absorption relation and not
	// because it happened to be greatest.
	fallbackRemap, err := buildTemplateRemap(before, after, nil)
	if err != nil {
		t.Fatalf("buildTemplateRemap (control): %v", err)
	}
	if got := fallbackRemap[fragment.ID]; got != later.ID {
		t.Fatalf("with no absorption the fragment remapped to %q, want the greatest survivor %q; "+
			"the control does not distinguish the two rules", got, later.ID)
	}
}

// TestRemapPartTwoBreaksTiesAtEachLevel asserts the total order one level at a
// time. Each case leaves exactly one distinguishing field.
func TestRemapPartTwoBreaksTiesAtEachLevel(t *testing.T) {
	dropped := tpl("dropped fragment line", SeverityInfo, 0, 0)

	t.Run("LastSeen descending", func(t *testing.T) {
		a := tpl("survivor a", SeverityInfo, 0, time.Minute)
		b := tpl("survivor b", SeverityInfo, 0, 2*time.Minute)
		remap := mustRemap(t, dropped, a, b)
		if got := remap[dropped.ID]; got != b.ID {
			t.Fatalf("remapped to %q, want the greater LastSeen %q", got, b.ID)
		}
	})

	t.Run("FirstSeen descending when LastSeen ties", func(t *testing.T) {
		a := tpl("survivor a", SeverityInfo, 0, time.Minute)
		b := tpl("survivor b", SeverityInfo, 30*time.Second, time.Minute)
		remap := mustRemap(t, dropped, a, b)
		if got := remap[dropped.ID]; got != b.ID {
			t.Fatalf("remapped to %q, want the greater FirstSeen %q", got, b.ID)
		}
	})

	t.Run("id ascending when both timestamps tie", func(t *testing.T) {
		a := tpl("survivor a", SeverityInfo, 0, time.Minute)
		b := tpl("survivor b", SeverityInfo, 0, time.Minute)
		want := min(b.ID, a.ID)
		remap := mustRemap(t, dropped, a, b)
		if got := remap[dropped.ID]; got != want {
			t.Fatalf("remapped to %q, want the lexicographically smaller id %q", got, want)
		}
	})
}

func mustRemap(t *testing.T, dropped *Template, survivors ...*Template) map[string]string {
	t.Helper()
	before := templatesByID(append([]*Template{dropped}, survivors...))
	after := templatesByID(survivors)
	remap, err := buildTemplateRemap(before, after, nil)
	if err != nil {
		t.Fatalf("buildTemplateRemap: %v", err)
	}
	return remap
}

// TestRemapRefusesWhenNothingSurvives is the orphan arm. It is unreachable with
// a non-empty surviving set, and it is asserted rather than left implicit so a
// change that made it reachable is loud rather than silently dropping entries.
func TestRemapRefusesWhenNothingSurvives(t *testing.T) {
	dropped := tpl("dropped fragment line", SeverityInfo, 0, 0)
	_, err := buildTemplateRemap(templatesByID([]*Template{dropped}), nil, nil)
	if err == nil {
		t.Fatal("a dropped template with no survivor to take its entries was accepted")
	}
	if !contains(err.Error(), dropped.ID) {
		t.Fatalf("the error does not name the dropped template: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestConsolidationLeavesNoDanglingEdgeAndNoOrphanedEntry runs the WHOLE
// produce path over a fixture that consolidates, and asserts the two invariants
// the built-in pipeline violates: every CONTAINS edge names a template that is
// in the batch, and every entry that clustered reaches a surviving template.
func TestConsolidationLeavesNoDanglingEdgeAndNoOrphanedEntry(t *testing.T) {
	entries := crashFixture()
	graph, err := Build(entries, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, edges, err := Emit(graph, nil, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	ids := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		if n.ID == "" {
			t.Fatalf("a %s node carries an empty id", n.Type)
		}
		ids[n.ID] = struct{}{}
	}
	for _, e := range edges {
		if e.Type != EdgeContains && e.Type != EdgeBelongsTo && e.Type != EdgeHasLabel {
			continue
		}
		if _, ok := ids[e.FromID]; !ok {
			t.Fatalf("edge %s from %q names a node that is not in the batch", e.Type, e.FromID)
		}
		if _, ok := ids[e.ToID]; !ok {
			t.Fatalf("edge %s to %q names a node that is not in the batch", e.Type, e.ToID)
		}
	}

	// The consolidation really happened: a merged crash template is present.
	mergedID := TemplateID("Go runtime crash (goroutine dump)")
	if _, ok := ids[mergedID]; !ok {
		t.Fatalf("the fixture did not consolidate; without a merge this test asserts nothing about the remap")
	}
	// And every entry reached a chunk, so none was orphaned.
	totalEntries := 0
	for _, c := range graph.Chunks {
		totalEntries += c.EntryCount
	}
	if totalEntries != len(entries) {
		t.Fatalf("chunks hold %d entries of %d; %d were orphaned by consolidation",
			totalEntries, len(entries), len(entries)-totalEntries)
	}
}

// TestConsolidationIsDeterministicAcrossRuns runs the produce path THIRTY times
// over one fixed input and requires a single id set.
//
// THE NO-DROP CONTROL IS IN THE SAME RUN, and it is what proves the instrument
// would have seen a difference: a fixture that consolidates nothing exercises
// no remap at all, so thirty agreeing runs over it would say nothing. The
// excluded failure is a randomized map walk agreeing with itself thirty times,
// which at two survivors is under one in five hundred million.
func TestConsolidationIsDeterministicAcrossRuns(t *testing.T) {
	const runs = 30

	consolidating := crashFixture()
	first := idSetOf(t, consolidating)
	for i := 1; i < runs; i++ {
		if got := idSetOf(t, consolidating); got != first {
			t.Fatalf("run %d produced a different id set:\n  run 0: %s\n  run %d: %s", i, first, i, got)
		}
	}

	// THE CONTROL: an input that drops nothing must also agree with itself, and
	// its id set must DIFFER from the consolidating one — otherwise both
	// fixtures are exercising the same path and neither says anything about the
	// remap.
	plain := []Entry{
		{Timestamp: at(0), Severity: SeverityInfo, Message: "service checkout started ok"},
		{Timestamp: at(time.Second), Severity: SeverityInfo, Message: "service checkout started fine"},
	}
	control := idSetOf(t, plain)
	for i := 1; i < runs; i++ {
		if got := idSetOf(t, plain); got != control {
			t.Fatalf("the no-drop control run %d disagreed with run 0", i)
		}
	}
	if control == first {
		t.Fatal("the consolidating fixture and the no-drop control produced the same id set; the control is not a control")
	}
}

func idSetOf(t *testing.T, entries []Entry) string {
	t.Helper()
	graph, err := Build(entries, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, _, err := Emit(graph, nil, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.Type+" "+n.ID)
	}
	sortStrings(ids)
	var out strings.Builder
	for _, id := range ids {
		out.WriteString(id + "\n")
	}
	return out.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// goStackLines are four lines of one goroutine dump, each matching a DIFFERENT
// clause of the Go stack matcher and each landing in a different cluster.
//
// THE SECOND PROPERTY IS WHY THEY ARE WRITTEN OUT RATHER THAN GENERATED. Four
// frames of one shape differing only in a function name merge into a SINGLE
// template before consolidation ever runs — every token carrying a digit is a
// wildcard in the parse tree, so they reach one leaf and score above the
// similarity threshold — and one fragment is below the three the consolidator
// needs. A generated fixture consolidates nothing and every assertion about the
// remap passes vacuously.
var goStackLines = []string{
	"goroutine 1 [running]:",                // the goroutine header
	"created by main.serve in goroutine 17", // the created-by clause
	"/Users/build/app/handler.go:42 +0x1c",  // a source reference with a program-counter offset
	"rax 0x1c",                              // a register dump line
}

// crashFixture is a Go runtime crash: four stack frames inside one
// thirty-second window, plus two ordinary lines. It consolidates.
func crashFixture() []Entry {
	labels := map[string]string{"app": "checkout", "instance": "host-3"}
	entries := []Entry{
		{Timestamp: at(0), Severity: SeverityInfo, Message: "service checkout started ok", Labels: labels},
	}
	for i, line := range goStackLines {
		entries = append(entries, Entry{
			Timestamp: at(time.Duration(i+1) * time.Second),
			Severity:  SeverityError,
			Message:   line,
			Labels:    labels,
		})
	}
	entries = append(entries, Entry{
		Timestamp: at(10 * time.Second),
		Severity:  SeverityInfo,
		Message:   "service checkout recovered ok",
		Labels:    labels,
	})
	return entries
}

func TestPythonConsolidatorMergesATracebackBurst(t *testing.T) {
	in := []*Template{
		tpl("Traceback (most recent call last)", SeverityError, 0, 0),
		tpl(`  File "app.py", line 3, in handler`, SeverityError, time.Second, time.Second),
		tpl("app.errors.TimeoutError: upstream timed out", SeverityError, 2*time.Second, 2*time.Second),
		tpl("service checkout started ok", SeverityInfo, 0, 0),
	}
	out := (&pythonTracebackConsolidator{}).Consolidate(in)
	if len(out.Templates) != 2 {
		t.Fatalf("templates = %d, want 2 (the survivor and the merged traceback)", len(out.Templates))
	}
	if len(out.Absorbed) != 1 {
		t.Fatalf("absorbed groups = %d, want 1", len(out.Absorbed))
	}
	for absorber, frags := range out.Absorbed {
		if absorber == "" {
			t.Fatal("the merged traceback carries no id")
		}
		if len(frags) != 3 {
			t.Fatalf("absorbed %d fragments, want 3", len(frags))
		}
	}
}

// TestConsolidatorsThatTouchNothingReportNothing covers the pass-through arm of
// both consolidators over a template set neither recognizes.
func TestConsolidatorsThatTouchNothingReportNothing(t *testing.T) {
	in := []*Template{
		tpl("service checkout started ok", SeverityInfo, 0, 0),
		tpl("cache eviction ran twice", SeverityInfo, time.Second, time.Second),
	}
	out := RunConsolidators(DefaultConsolidators(), in)
	if len(out.Templates) != 2 {
		t.Fatalf("templates = %d, want 2", len(out.Templates))
	}
	if len(out.Absorbed) != 0 {
		t.Fatalf("absorbed = %v, want nothing", out.Absorbed)
	}
}
