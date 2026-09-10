// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// census_test.go — the IMPORT PURITY CENSUS, which is the carrier for the
// ticket's structural requirement that this module imports nothing of the
// client's or the server's.
//
// THE COMPILER ALREADY REFUSES THOSE IMPORTS, and that is the primary carrier:
// Go's internal-package rule makes them fail to build across a module boundary,
// which is a stronger gate than any test. What the compiler does NOT catch is
// the shape this ticket actually forbids — a file COPIED out of the client with
// its import path rewritten to something that resolves — and what a compile
// cannot state is the ALLOWLIST: that the framework is the one path under our
// own module root this module may require.
//
// SO THE CENSUS ASSERTS THREE REFUSED PREFIXES AND TWO ADMITTED ONES, and it
// fails in BOTH directions: a refused prefix appearing is a failure, and an
// admitted one disappearing is a failure too, so the allowlist cannot rot into a
// permission slip for a dependency that has moved on.
//
// THE ADMITTED SET IS TWO PATHS, NOT ONE, AND NEITHER IS A COLLECTOR. The
// framework, and the common correlation module every logs collector shares. What
// is refused is unchanged in the direction that matters most here: a SIBLING
// COLLECTOR. scripts/collectors-isolation-census.sh asserts that rule across the
// whole workspace, because it needs to know which module a file belongs to and
// a per-module census cannot see the other modules.
//
// WHY IT IS A TEST AND NOT A CORPUS CHECK: a check carries one pattern, so three
// refused prefixes would be three checks, and none of them could carry the
// admitted prefix or the both-directions property that is the whole point.

// refusedImportPrefixes are the paths no file in this module may import.
var refusedImportPrefixes = []string{
	"github.com/fulminate-io/knowledge/cmd/knowledge/",
	"github.com/fulminate-io/knowledge/cmd/knowledge-server/",
	"github.com/fulminate-io/knowledge/gen/",
}

// admittedOurImports are the paths under our own module root this module
// requires. They are asserted PRESENT, which is what stops a census written
// against the bare module root from passing: such a census would refuse these
// too.
var admittedOurImports = []string{
	"github.com/fulminate-io/knowledge-contrib/framework",
	"github.com/fulminate-io/knowledge-contrib/common/correlation",
}

// TestNoFileImportsTheClientOrServerModules is the census.
func TestNoFileImportsTheClientOrServerModules(t *testing.T) {
	imports, files := moduleImports(t)

	for path, where := range imports {
		for _, refused := range refusedImportPrefixes {
			if strings.HasPrefix(path, refused) {
				t.Errorf("%s imports %q, which is under the refused prefix %q", where, path, refused)
			}
		}
	}

	// Each admitted path must be PRESENT, in both directions.
	for _, admitted := range admittedOurImports {
		found := false
		for path := range imports {
			if path == admitted || strings.HasPrefix(path, admitted+"/") {
				found = true
			}
		}
		if !found {
			t.Errorf("no file imports %q; either that dependency was dropped or this census "+
				"is reading the wrong files", admitted)
		}
	}

	// KNOWN POSITIVE: the census read the module. A census over zero files
	// passes exactly as quietly as one over a clean module.
	if files < 10 {
		t.Fatalf("the census read %d files, too few to be this module", files)
	}
	if len(imports) < 5 {
		t.Fatalf("the census found %d distinct imports, too few to be this module", len(imports))
	}
}

// TestTheRefusedPrefixesWouldActuallyFire is the census's own control: the
// matching rule is exercised against a path that must be refused and one that
// must not, so a rule that matched nothing at all is distinguishable from a
// clean module.
func TestTheRefusedPrefixesWouldActuallyFire(t *testing.T) {
	refuse := func(path string) bool {
		for _, prefix := range refusedImportPrefixes {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
		return false
	}
	for _, bad := range []string{
		"github.com/fulminate-io/knowledge/cmd/knowledge/internal/logwire",
		"github.com/fulminate-io/knowledge/cmd/knowledge/internal/collector/logs",
		"github.com/fulminate-io/knowledge/cmd/knowledge-server/internal/store",
		"github.com/fulminate-io/knowledge/gen/knowledge/v1",
	} {
		if !refuse(bad) {
			t.Errorf("%q is not refused", bad)
		}
	}
	// THE NEAR MISSES: the framework and the common correlation module share the
	// module root and must NOT be refused. A census written against the bare root
	// would fail here, which is why the subject is three paths rather than one.
	for _, good := range []string{
		admittedOurImports[0],
		admittedOurImports[0] + "/frameworktest",
		admittedOurImports[1],
		"cloud.google.com/go/logging/logadmin",
		"github.com/klauspost/compress/zstd",
	} {
		if refuse(good) {
			t.Errorf("%q is refused", good)
		}
	}
}

// TestNoFileWritesToStdout is the stdio-protocol census. On the transport this
// collector is installed under, stdout is the JSON-RPC stream: a stray write
// there is a corrupt frame that surfaces as an opaque handshake failure with
// nothing pointing at its cause. Every diagnostic goes through logDiagnostic,
// which writes to stderr.
func TestNoFileWritesToStdout(t *testing.T) {
	names := moduleFiles(t)
	found := 0
	for _, name := range names {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		text := string(body)
		for _, banned := range []string{"fmt.Print", "println(", "os.Stdout"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s names %q; stdout is the protocol stream on this collector's transport", name, banned)
			}
		}
		found++
	}
	// KNOWN POSITIVE: the census read files, and the one legitimate stderr
	// writer is still there.
	if found < 10 {
		t.Fatalf("the census read %d files, too few to be this module", found)
	}
	body, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatalf("reading client.go: %v", err)
	}
	if !strings.Contains(string(body), "os.Stderr") {
		t.Fatalf("client.go no longer writes diagnostics to stderr; this census is reading the wrong file")
	}
}

// moduleImports returns every import path in this module's NON-TEST files,
// mapped to the file that carries it.
func moduleImports(t *testing.T) (map[string]string, int) {
	t.Helper()
	out := make(map[string]string)
	names := moduleFiles(t)
	fset := token.NewFileSet()
	for _, name := range names {
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquoting %s: %v", name, spec.Path.Value, err)
			}
			out[path] = name
		}
	}
	return out, len(names)
}

// moduleFiles lists this module's non-test Go files at the module root.
//
// IT DOES NOT RECURSE, and the reason is worth stating: the only subdirectory is
// testdata, which holds the parity golden's generator. That file imports the
// client on purpose and runs from inside the client module, so a recursive
// census would report it as a violation of the very rule it exists to serve.
func moduleFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the module root: %v", err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	return names
}
