// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// census_test.go — the two SOURCE censuses. Each carries its own known positive,
// because a census that read nothing passes exactly as loudly as one that read
// everything and found nothing wrong.

// TestTheFixtureCollectorCarriesNoMCPOrEnvelopeCode is row 6, over the only
// collector this module holds. "A collector author writes the walk and a params
// type and nothing else" is a requirement of this ticket; a compile is not
// evidence of it, because a collector that DID carry MCP code would compile just
// as well.
func TestTheFixtureCollectorCarriesNoMCPOrEnvelopeCode(t *testing.T) {
	const fixtureFile = "fixture_test.go"
	body, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	text := string(body)

	for _, banned := range []string{
		"modelcontextprotocol", // the MCP SDK
		"mcp.",                 // any MCP symbol
		"jsonschema",           // schema construction
		"walk_complete",        // the envelope's own wire field
		"structuredContent",
		"InputSchema",
		"OutputSchema",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("%s names %q; a collector implements the walk and nothing about MCP or the envelope",
				fixtureFile, banned)
		}
	}

	// KNOWN POSITIVE, both halves: the census read the real file, and that file
	// really is a collector. A rename would otherwise leave this passing over an
	// empty string.
	if len(text) < 1000 {
		t.Fatalf("the census read %d bytes of %s, too few to be the fixture", len(text), fixtureFile)
	}
	if !strings.Contains(text, "func (f *fixtureCollector) Walk(") ||
		!strings.Contains(text, "func (f *fixtureCollector) Tool(") {
		t.Fatalf("%s does not declare the fixture's Walk and Tool; the census is reading the wrong file", fixtureFile)
	}
}

// TestTheRawEscapeIsNotExported is row 13's first half. The raw ToolHandler form
// performs no input validation and no output validation, so anything reaching it
// serves a tool advertising schemas it does not keep. It exists for this
// module's own negative tests and must stay unreachable from a collector.
func TestTheRawEscapeIsNotExported(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}

	installers := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || !callsRawAddTool(fn) {
					continue
				}
				installers++
				if fn.Name.IsExported() {
					t.Errorf("%s declares the EXPORTED %s, which installs the SDK's raw ToolHandler; "+
						"the raw form validates neither input nor output, so it stays framework-internal",
						name, fn.Name.Name)
				}
			}
		}
	}

	// KNOWN POSITIVE: the census found the escape it is about. A rename of the
	// SDK method, or a move of raw.go, would otherwise leave this passing over
	// zero sites.
	if installers != 1 {
		t.Fatalf("the census found %d functions installing the raw ToolHandler, want exactly 1 (newRawServer)", installers)
	}
}

// callsRawAddTool reports whether fn calls the SDK's METHOD form of AddTool,
// which is the raw one. The generic form is the package-level mcp.AddTool: a
// selector on the mcp package rather than on a server value.
func callsRawAddTool(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "AddTool" {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "mcp" {
			return true // the generic package-level form
		}
		found = true
		return false
	})
	return found
}
