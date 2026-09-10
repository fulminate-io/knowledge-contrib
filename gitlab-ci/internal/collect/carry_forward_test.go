// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// carry_forward_test.go — two collects of a group that did not change produce
// the same bytes.
//
// IT IS A SEPARATE FILE FROM THE COVERAGE ASSERTION because it rests on a
// different property: the parity rows ask whether the declared vocabulary is
// emitted at all, and this one asks whether the ORDER the enumerations happened
// to run in reaches the output. The second order is asserted to differ from the
// first, which is the whole reason the row discriminates.

// TestTwoCollectsOfAnUnchangedGroupAreIdentical is the carry-forward row.
//
// THE SECOND ORDER IS ASSERTED TO DIFFER FROM THE FIRST, and the whole row rests
// on that. A shuffle of seven elements returns the identity permutation once in
// 5040, and on that run this compares one fixed order against the same fixed
// order — which passes with the graph builder's sort deleted, the exact defect
// the shuffle was introduced to prevent. So the order is captured before and
// after and the run fails if the shuffle moved nothing.
func TestTwoCollectsOfAnUnchangedGroupAreIdentical(t *testing.T) {
	first := walkTheFixtureGroup(t)

	var before, after []string
	second := walkTheFixtureGroupInOrder(t, fixtureAPI(), func(subs []collect.Subcollector) {
		before = enumerationOrder(subs)
		for attempt := range 100 {
			rand.Shuffle(len(subs), func(i, j int) { subs[i], subs[j] = subs[j], subs[i] })
			if after = enumerationOrder(subs); !slices.Equal(before, after) {
				return
			}
			if attempt == 99 {
				t.Fatalf("100 shuffles of %d enumerations all returned the original order; the "+
					"shuffle is not shuffling", len(subs))
			}
		}
	})
	if slices.Equal(before, after) {
		t.Fatalf("the second walk ran the enumerations in the same order as the first (%v), so "+
			"this row would pass with the graph builder's sort deleted", after)
	}

	firstNodes, firstEdges, err := glgraph.Build(first)
	if err != nil {
		t.Fatalf("building the first collect: %v", err)
	}
	secondNodes, secondEdges, err := glgraph.Build(second)
	if err != nil {
		t.Fatalf("building the second collect: %v", err)
	}

	// COMPARED AS THE BYTES THAT GO ON THE WIRE, which is what "changes nothing"
	// means to the receiving server: it compares generations, not Go values. The
	// node struct carries a map and is not comparable in Go anyway, and a
	// field-by-field comparison would silently stop covering a field added later.
	if a, b := marshal(t, firstNodes), marshal(t, secondNodes); a != b {
		t.Errorf("two collects of an unchanged group produced different nodes:\n%s\nvs\n%s", a, b)
	}
	if a, b := marshal(t, firstEdges), marshal(t, secondEdges); a != b {
		t.Errorf("two collects of an unchanged group produced different edges:\n%s\nvs\n%s", a, b)
	}

	// The known positive: an empty walk would compare equal to itself. The fixture
	// group is not empty, and this says so in the same run.
	if len(firstNodes) == 0 || len(firstEdges) == 0 {
		t.Fatalf("the fixture walk produced %d nodes and %d edges; two empty results are equal for "+
			"the wrong reason", len(firstNodes), len(firstEdges))
	}
}

// enumerationOrder is the order a list of enumerations is in, by name.
func enumerationOrder(subs []collect.Subcollector) []string {
	out := make([]string, 0, len(subs))
	for _, sub := range subs {
		out = append(out, sub.Name)
	}
	return out
}

// marshal renders a value as the JSON the envelope carries.
func marshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling a collect result: %v", err)
	}
	return string(raw)
}
