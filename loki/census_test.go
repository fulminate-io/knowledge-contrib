// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/loki/internal/lokiapi"
)

// census_test.go — THE SOURCE CENSUSES, each carrying its own known positive.
//
// WHY THESE ARE TESTS AND NOT CORPUS CHECKS. A corpus check reports the sites
// where a SHAPE IS PRESENT; it has no vocabulary for "this named file must
// still contain X", for a per-file allowlist carrying the reason each entry is
// admissible, or for a module-scoped contract. What a check CAN express here —
// a bare import path as a literal — matches the path wherever the literal
// appears rather than binding an import spec, so it would flag a mention in a
// comment and miss an aliased import. The census below binds the import spec
// through go/parser, which is what the assertion actually needs.
//
// A CENSUS THAT READ NOTHING PASSES EXACTLY AS LOUDLY AS ONE THAT READ
// EVERYTHING AND FOUND NOTHING WRONG, which is why each carries a known
// positive naming a file it is certain about.

// moduleRoot is the directory holding this module's go.mod. The walk starts
// here rather than at the repository root: the assertion is about THIS MODULE'S
// sources, and a walk that wandered into a sibling would report another
// module's imports as this one's.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod at or above %s", dir)
		}
		dir = parent
	}
}

// nonTestGoFiles returns every non-test Go file of this module, repo-relative to
// the module root.
//
// THE SCOPE IS NON-TEST FILES, and that is the rule rather than an oversight: a
// TEST that drives a container runtime or reads another module's checked-in
// files is the harness's business, while a COLLECTOR that shells out is the
// violation both censuses below are about.
func nonTestGoFiles(t *testing.T) map[string][]byte {
	t.Helper()
	root := moduleRoot(t)
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		//nolint:gosec // G122: the path is the walker's own entry under this module's root.
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = body
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	return out
}

// importsOf parses one file and returns its import paths, unquoted. An ALIASED
// import carries the same path, which is why the paths are read from the parsed
// spec rather than matched as text.
func importsOf(t *testing.T, name string, body []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, body, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	out := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s: unquoting the import %s: %v", name, spec.Path.Value, err)
		}
		out = append(out, path)
	}
	return out
}

// TestNoNonTestFileImportsTheKnowledgeClientOrServer is R1's own assertion, and
// it has NO OTHER CARRIER: nothing in the build fails on such an import, because
// the Go workspace resolves it happily.
//
// The assertion names the two `cmd/` prefixes rather than the whole
// github.com/fulminate-io/knowledge prefix, so the collector framework — which
// this module does require and does import — falsifies neither clause. That is
// deliberate and no exception is written for it: a census listing an exception
// nobody needs invites the next reader to widen it again.
func TestNoNonTestFileImportsTheKnowledgeClientOrServer(t *testing.T) {
	const (
		client = "github.com/fulminate-io/knowledge/cmd/knowledge/"
		server = "github.com/fulminate-io/knowledge/cmd/knowledge-server/"
	)
	files := nonTestGoFiles(t)
	for name, body := range files {
		for _, path := range importsOf(t, name, body) {
			if strings.HasPrefix(path, client) || strings.HasPrefix(path, server) {
				t.Errorf("%s imports %q; this module is a separate binary and imports nothing of the knowledge client or server",
					name, path)
			}
		}
	}

	// KNOWN POSITIVE: the census read a file it is certain about, and that file
	// really is this module's. A mistyped root, a rename or a walk that started
	// in the wrong directory would otherwise leave this passing over nothing.
	assertCensusSawItsSubject(t, files)
}

// TestNoNonTestFileSpawnsAProcess is the coarse spawn carrier. A collector that
// shelled out to docker, or to logcli, would be the violation; a TEST that
// drives a container runtime to stand a venue up is outside this census by the
// non-test scope rule stated above.
//
// A PROCESS CANNOT BE SPAWNED FROM GO WITHOUT IMPORTING ONE OF THE PACKAGES
// THAT CAN SPAWN IT, which is why the census is over imports rather than over
// call shapes: a call shape moves one construct over and a pattern misses it.
// The syscall half is NOT a ban on the import. syscall carries the signal
// numbers a long-lived process needs alongside its exec primitives, so banning
// the package would ban handling SIGTERM, and a collector that cannot be asked
// to stop is a worse outcome than the one this census exists to prevent. The
// allowlist below names each file admitted and WHY, and the primitive check
// underneath is what the ban is really about.
func TestNoNonTestFileSpawnsAProcess(t *testing.T) {
	// syscallAllowlist admits a file to import syscall, with the reason. An
	// entry is a per-file permission, not a standing one: a file that no longer
	// needs it fails the staleness assertion below rather than keeping it.
	syscallAllowlist := map[string]string{
		"main.go": "signal.NotifyContext takes syscall.SIGTERM, which is how a spawned collector is asked to stop",
	}
	// spawnPrimitives are the selectors that actually start a process. They are
	// checked by NAME on the parsed tree, so an aliased import does not hide one.
	spawnPrimitives := map[string]bool{
		"Exec": true, "ForkExec": true, "StartProcess": true, "CreateProcess": true,
	}

	files := nonTestGoFiles(t)
	used := map[string]bool{}
	for name, body := range files {
		importsSyscall := false
		for _, path := range importsOf(t, name, body) {
			switch {
			case path == "os/exec":
				t.Errorf("%s imports %q, which runs a subprocess; this collector speaks HTTP to Loki and spawns nothing",
					name, path)
			case path == "syscall":
				importsSyscall = true
			}
		}
		if importsSyscall {
			if _, ok := syscallAllowlist[name]; !ok {
				t.Errorf("%s imports syscall and is not in the allowlist; add it with the reason, or drop the import", name)
			}
			used[name] = true
		}
		assertNoSpawnPrimitive(t, name, body, spawnPrimitives)
	}

	// THE STALE-ENTRY DIRECTION, which is what keeps the allowlist a census
	// rather than a standing permission: an entry for a file that no longer
	// imports syscall is removed rather than left to cover a future one.
	for name := range syscallAllowlist {
		if !used[name] {
			t.Errorf("the allowlist admits %s to import syscall, but it does not import it; remove the entry", name)
		}
	}

	assertCensusSawItsSubject(t, files)
}

// assertNoSpawnPrimitive walks a file's syntax tree for a call to one of the
// process-starting selectors, whatever package it is qualified by.
func assertNoSpawnPrimitive(t *testing.T, name string, body []byte, primitives map[string]bool) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, body, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !primitives[sel.Sel.Name] {
			return true
		}
		pkg, _ := sel.X.(*ast.Ident)
		qualifier := "?"
		if pkg != nil {
			qualifier = pkg.Name
		}
		t.Errorf("%s calls %s.%s, which starts a process; this collector spawns nothing",
			name, qualifier, sel.Sel.Name)
		return false
	})
}

// assertCensusSawItsSubject is both censuses' known positive.
func assertCensusSawItsSubject(t *testing.T, files map[string][]byte) {
	t.Helper()
	const certain = "internal/lokiapi/client.go"
	body, ok := files[certain]
	if !ok {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		t.Fatalf("the census did not read %s, so it did not read this module; it read %v", certain, names)
	}
	// And that file really is the one meant: it declares the HTTP client this
	// module talks to Loki with. A file renamed to that path would otherwise
	// satisfy the check above.
	if !strings.Contains(string(body), "func NewClient(") {
		t.Fatalf("%s does not declare NewClient; the census is reading the wrong file", certain)
	}
	if len(files) < 8 {
		t.Fatalf("the census read %d non-test Go files, too few to be this module", len(files))
	}
}

// TestMainServesTheRealCollectorThroughTheFrameworksEntryPoint ties this
// package's test child to the shipped binary. The stdio arm in main_test.go
// re-executes THIS TEST BINARY, so without this assertion it would be exercising
// an entry point main() might not use.
func TestMainServesTheRealCollectorThroughTheFrameworksEntryPoint(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(moduleRoot(t), "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", body, 0)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}

	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ServeStdio" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "framework" {
			return true
		}
		found = true
		return false
	})
	if !found {
		t.Fatal("main.go does not call framework.ServeStdio; the stdio arm's child is not the shipped entry point")
	}
	if !strings.Contains(string(body), "collect.Collector{}") {
		t.Fatal("main.go does not serve the real collector")
	}
	// AND IT SERVES ONLY STDIO. An http listener would be additive rather than
	// wrong, but it is not what this module's config entry declares, and a
	// binary that opened a port an operator did not ask for is a surface nobody
	// reviewed.
	if strings.Contains(string(body), "ServeHTTP") {
		t.Fatal("main.go serves over HTTP as well as stdio; this module's entry is stdio")
	}
}

// TestTheInstallGuideListsExactlyTheNamesThisCollectorReads keeps the shipped
// guide from drifting from the code.
//
// AN OPERATOR'S CONFIG ENTRY IS THE CHILD'S WHOLE ENVIRONMENT, so a name this
// collector reads and the guide omits is a variable the operator sets and finds
// ignored, with a transport or credential failure naming neither cause. A name
// the guide lists and nothing reads is a line in a config file nobody can
// justify. The census runs in BOTH directions for that reason.
func TestTheInstallGuideListsExactlyTheNamesThisCollectorReads(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join(moduleRoot(t), "README.md"))
	if err != nil {
		t.Fatalf("reading the install guide: %v", err)
	}
	text := string(guide)

	block, ok := envBlockOf(text)
	if !ok {
		t.Fatal("the guide's example config entry carries no env block")
	}

	declared := map[string]bool{}
	for _, name := range lokiapi.Names() {
		declared[name] = true
		if !block[name] {
			t.Errorf("the guide's env block does not list %s, which this collector reads; "+
				"an operator whose entry omits it sets it and finds it ignored", name)
		}
	}
	for name := range block {
		if !declared[name] {
			t.Errorf("the guide's env block lists %s, which nothing in this module reads", name)
		}
	}

	// KNOWN POSITIVE: the extractor found a real block rather than an empty
	// one. Without this, a guide whose block stopped parsing would satisfy the
	// listed-minus-read direction vacuously.
	if len(block) < len(lokiapi.Names()) {
		t.Fatalf("the extractor found %d names in the guide's env block, fewer than the %d this collector reads",
			len(block), len(lokiapi.Names()))
	}
	// AND THE ONE NAME THAT MUST NOT BE THERE: the address is a tool parameter,
	// so a guide listing LOKI_ADDR would tell an operator to configure it twice.
	if block["LOKI_ADDR"] {
		t.Error("the guide's env block lists LOKI_ADDR; the address is a parameter of the collect call")
	}
}

// envBlockOf extracts the variable names from the "env" object of the guide's
// example config entry: the lines between `"env": {` and its closing brace.
func envBlockOf(text string) (map[string]bool, bool) {
	const open = `"env": {`
	_, rest, ok := strings.Cut(text, open)
	if !ok {
		return nil, false
	}

	// THE CLOSING BRACE IS FOUND BY DEPTH, not by the first `}`. Every value in
	// the block is a `${NAME}` placeholder, so the first closing brace is the
	// end of the first value rather than the end of the block — an extractor
	// that stopped there would read one name and report the other twenty-five
	// as missing.
	depth, end := 1, -1
	for i, r := range rest {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, false
	}

	out := map[string]bool{}
	for line := range strings.SplitSeq(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		if idx := strings.Index(line[1:], `"`); idx >= 0 {
			out[line[1:1+idx]] = true
		}
	}
	return out, len(out) > 0
}
