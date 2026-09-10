// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"testing"
	"time"
)

// sort_test.go — THE SORT KEY, which nothing else in this workspace observes.
//
// (ServiceA, ServiceB, TemplateA) is what makes the returned slice independent
// of the order the candidate pass happened to build pairs in, and therefore what
// makes a collect's emitted edge set reproducible. The stackdriver parity golden
// CANNOT observe it: that golden carries exactly one CORRELATES_WITH edge, and a
// single edge has no order, so a detector that dropped the sort entirely would
// still produce a byte-identical golden. This arm is the only thing standing
// between that and a nondeterministic edge set in whichever collector first
// emits two correlations.

// TestFindCorrelations_SortsByServiceAServiceBTemplateA drives a fixture whose
// FIVE confirmed pairs differ on each key component in turn, and asserts the
// whole order against a hand-written literal list.
//
// EACH COMPONENT IS LOAD-BEARING IN THIS FIXTURE, which is what makes the arm
// red for a detector that sorts on fewer than three or in the wrong precedence:
// rows 1↔2 and 3↔4 differ only in TemplateA, rows 2↔3 only in ServiceB, and rows
// 4↔5 only in ServiceA. The build order is deliberately NOT the sorted order.
func TestFindCorrelations_SortsByServiceAServiceBTemplateA(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	alpha, bravo, charlie := streamFor("alpha"), streamFor("bravo"), streamFor("charlie")

	// Two alpha templates whose ids sort in the OPPOSITE order to their position
	// in the slice, so a detector that returned build order fails on rows 1↔2.
	a9 := templateAt("tpl-9", SeverityError, base, 5*time.Minute)
	a4 := templateAt("tpl-4", SeverityError, base, 5*time.Minute)
	b2 := templateAt("tpl-2", SeverityError, base, 5*time.Minute)
	c7 := templateAt("tpl-7", SeverityError, base, 5*time.Minute)

	in := Input{
		Templates: []*Template{a9, a4, b2, c7},
		Chunks: []*Chunk{
			chunkFor(alpha, a9), chunkFor(alpha, a4),
			chunkFor(bravo, b2), chunkFor(charlie, c7),
		},
		Streams: []*Stream{alpha, bravo, charlie},
		ProxyMap: map[string]string{
			"alpha": "acct:arn:alpha", "bravo": "acct:arn:bravo", "charlie": "acct:arn:charlie",
		},
		Resolver: newResolver(map[string]string{
			"alpha": "arn:alpha", "bravo": "arn:bravo", "charlie": "arn:charlie",
		}),
		Oracle: newOracle(
			[2]string{"arn:alpha", "arn:bravo"},
			[2]string{"arn:alpha", "arn:charlie"},
			[2]string{"arn:bravo", "arn:charlie"},
		),
	}

	want := [][3]string{
		{"alpha", "bravo", "tpl-4"},
		{"alpha", "bravo", "tpl-9"},
		{"alpha", "charlie", "tpl-4"},
		{"alpha", "charlie", "tpl-9"},
		{"bravo", "charlie", "tpl-2"},
	}

	// RUN TWICE IN ONE TEST. The candidate pass reads a map, so a dependence on
	// map iteration order shows up as a disagreement between two runs of one
	// fixture rather than as a rare flake in CI.
	for run := 1; run <= 2; run++ {
		results, err := FindCorrelations(in)
		if err != nil {
			t.Fatalf("run %d: FindCorrelations: %v", run, err)
		}
		if len(results) != len(want) {
			t.Fatalf("run %d: expected %d pairs, got %d: %+v", run, len(want), len(results), results)
		}
		for i, w := range want {
			got := [3]string{results[i].ServiceA, results[i].ServiceB, results[i].TemplateA}
			if got != w {
				t.Errorf("run %d: row %d = %v, want %v (full order %+v)", run, i, got, w, results)
			}
		}
		for i, r := range results {
			if !r.StructurallyConfirmed {
				t.Errorf("run %d: row %d is unconfirmed, so the fixture is not exercising confirmed pairs: %+v", run, i, r)
			}
		}
	}
}
