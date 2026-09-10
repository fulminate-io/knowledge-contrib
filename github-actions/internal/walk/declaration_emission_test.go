// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"slices"
	"sort"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/walk"
)

// declaration_emission_test.go — THE DECLARED VOCABULARY AND THE EMITTED ONE,
// DRIVEN IN ONE PROCESS.
//
// WHY IT DID NOT EXIST AND WHAT THAT COST. This module declared the RESOURCE-KIND
// list on the describe tool's `node_types` while every node its builder produces
// carries the ONE contract node type. Both halves had their own passing tests:
// the enumerations' parity rows check the kind floor against the walk's
// intermediate result, and the describe rows check that a declaration is served
// and is well formed. Neither ever put an EMITTED node beside the DECLARED
// vocabulary, so the contradiction was invisible until the server's
// undeclared-type gate refused the first chunk of every live collect and the
// module was non-functional end to end.
//
// THE SEAM IS THE WALK, not the builder: what the server compares against the
// registered declaration is the node type on the contract node the walk returns,
// so that is what this reads. Both sides are the shipped ones — the shipped
// Describe, and a whole walk over this package's in-process fake.
//
// WHAT IT DELIBERATELY DOES NOT CLAIM. It does not assert that every declared
// EDGE type is emitted: this fixture is one small organization and the whole-
// vocabulary corpus lives with the enumerations, whose parity rows already close
// that direction. What it closes is the direction the defect took — a declared
// set and an emitted set that are not the same set.

// walkTheFixtureOrganization runs one whole walk over this package's fake.
//
// IT IS THIS FILE'S OWN DRIVER rather than the neighboring mustWalk: that
// helper takes the organization and the parameters, and every row in this file
// wants the defaults, so routing through it would add callers that pin its two
// parameters to one value each and say nothing about either.
func walkTheFixtureOrganization(t *testing.T) framework.Result {
	t.Helper()
	api := &recordingAPI{}
	got, err := walk.Collector{API: api.build}.Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walking the fixture organization: %v", err)
	}
	return got
}

func TestEveryEmittedNodeTypeIsDeclaredAndEveryDeclaredOneIsEmitted(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	// THE KNOWN POSITIVE. Every assertion below passes vacuously over a walk that
	// emitted nothing, and a fake that stopped answering would produce exactly
	// that, so the emitted set is required to be non-empty first.
	if len(got.Nodes) == 0 {
		t.Fatal("the fixture walk emitted no nodes at all; the vocabulary assertions below " +
			"would hold over an empty set and say nothing")
	}

	declared := walk.Collector{}.Describe().NodeTypes
	emitted := map[string]int{}
	for _, node := range got.Nodes {
		emitted[node.Type]++
	}

	for _, node := range got.Nodes {
		if !slices.Contains(declared, node.Type) {
			t.Errorf("the walk emitted the node %q with the node type %q, which the describe "+
				"declaration does not name; it declares %v.\n"+
				"The server refuses an undeclared node type by name on the first chunk, so this "+
				"is every collect of this collector failing at ingest.",
				node.ID, node.Type, declared)
			break
		}
	}

	for _, want := range declared {
		if emitted[want] == 0 {
			t.Errorf("the describe declaration names the node type %q, which this walk emits "+
				"on no node; the emitted types are %v.\n"+
				"A declared type nothing produces is a promise to a consumer that this "+
				"collector does not keep.", want, sortedKeys(emitted))
		}
	}
}

func TestEveryEmittedEdgeTypeIsDeclared(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	// The known positive, on the same terms as the node row above.
	if len(got.Edges) == 0 {
		t.Fatal("the fixture walk emitted no edges at all; the assertion below would hold " +
			"over an empty set")
	}

	declared := walk.Collector{}.Describe().EdgeTypes
	for _, edge := range got.Edges {
		if !slices.Contains(declared, edge.Type) {
			t.Errorf("the walk emitted the edge %s -> %s with the edge type %q, which the "+
				"describe declaration does not name; it declares %v.",
				edge.FromID, edge.ToID, edge.Type, declared)
			break
		}
	}
}

// sortedKeys renders an emitted-type histogram's keys in a fixed order so a
// failure reads the same twice.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
