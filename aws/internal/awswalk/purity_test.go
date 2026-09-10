// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// purity_test.go — THE ONE THING THE COMPILER DOES NOT ENFORCE about this
// module's dependencies, and the result size bound the client imposes on it.
//
// THE COMPILER ALREADY REFUSES THE HALF THAT MATTERS MOST: an import of
// cmd/knowledge/internal or cmd/knowledge-server/internal from here fails to
// build, with "use of internal package ... not allowed", so no test is owed for
// it. What the compiler does NOT catch is a require on the ROOT module or on
// gen/, both of which are importable and would make this collector depend on the
// generated wire types rather than on the JSON contract.
//
// AND IT DOES NOT CATCH THE OTHER DIRECTION EITHER: `go mod tidy` DROPS a require
// nothing imports, so a refactor that stopped importing the framework would leave
// this module with no require on it at all — which builds fine inside the
// workspace and cannot be tidied outside it. So the assertion runs in BOTH
// directions, and neither half is satisfied by the other.

// modFilePath is this module's go.mod, two directories up from internal/awswalk.
// It resolves inside this module's root, so the go tool records the read and an
// edit to it re-runs this test.
const modFilePath = "../../go.mod"

func TestPurity_RequiresTheFrameworkAndNothingElseOfOurs(t *testing.T) {
	body, err := os.ReadFile(filepath.FromSlash(modFilePath))
	if err != nil {
		t.Fatalf("read %s: %v", modFilePath, err)
	}
	text := string(body)

	const frameworkPath = "github.com/fulminate-io/knowledge-contrib/framework"

	// DIRECTION ONE: the framework require is present. Without it `go mod tidy`
	// has dropped the dependency this module is built on.
	if !strings.Contains(text, frameworkPath) {
		t.Errorf("%s carries no require on %s; `go mod tidy` drops a require nothing imports, so this module "+
			"would build inside the workspace and fail to resolve outside it", modFilePath, frameworkPath)
	}
	// AND ITS REPLACE, which is what makes the require resolve to the in-tree
	// module rather than to a published version that does not exist.
	if !strings.Contains(text, "replace "+frameworkPath+" => ../framework") {
		t.Errorf("%s carries no `replace %s => ../framework`; the bare require resolves to a version that was "+
			"never published", modFilePath, frameworkPath)
	}

	// DIRECTION TWO: nothing ELSE of ours. Each of these is importable — no
	// compiler refuses it — and each would make this collector depend on our Go
	// types rather than on the JSON contract it is a peer consumer of.
	forbidden := map[string]string{
		"github.com/fulminate-io/knowledge/cmd/knowledge\n":      "the client module",
		"github.com/fulminate-io/knowledge/cmd/knowledge-server": "the server module",
		"github.com/fulminate-io/knowledge/cmd/server-bench":     "the bench module",
	}
	for path, what := range forbidden {
		if strings.Contains(text, path) {
			t.Errorf("%s requires %s (%s); a collector consumes the contract as JSON over MCP and requires "+
				"nothing of ours but the framework", modFilePath, path, what)
		}
	}
	// THE ROOT MODULE takes its own check, because its path is a PREFIX of the
	// framework's and a naive substring test would report the framework require as
	// a root require on every run.
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "github.com/fulminate-io/knowledge" {
			t.Errorf("%s requires the ROOT module (%s); that is the generated wire contract, and this collector "+
				"crosses to the client as JSON rather than as a Go type", modFilePath, strings.TrimSpace(line))
		}
	}
}

// TestPurity_TheModuleResolvesOutsideTheWorkspace is the gate the go.work file
// hides.
//
// WHAT WENT WRONG WITHOUT IT. Inside the workspace, go.work resolves every
// import whether or not this module's own go.mod is honest about them, so
// `go build ./...` and `go test ./...` both passed while thirty-seven direct
// requirements sat in the `// indirect` block and the test build was missing a
// dependency outright: `GOWORK=off go test ./...` answered "go: updates to go.mod
// needed". The module was therefore not independently buildable, which is the one
// property a peer-consumer collector module is supposed to have — a consumer
// vendoring it has no go.work.
//
// IT ASSERTS THE FILE, NOT A SUBPROCESS. Running `go mod tidy` from a test would
// WRITE to the module under test, and running `go list -m` in a clean-module check
// costs a network resolve on a cold cache. The property that actually broke is
// visible in the file: a package this module IMPORTS must not be marked indirect.
func TestPurity_TheModuleResolvesOutsideTheWorkspace(t *testing.T) {
	body, err := os.ReadFile(filepath.FromSlash(modFilePath))
	if err != nil {
		t.Fatalf("read %s: %v", modFilePath, err)
	}

	direct := directImports(t)
	if len(direct) == 0 {
		t.Fatal("this package imports no external modules, which cannot be true of an AWS collector; the " +
			"import scan is broken and the assertion below would pass on nothing")
	}

	indirect := indirectRequires(string(body))
	var mismarked []string
	for _, imported := range direct {
		if indirect[imported] {
			mismarked = append(mismarked, imported)
		}
	}
	sort.Strings(mismarked)
	if len(mismarked) > 0 {
		t.Errorf("%d module(s) this package IMPORTS BY NAME are marked `// indirect` in %s:\n  %s\n\n"+
			"The marker is a statement about the dependency graph and it is false for these. Inside the "+
			"workspace nothing notices, because go.work resolves the imports regardless; outside it "+
			"`GOWORK=off go test ./...` refuses the module. Run `GOWORK=off go mod tidy` in the module root.",
			len(mismarked), modFilePath, strings.Join(mismarked, "\n  "))
	}
}

// directImports returns the external module paths this package imports, taking
// the aws-sdk-go-v2 convention that a service package IS its own module.
func directImports(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if !strings.Contains(path, ".") {
				continue // a standard-library package has no dot in its first segment
			}
			// A /types OR /document SUB-PACKAGE belongs to its service's module,
			// so the require is on the parent path.
			for _, suffix := range []string{"/types", "/document"} {
				path = strings.TrimSuffix(path, suffix)
			}
			seen[path] = true
		}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// indirectRequires returns the set of module paths go.mod marks `// indirect`.
func indirectRequires(text string) map[string]bool {
	out := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.Contains(line, "// indirect") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 {
			out[strings.TrimPrefix(fields[0], "require ")] = true
		}
	}
	return out
}

// TestPurity_TheResultFitsTheClientsSizeBound covers the one hard limit this
// collector can breach.
//
// The client refuses a provider result over 64 MiB with an error naming the byte
// count, never a truncated parse — so an account large enough to exceed it fails
// the collect rather than landing partially, which is the correct failure. What
// this test pins is the LEVER: how much per-resource detail rides in a node's
// content is what decides how close a real account gets, and a change that
// started carrying a large blob per resource would move it a long way with
// nothing else noticing.
func TestPurity_TheResultFitsTheClientsSizeBound(t *testing.T) {
	res := fixtureResult(t)
	encoded, err := json.Marshal(map[string]any{
		"nodes":         res.Nodes,
		"edges":         res.Edges,
		"walk_complete": res.Complete.IsComplete(),
	})
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}

	// PER-RESOURCE, not in total: the fixture is one of everything, so its total
	// says nothing about a real account. What scales is the bytes each resource
	// costs, and the bound below is generous enough that only a genuine change of
	// shape — a whole API response marshaled into content — reaches it.
	perNode := len(encoded) / max(len(res.Nodes), 1)
	const perNodeBudget = 4096
	if perNode > perNodeBudget {
		t.Errorf("the result costs %d bytes per node, above the %d-byte budget. The client refuses a result "+
			"over 64 MiB outright, so per-resource detail is the lever that decides how large an account this "+
			"collector can carry: at this size the ceiling is about %d resources.",
			perNode, perNodeBudget, (64<<20)/perNode)
	}

	// A NODE CARRIES NO SECRET, asserted rather than assumed: this collector calls
	// no operation that returns a secret value, and this is what would notice if
	// one were added.
	for _, n := range res.Nodes {
		for _, forbidden := range []string{"SecretString", "AWS_SECRET_ACCESS_KEY", "-----BEGIN"} {
			if strings.Contains(n.Content, forbidden) || strings.Contains(n.Summary, forbidden) {
				t.Errorf("node %s carries %q; a collected node is searched, summarized and synchronized",
					n.ID, forbidden)
			}
		}
	}
}

// TestPurity_TheCollectorSatisfiesTheFrameworkInterface is a compile-time
// assertion made explicit: the framework's entry points are generic over the
// params type, so a mismatch surfaces at every call site rather than once.
func TestPurity_TheCollectorSatisfiesTheFrameworkInterface(t *testing.T) {
	var _ framework.Collector[Params] = New()
	if New().Tool().Name != ToolName {
		t.Errorf("the collector serves %q, want %q", New().Tool().Name, ToolName)
	}
}
