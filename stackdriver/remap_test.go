// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"
)

// remap_test.go — the two-part remap rule, one arm per part and one cell per
// tie-break level.

// TestRemapPrefersTheAbsorberOverTheMostRecentSurvivor is PART ONE, and it is
// the case a total order alone gets wrong. A merged template's LastSeen is the
// maximum over its group, so an unrelated template that fired AFTER the burst
// has a greater LastSeen — a pure recency rule deterministically remaps the
// crash's fragments onto that unrelated template.
func TestRemapPrefersTheAbsorberOverTheMostRecentSurvivor(t *testing.T) {
	fragment := mustTemplate("goroutine 42 [running]:", severityError, 0, time.Second)
	absorber := mustTemplate(goStackMergedPattern, severityCritical, 0, time.Second)
	later := mustTemplate("request served in <*> ms", severityInfo, time.Minute, time.Hour)

	remap := buildTemplateRemap(
		[]*logTemplate{fragment, later},
		consolidation{
			Templates: []*logTemplate{absorber, later},
			Absorbed:  map[string][]string{absorber.ID: {fragment.ID}},
		},
	)
	if got := remap[fragment.ID]; got != absorber.ID {
		if got == later.ID {
			t.Fatalf("the fragment remapped onto the unrelated later template; "+
				"the absorber (%s) must win over recency", absorber.ID)
		}
		t.Fatalf("the fragment remapped to %q, want the absorber %s", got, absorber.ID)
	}
}

// TestRemapFallsBackToTheTotalOrderWhenNothingAbsorbed is PART TWO's first
// level: with no absorber, the survivor greatest under LastSeen descending wins.
func TestRemapFallsBackToTheTotalOrderWhenNothingAbsorbed(t *testing.T) {
	dropped := mustTemplate("dropped", severityInfo, 0, time.Second)
	oldest := templateWithID("aaa", 0, time.Second)
	newest := templateWithID("bbb", 0, time.Hour)
	middle := templateWithID("ccc", 0, time.Minute)

	remap := buildTemplateRemap(
		[]*logTemplate{dropped, oldest, newest, middle},
		consolidation{Templates: []*logTemplate{oldest, newest, middle}},
	)
	if got := remap[dropped.ID]; got != newest.ID {
		t.Fatalf("remap target = %q, want the greatest LastSeen %q", got, newest.ID)
	}
}

// TestRemapBreaksATiedLastSeenOnFirstSeen is PART TWO's second level. A module
// that sorts on LastSeen alone passes the level above and fails this one.
func TestRemapBreaksATiedLastSeenOnFirstSeen(t *testing.T) {
	dropped := mustTemplate("dropped", severityInfo, 0, time.Second)
	earlierStart := templateWithID("aaa", 0, time.Hour)
	laterStart := templateWithID("bbb", time.Minute, time.Hour)

	remap := buildTemplateRemap(
		[]*logTemplate{dropped, earlierStart, laterStart},
		consolidation{Templates: []*logTemplate{earlierStart, laterStart}},
	)
	if got := remap[dropped.ID]; got != laterStart.ID {
		t.Fatalf("remap target = %q, want the later FirstSeen %q", got, laterStart.ID)
	}
}

// TestRemapBreaksABothTiedPairOnTheIDAscending is PART TWO's third level, and it
// is what makes the order TOTAL rather than merely usually decisive. Without it
// two survivors tied on both timestamps are decided by whatever order the slice
// happens to be in.
func TestRemapBreaksABothTiedPairOnTheIDAscending(t *testing.T) {
	dropped := mustTemplate("dropped", severityInfo, 0, time.Second)
	lowID := templateWithID("aaa", time.Minute, time.Hour)
	highID := templateWithID("zzz", time.Minute, time.Hour)

	// Both slice orders must give the same answer, which is what "total" means.
	for _, survivors := range [][]*logTemplate{{lowID, highID}, {highID, lowID}} {
		remap := buildTemplateRemap(
			[]*logTemplate{dropped, lowID, highID},
			consolidation{Templates: survivors},
		)
		if got := remap[dropped.ID]; got != lowID.ID {
			t.Fatalf("remap target = %q, want the lowest id %q", got, lowID.ID)
		}
	}
}

// TestRemapIsEmptyWhenNothingWasDropped is the same-run control for every arm
// above: the rule fires only on a drop.
func TestRemapIsEmptyWhenNothingWasDropped(t *testing.T) {
	kept := mustTemplate("kept", severityInfo, 0, time.Second)
	if remap := buildTemplateRemap([]*logTemplate{kept}, consolidation{Templates: []*logTemplate{kept}}); remap != nil {
		t.Fatalf("a no-drop consolidation produced the remap %v", remap)
	}
}

// TestRemapIsNilWhenConsolidationLeftNothing is the degenerate arm: with no
// survivor there is nothing to map to, and the entries keep no dead id because
// the pipeline drops an id that is not in the live set.
func TestRemapIsNilWhenConsolidationLeftNothing(t *testing.T) {
	dropped := mustTemplate("dropped", severityInfo, 0, time.Second)
	if remap := buildTemplateRemap([]*logTemplate{dropped}, consolidation{}); remap != nil {
		t.Fatalf("remap = %v, want nil when nothing survived", remap)
	}
}

// TestRemapIgnoresAnAbsorberThatIsNoLongerInTheSet covers the composition case:
// a survivor recorded as an absorber but since folded away must not be a target,
// and the total order decides instead.
func TestRemapIgnoresAnAbsorberThatIsNoLongerInTheSet(t *testing.T) {
	dropped := mustTemplate("dropped", severityInfo, 0, time.Second)
	survivor := templateWithID("aaa", 0, time.Hour)

	remap := buildTemplateRemap(
		[]*logTemplate{dropped, survivor},
		consolidation{
			Templates: []*logTemplate{survivor},
			Absorbed:  map[string][]string{"a-template-that-was-folded-away": {dropped.ID}},
		},
	)
	if got := remap[dropped.ID]; got != survivor.ID {
		t.Fatalf("remap target = %q, want the live survivor %q", got, survivor.ID)
	}
}

// TestComposedAbsorptionCarriesFragmentsForwardThroughASecondFold is the reason
// runConsolidators rewrites rather than concatenates: a later pass absorbing an
// earlier pass's merged template must inherit that template's own fragments, or
// they point at something no longer in the set.
func TestComposedAbsorptionCarriesFragmentsForwardThroughASecondFold(t *testing.T) {
	first := &stubPass{
		out: consolidation{
			Templates: []*logTemplate{templateWithID("merged-1", 0, time.Minute)},
			Absorbed:  map[string][]string{"merged-1": {"frag-a", "frag-b"}},
		},
	}
	second := &stubPass{
		out: consolidation{
			Templates: []*logTemplate{templateWithID("merged-2", 0, time.Hour)},
			Absorbed:  map[string][]string{"merged-2": {"merged-1"}},
		},
	}
	out := runConsolidators([]consolidatorPass{first, second}, nil)

	got := out.Absorbed["merged-2"]
	want := map[string]bool{"frag-a": true, "frag-b": true, "merged-1": true}
	if len(got) != len(want) {
		t.Fatalf("merged-2 absorbed %v, want the three of %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("merged-2 absorbed the unexpected %q", id)
		}
	}
	if _, stale := out.Absorbed["merged-1"]; stale {
		t.Errorf("merged-1 is still recorded as an absorber after being absorbed itself")
	}
}

// TestFoldDuplicateTemplatesUnionsAndSorts covers the fold's own arithmetic.
func TestFoldDuplicateTemplatesUnionsAndSorts(t *testing.T) {
	a := mustTemplate("same pattern", severityInfo, 0, time.Minute)
	b := mustTemplate("same pattern", severityCritical, -time.Minute, time.Hour)
	c := mustTemplate("other pattern", severityInfo, 0, time.Minute)

	out := foldDuplicateTemplates([]*logTemplate{a, b, c})
	if len(out) != 2 {
		t.Fatalf("fold produced %d templates, want 2: %s", len(out), patternsOf(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].ID >= out[i].ID {
			t.Errorf("the fold's output is not sorted by id")
		}
	}
	folded := findByPattern(out, "same pattern")
	if folded.Count != 2 {
		t.Errorf("count = %d, want the union 2", folded.Count)
	}
	if folded.Severity != severityCritical {
		t.Errorf("severity = %s, want the higher %s", folded.Severity, severityCritical)
	}
	if folded.Alias != "same-pattern@crit" {
		t.Errorf("alias = %q, want it re-derived after the severity rose", folded.Alias)
	}
	if !folded.FirstSeen.Equal(baseTime.Add(-time.Minute)) || !folded.LastSeen.Equal(baseTime.Add(time.Hour)) {
		t.Errorf("range = %v..%v, want the union", folded.FirstSeen, folded.LastSeen)
	}
}

// stubPass is a consolidator that returns a fixed consolidation, for the
// composition arm above.
type stubPass struct{ out consolidation }

func (s *stubPass) Name() string                             { return "stub" }
func (s *stubPass) Consolidate([]*logTemplate) consolidation { return s.out }
