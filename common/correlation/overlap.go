// SPDX-License-Identifier: Apache-2.0

package correlation

import "time"

// overlap.go — THE TEMPORAL HALF: how much two templates' ranges share, and
// whether they share anything at all.
//
// THE PADDING IS WHAT MAKES A POINT EVENT CORRELATABLE. A template seen once has
// FirstSeen equal to LastSeen and a zero-length range, which intersects nothing;
// padding both sides by half the window lets two single-shot errors seconds
// apart still meet. The score is the shared interval over the WIDER of the two
// ranges, so a long noisy range does not score highly against a brief one it
// happens to contain.

// defaultCorrelationWindow is how far two templates' ranges may drift and still
// count as co-occurring. It absorbs the clock skew between two services' log
// ingestors without admitting unrelated background noise.
//
// IT IS A SEMANTIC PARAMETER, NOT A CAP. It bounds no result count and truncates
// nothing: the detector returns every scored pair it builds.
const defaultCorrelationWindow = 60 * time.Second

// temporalOverlap scores how much two templates' ranges share, after padding
// each by half the window, and reports whether they overlap at all.
//
// A WINDOW AT OR BELOW ZERO TAKES THE DEFAULT, which is the parity target's own
// behaviour (pipeline_correlation.go:290-292) and is kept deliberately: a
// zero-width window would make every pair disjoint and silently empty the
// correlation set. The window is not caller-supplied on this module's exported
// surface, so the arm is reachable only from inside the package.
func temporalOverlap(a, b *Template, window time.Duration) (float64, bool) {
	if a == nil || b == nil {
		return 0, false
	}
	if window <= 0 {
		window = defaultCorrelationWindow
	}
	startA, endA := expandRange(a.FirstSeen, a.LastSeen, window)
	startB, endB := expandRange(b.FirstSeen, b.LastSeen, window)
	if endA.Before(startB) || endB.Before(startA) {
		return 0, false
	}
	overlapStart := startA
	if startB.After(overlapStart) {
		overlapStart = startB
	}
	overlapEnd := endA
	if endB.Before(overlapEnd) {
		overlapEnd = endB
	}
	wider := maxDuration(endA.Sub(startA), endB.Sub(startB)).Seconds()
	if wider <= 0 {
		// Both ranges are degenerate and they intersect, so they are the same
		// instant: a full score rather than a division by zero.
		return 1, true
	}
	return clampUnit(overlapEnd.Sub(overlapStart).Seconds() / wider), true
}

// expandRange pads a range by half the window on each side. A range with a zero
// endpoint passes through unpadded: an unknown bound is not a bound to widen.
func expandRange(first, last time.Time, window time.Duration) (time.Time, time.Time) {
	if first.IsZero() || last.IsZero() {
		return first, last
	}
	pad := window / 2
	return first.Add(-pad), last.Add(pad)
}

// maxDuration returns the larger of two durations.
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// clampUnit bounds a score to [0, 1] so the edge's confidence is always a
// proportion. It is not reachable through temporalOverlap on well-formed ranges
// — the shared span of two intersecting ranges never exceeds the wider of them —
// and it stays as the guard for whatever scores a pair next.
func clampUnit(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
