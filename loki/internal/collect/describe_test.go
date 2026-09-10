// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/lokiapi"
)

// describe_test.go — THIS MODULE'S DECLARATION, checked against the module's own
// symbols rather than against a literal copy of it.
//
// A ROW THAT RETYPED THE DECLARATION WOULD PROVE NOTHING: the declaration is
// built from these symbols, so asserting it equals them again is the same
// expression on both sides. What the rows below check is what two DIFFERENT
// symbols say about each other, and what the framework's own gate says about the
// whole.
//
// THIS MODULE PUBLISHES NO EXCLUSION TABLE, so there is no
// deliberately-omitted row here; that emptiness is a fact about the collector
// rather than a gap in this file, and the environment census names it in its own
// verdict line.

// TestDescribe_IsServable is the gate this collector's own main hits first: the
// framework refuses to build a server for an incomplete declaration, so a
// declaration that would fail an operator's install fails here instead.
func TestDescribe_IsServable(t *testing.T) {
	if _, err := framework.NewServer[collect.Params](&collect.Collector{}); err != nil {
		t.Fatalf("this collector's declaration must be servable: %v", err)
	}
}

// TestDescribe_EveryEnumeratedEnvNameHasADisposition is the cross-check between
// two different symbols: the names this collector reads for the platform an
// installer targets, and the map that says what an installer must do with each.
func TestDescribe_EveryEnumeratedEnvNameHasADisposition(t *testing.T) {
	names := lokiapi.Names()
	if len(names) == 0 {
		t.Fatal("this collector enumerates no environment names at all; the census below would pass vacuously")
	}
	declared := map[string]string{}
	for _, row := range (&collect.Collector{}).Describe().Environment {
		declared[row.Name] = row.Class
	}
	for _, name := range names {
		if _, ok := declared[name]; !ok {
			t.Errorf("this collector reads %q and its declaration says nothing about it — an installer has no rule "+
				"for the name and an operator cannot tell an omission from a decision", name)
		}
	}
}

// TestDescribe_TheDeclaredVocabularyIsWellFormed pins the two properties the
// server's ingest refusal depends on: every declared type is a non-empty name,
// and no type is declared twice.
func TestDescribe_TheDeclaredVocabularyIsWellFormed(t *testing.T) {
	decl := (&collect.Collector{}).Describe()
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
	if !slices.IsSortedFunc(decl.Environment, func(a, b framework.EnvDeclaration) int {
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
