// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/walk"
)

// describe_test.go — THIS MODULE'S DECLARATION, checked against the module's own
// symbols rather than against a literal copy of it.
//
// A ROW THAT RETYPED THE DECLARATION WOULD PROVE NOTHING. The declaration is
// built from these symbols, so asserting it equals them again would be the same
// expression on both sides. What the rows below check is what two DIFFERENT
// symbols say about each other, and what the framework gate says about the whole.

// TestDescribe_IsServable is the gate this collector's own main hits first: the
// framework refuses to build a server for an incomplete declaration, so a
// declaration that would fail an operator's install fails here instead.
func TestDescribe_IsServable(t *testing.T) {
	if _, err := framework.NewServer[walk.Params](walk.Collector{}); err != nil {
		t.Fatalf("this collector's declaration must be servable: %v", err)
	}
}

// TestDescribe_EveryEnvironmentNameCarriesItsClass is the cross-check between
// two different symbols: the names this collector reads, and the map that says
// what an installer must do with each. A name added to one and not the other is
// the failure this catches.
func TestDescribe_EveryEnvironmentNameCarriesItsClass(t *testing.T) {
	declared := walk.Collector{}.Describe().Environment
	names := enventry.Names()
	if len(names) == 0 {
		t.Fatal("this collector enumerates no environment names at all; the row below would pass vacuously")
	}
	if len(declared) != len(names) {
		t.Fatalf("the declaration carries %d environment rows and this collector reads %d names", len(declared), len(names))
	}
	classes := enventry.Classes()
	for _, row := range declared {
		if !slices.Contains(names, row.Name) {
			t.Errorf("the declaration names %q, which this collector's own environment source does not read", row.Name)
			continue
		}
		if want := string(classes[row.Name]); row.Class != want {
			t.Errorf("the declaration gives %q the class %q; this collector's own table says %q", row.Name, row.Class, want)
		}
	}
	if !slices.IsSortedFunc(declared, func(a, b framework.EnvDeclaration) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	}) {
		t.Error("the declared environment must be sorted, so one unchanged collector renders one byte-identical declaration")
	}
}

// TestDescribe_DeclaresThePersonEdgesAndOnlyTheContractNodeType is R3, and both
// halves are written as LITERALS because both are contracts with the server
// rather than restatements of this module's own symbols.
//
// THE EDGE HALF. The server refuses an undeclared EDGE type by name on the first
// chunk, exactly as it refuses an undeclared node type, so a class emitted by the
// walk and missing from this list is every collect of this collector failing at
// ingest. Declaring one means growing the edge vocabulary; the declaration is
// read from it and needs no edit of its own, which is precisely why nothing
// would have noticed a class added to the walk and not to the vocabulary.
//
// THE NODE HALF IS AN ABSENCE, AND IT IS THE MORE IMPORTANT OF THE TWO. This
// collector emits ONE contract node type and puts the kind of thing each resource
// is in metadata. Declaring the kinds here published a vocabulary no node ever
// carries and the server's gate refused the first chunk of every collect; adding
// `user` to this list because a user is now a first-class thing in the graph
// would reproduce that defect exactly.
func TestDescribe_DeclaresThePersonEdgesAndOnlyTheContractNodeType(t *testing.T) {
	decl := walk.Collector{}.Describe()

	if want := []string{"cicd-resource"}; !slices.Equal(decl.NodeTypes, want) {
		t.Errorf("the declaration names the node types %v, want exactly %v.\n"+
			"The kinds — user among them — ride the resource_type metadata key on every node, "+
			"and a kind declared here is a type the server's gate refuses on ingest because "+
			"no node carries it.", decl.NodeTypes, want)
	}

	for _, want := range []string{"ATTRIBUTED_TO", "INITIATED_BY", "CREATED_BY"} {
		if !slices.Contains(decl.EdgeTypes, want) {
			t.Errorf("the declaration does not name the edge type %q; the walk emits it and the "+
				"server refuses an undeclared edge type by name. It declares %v",
				want, decl.EdgeTypes)
		}
	}
	// The known positive for the matcher: a class this collector does not emit is
	// absent, so an assertion that passed against any declaration would not pass
	// here.
	if slices.Contains(decl.EdgeTypes, "RUNS_IN") {
		t.Error("the declaration names RUNS_IN, which nothing this collector reads can produce")
	}
}

// TestDescribe_TheDeclaredVocabularyIsWellFormed pins the two properties the
// server's ingest refusal depends on: every declared type is a non-empty name,
// and no type is declared twice.
func TestDescribe_TheDeclaredVocabularyIsWellFormed(t *testing.T) {
	decl := walk.Collector{}.Describe()
	if len(decl.NodeTypes) == 0 {
		t.Fatal("a collector that declares no node type refuses every node it emits at ingest")
	}
	for _, pair := range []struct {
		label string
		types []string
	}{{"node", decl.NodeTypes}, {"edge", decl.EdgeTypes}} {
		seen := map[string]struct{}{}
		for _, ty := range pair.types {
			if ty == "" {
				t.Errorf("the declared %s vocabulary carries an empty type name", pair.label)
			}
			if _, dup := seen[ty]; dup {
				t.Errorf("the declared %s vocabulary names %q twice", pair.label, ty)
			}
			seen[ty] = struct{}{}
		}
	}
}
