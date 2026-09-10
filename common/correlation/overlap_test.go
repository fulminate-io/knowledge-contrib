// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"testing"
	"time"
)

// overlap_test.go — THE INPUT CLASSES THE SPECIFICATION NAMES, one arm each,
// every expectation computed from the fixture rather than read back from the
// code under test.

// TestFindCorrelations_FewerThanTwoErrorTemplates covers the two empty classes:
// no templates at all, and one error template. Both return no results and no
// error — a well-formed empty input is an empty result, not a refusal.
func TestFindCorrelations_FewerThanTwoErrorTemplates(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api := streamFor("api")
	only := templateAt("a", SeverityError, base, time.Minute)

	for _, tc := range []struct {
		name string
		in   Input
	}{
		{"no templates at all", Input{}},
		{"one error template", Input{
			Templates: []*Template{only},
			Chunks:    []*Chunk{chunkFor(api, only)},
			Streams:   []*Stream{api},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, err := FindCorrelations(tc.in)
			if err != nil {
				t.Fatalf("FindCorrelations: %v", err)
			}
			if len(results) != 0 {
				t.Errorf("expected no results, got %d", len(results))
			}
			// NIL, NOT AN EMPTY SLICE. The two early floors are what produce it,
			// and this is the only thing that observes them: without both, the
			// pairing loop returns a zero-length allocated slice instead, which
			// the parity target never does.
			if results != nil {
				t.Errorf("an input with nothing to correlate returns nil, got %#v", results)
			}
		})
	}
}

// TestFindCorrelations_OneAttributableTemplate is the second floor: two error
// templates exist, but only one is attributable to a service, so there are fewer
// than two attributed templates and no pair can be built.
func TestFindCorrelations_OneAttributableTemplate(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api := streamFor("api")
	unlabelled := &Stream{ID: "stream-none", Labels: map[string]string{"pod": "x-1"}}
	a := templateAt("a", SeverityError, base, time.Minute)
	b := templateAt("b", SeverityError, base, time.Minute)

	results, err := FindCorrelations(Input{
		Templates: []*Template{a, b},
		Chunks:    []*Chunk{chunkFor(api, a), chunkFor(unlabelled, b)},
		Streams:   []*Stream{api, unlabelled},
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("a template no stream attributes to a service cannot pair, got %+v", results)
	}
}

// TestTemporalOverlap_ZeroWindowFallsBackToTheDefault pins the one coercion the
// parity target has and this module keeps: an unset window is the default rather
// than a zero-width one, which would make every pair disjoint.
func TestTemporalOverlap_ZeroWindowFallsBackToTheDefault(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	a := templateAt("a", SeverityError, base, 0)
	b := templateAt("b", SeverityError, base.Add(20*time.Second), 0)

	zeroScore, zeroOK := temporalOverlap(a, b, 0)
	defScore, defOK := temporalOverlap(a, b, defaultCorrelationWindow)
	if !zeroOK || !defOK {
		t.Fatalf("both runs must overlap: zero=%v default=%v", zeroOK, defOK)
	}
	if zeroScore != defScore {
		t.Errorf("a zero window scored %f, the default scored %f", zeroScore, defScore)
	}
	// A NEGATIVE WINDOW TAKES THE SAME ARM, which is what the `<= 0` guard says.
	if negScore, negOK := temporalOverlap(a, b, -time.Hour); !negOK || negScore != defScore {
		t.Errorf("a negative window scored %f/%v, the default scored %f", negScore, negOK, defScore)
	}
}

// TestExpandRange_ZeroBoundPassesThroughUnpadded is the unknown-bound class: an
// unset timestamp is not a bound to widen, so it is left where it is.
func TestExpandRange_ZeroBoundPassesThroughUnpadded(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)

	first, last := expandRange(base, base.Add(time.Minute), time.Minute)
	if !first.Equal(base.Add(-30*time.Second)) || !last.Equal(base.Add(90*time.Second)) {
		t.Errorf("a whole range pads by half the window on each side: got %v..%v", first, last)
	}
	for _, tc := range []struct{ first, last time.Time }{
		{time.Time{}, base},
		{base, time.Time{}},
		{time.Time{}, time.Time{}},
	} {
		gotFirst, gotLast := expandRange(tc.first, tc.last, time.Minute)
		if !gotFirst.Equal(tc.first) || !gotLast.Equal(tc.last) {
			t.Errorf("expandRange(%v, %v) padded an unknown bound: got %v..%v",
				tc.first, tc.last, gotFirst, gotLast)
		}
	}
}

// TestTemporalOverlap_DegenerateRangesScoreOne is the divide-by-zero class: two
// ranges of zero width that intersect are the same instant, which scores 1
// rather than dividing by a zero wider-range.
func TestTemporalOverlap_DegenerateRangesScoreOne(t *testing.T) {
	a := &Template{ID: "a", Severity: SeverityError}
	b := &Template{ID: "b", Severity: SeverityError}
	score, ok := temporalOverlap(a, b, testWindow)
	if !ok || score != 1 {
		t.Errorf("two unset ranges are the same instant: score=%f ok=%v", score, ok)
	}
}

// TestTemporalOverlap_NilTemplateDoesNotOverlap is the arm the detector's own
// nil refusal makes unreachable from outside, kept because temporalOverlap is
// also the module's own helper.
func TestTemporalOverlap_NilTemplateDoesNotOverlap(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	a := templateAt("a", SeverityError, base, time.Minute)
	if _, ok := temporalOverlap(a, nil, testWindow); ok {
		t.Error("a nil template overlaps nothing")
	}
	if _, ok := temporalOverlap(nil, a, testWindow); ok {
		t.Error("a nil template overlaps nothing")
	}
}

// TestClampUnit bounds the score to a proportion. THE CLAMP IS NOT REACHABLE
// THROUGH temporalOverlap ON WELL-FORMED RANGES — the shared span of two
// intersecting ranges never exceeds the wider of them — so it is exercised at its
// own level, which is the honest place for a guard that exists against a future
// scorer rather than against today's arithmetic.
func TestClampUnit(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{-1, 0}, {0, 0}, {0.5, 0.5}, {1, 1}, {1.5, 1},
	} {
		if got := clampUnit(tc.in); got != tc.want {
			t.Errorf("clampUnit(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestSeverityAtLeast pins the rank vocabulary the ERROR filter reads. An
// unknown name ranks below TRACE, so it never satisfies a minimum: an unmapped
// level is not evidence of an error.
func TestSeverityAtLeast(t *testing.T) {
	ordered := []string{SeverityTrace, SeverityDebug, SeverityInfo, SeverityWarn, SeverityError, SeverityCritical}
	for i, low := range ordered {
		for j, high := range ordered {
			if got, want := SeverityAtLeast(high, low), j >= i; got != want {
				t.Errorf("SeverityAtLeast(%q, %q) = %v, want %v", high, low, got, want)
			}
		}
	}
	if SeverityAtLeast("NOT-A-LEVEL", SeverityError) {
		t.Error("an unknown severity must not satisfy the ERROR minimum")
	}
	if SeverityAtLeast("", SeverityError) {
		t.Error("an empty severity must not satisfy the ERROR minimum")
	}
	if !SeverityAtLeast("NOT-A-LEVEL", SeverityTrace) {
		t.Error("an unknown severity ranks zero, which is at or above TRACE")
	}
}
