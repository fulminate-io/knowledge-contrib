// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe_test.go — THIS MODULE'S DECLARATION, checked against the module's own
// symbols rather than against a literal copy of it.
//
// A ROW THAT RETYPED THE DECLARATION WOULD PROVE NOTHING. The declaration is
// built from these symbols, so asserting it equals them again would be the same
// expression on both sides. What the rows below check is what two DIFFERENT
// symbols say about each other: the environment names this collector enumerates
// against the dispositions recorded for them, and the declaration as a whole
// against the framework gate that refuses a malformed one.

// TestDescribe_IsServable is the gate the collector's own main hits first: the
// framework refuses to build a server for a declaration that is incomplete, so a
// declaration that would fail an operator's install fails here instead.
func TestDescribe_IsServable(t *testing.T) {
	if _, err := framework.NewServer[Params](New()); err != nil {
		t.Fatalf("this collector's declaration must be servable: %v", err)
	}
}

// TestDescribe_EveryEnumeratedEnvNameHasADisposition is the cross-check between
// two different symbols: the names this collector's own environment function
// enumerates, and the map that says what an installer must do with each.
//
// A NAME ADDED TO ONE AND NOT THE OTHER IS THE FAILURE THIS CATCHES, and it is
// the same failure the environment census catches across the repository — held
// here too, so a module author sees it in their own test run rather than in a
// CI leg over someone else's script.
func TestDescribe_EveryEnumeratedEnvNameHasADisposition(t *testing.T) {
	enumerated := DeclaredEnv("linux")
	if len(enumerated) == 0 {
		t.Fatal("this collector enumerates no environment names at all; the census below would pass vacuously")
	}
	for _, name := range enumerated {
		if _, ok := envDispositions[name]; !ok {
			t.Errorf("this collector reads %q and nothing says what an installer must do with it — "+
				"add a disposition row beside the name", name)
		}
	}
}

// TestDescribe_TheDeclaredVocabularyIsWellFormed pins the two properties the
// server's ingest refusal depends on: every declared type is a non-empty name,
// and no type is declared twice.
func TestDescribe_TheDeclaredVocabularyIsWellFormed(t *testing.T) {
	decl := New().Describe()
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
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	}) {
		t.Error("the declared environment must be sorted, so one unchanged collector renders one byte-identical declaration")
	}
}
