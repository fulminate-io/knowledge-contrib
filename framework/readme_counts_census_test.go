// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// readme_counts_census_test.go — THE CENSUS HALF of the README's count gate,
// split from readme_test.go for that file's length limit.
//
// Each helper here MEASURES one quantity the published document states in
// words: how many schema documents the contract is, how many properties those
// documents require between them, and how many exported Serve entry points this
// module offers. A count spelled as a word ("two documents", "four
// properties", "the two entry points") is exactly as capable of going stale as
// a digit, and until this file existed nothing observed the three. Every helper
// FAILS rather than returning zero when it finds nothing, because a zero census
// would make the sentence it feeds true by vacuity.

// contractDocumentCount counts the schema documents the contract directory
// holds. It reads the DIRECTORY rather than the three embedded accessors, so a
// fourth document added beside them counts here the moment it lands, which is
// the only way this number stays measured rather than transcribed.
func contractDocumentCount(t *testing.T) int {
	t.Helper()
	names := contractDocumentNames(t)
	return len(names)
}

func contractDocumentNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("contract")
	if err != nil {
		t.Fatalf("reading the contract directory, which the README counts: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, filepath.Join("contract", entry.Name()))
	}
	if len(names) == 0 {
		t.Fatal("the contract directory holds no schema document, so the count the README states cannot be checked")
	}
	return names
}

// contractRequiredPropertyCount sums the contract documents' own top-level
// required lists. A document that declares none is a failure rather than a
// zero: a contract that requires nothing would make the sentence this feeds
// true by vacuity.
func contractRequiredPropertyCount(t *testing.T) int {
	t.Helper()
	total := 0
	for _, name := range contractDocumentNames(t) {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		total += len(requiredNames(t, decode(t, raw)))
	}
	return total
}

// serveEntryPoints censuses this module's exported Serve entry points, by
// parsing its non-test source rather than by naming them here — a list typed
// here would be a second transcription that drifts with the one in the README.
func serveEntryPoints(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the module directory: %v", err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s for the entry-point census: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Serve") {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("the entry-point census found no exported Serve function, so the count the README states " +
			"would be checked against nothing")
	}
	sort.Strings(names)
	return names
}
