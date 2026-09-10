// SPDX-License-Identifier: Apache-2.0

package main

import (
	"embed"
	"os"
	"slices"
	"strings"
	"testing"
)

// moduleSources is this module's own Go source, EMBEDDED rather than read from
// disk at run time.
//
// The difference is not stylistic. A test that walked the directory and opened
// each file would read paths the build never saw, so the go tool could store a
// PASS for a source set the run did not actually read; embedded files are build
// inputs, so editing any of them rebuilds the test binary and no stale result
// can survive. It also keeps this package free of the dynamic-path reads the
// repository's cache-blindness census exists to bound.
//
//go:embed *.go
var moduleSources embed.FS

// purity_test.go — THE DEPENDENCY-PURITY GATE this module's go.mod header claims
// to be.
//
// WHY IT IS A TEST AND NOT A COMMENT. The header calls itself a purity lock and
// names what the module may and may not require, and for one commit it did so
// while misdeclaring its own dependency set: the MCP SDK was marked indirect
// though a test imports it directly, and goleak was absent from the require list
// though a test reaches it. Neither is visible to `go build`, which compiles no
// test files, so the module built standalone and could not be vetted or tested
// standalone.
//
// WHAT THIS GATE READS. The checked-in go.mod itself. It is deliberately not a
// re-run of `go mod tidy`: a gate that re-derived the answer would agree with
// whatever the tool produced and would never disagree with the file. Reading the
// file and comparing it to a written expectation is what makes a re-marked or
// dropped dependency a red.
//
// THE STANDALONE ARM IS THE OTHER HALF and it lives outside this file, because a
// test cannot run itself outside its own workspace: `GOWORK=off go vet ./...`
// and `GOWORK=off go test ./...` are the commands, and the implementation report
// carries their output. `go build` alone cannot see this class of defect and is
// not the arm.

// directRequirements are the modules this collector imports itself, in its
// source or in its tests. Anything here that go.mod marks indirect, or drops, is
// a misdeclaration.
var directRequirements = []string{
	"github.com/aws/aws-sdk-go-v2",
	"github.com/aws/aws-sdk-go-v2/config",
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs",
	"github.com/fulminate-io/knowledge-contrib/common/correlation",
	"github.com/fulminate-io/knowledge-contrib/framework",
	"github.com/modelcontextprotocol/go-sdk",
}

// forbiddenRequirements are the modules this one must never require AT ALL,
// directly or transitively. Each is a boundary rather than a preference.
var forbiddenRequirements = map[string]string{
	"github.com/fulminate-io/knowledge/cmd/knowledge":        "the client's own implementation of this contract; the crossing is JSON over MCP, never a Go type",
	"github.com/fulminate-io/knowledge/cmd/knowledge-server": "the server; a collector speaks to it through no Go package at all",
	"github.com/fulminate-io/knowledge":                      "the root contract module; a collector needs no generated protobuf",
}

// neverDirectRequirements are modules that may appear as INDIRECT dependencies —
// something this module requires pulls them in — but that this module must never
// import itself.
//
// THE DISTINCTION IS NOT PEDANTRY, and getting it wrong is what this entry
// records. aws-sdk-go-v2/credentials is in the module graph whatever this
// collector does: the configuration package builds the single-sign-on and
// web-identity providers out of it while resolving the default chain. What the
// collector controls is whether IT constructs one, and a static-credentials
// provider is the second credential route the ticket's requirement forbids.
// Declaring the module forbidden outright would be a rule the module cannot
// keep; declaring it never-direct is the rule that means something.
var neverDirectRequirements = map[string]string{
	"github.com/aws/aws-sdk-go-v2/credentials": "the static-credentials provider; this collector reads the default chain only, and constructing one would be a second credential route",
}

// TestGoModDeclaresItsDependenciesHonestly is the gate.
func TestGoModDeclaresItsDependenciesHonestly(t *testing.T) {
	body, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	declared := parseRequirements(string(body))
	if len(declared) == 0 {
		t.Fatal("go.mod declares no requirements at all; every assertion below would be vacuous")
	}

	for _, path := range directRequirements {
		indirect, present := declared[path]
		if !present {
			t.Errorf("go.mod does not require %s, which this module imports directly", path)
			continue
		}
		if indirect {
			t.Errorf("go.mod marks %s indirect, and this module imports it directly; the marker is a claim "+
				"about who needs the dependency and this one is false", path)
		}
	}

	for path, why := range forbiddenRequirements {
		if _, present := declared[path]; present {
			t.Errorf("go.mod requires %s, which this module must not at all: %s", path, why)
		}
	}

	for path, why := range neverDirectRequirements {
		indirect, present := declared[path]
		if present && !indirect {
			t.Errorf("go.mod requires %s DIRECTLY, which means this module imports it: %s", path, why)
		}
	}
}

// TestEveryDirectRequirementIsActuallyImported is the other direction, so the
// list above cannot rot into a wish.
//
// A path declared direct that nothing imports would make the gate above pass on
// a module that has since stopped using it, and the require line would then sit
// there unexamined.
func TestEveryDirectRequirementIsActuallyImported(t *testing.T) {
	corpus, files := readEmbeddedSources(t)
	if files == 0 || !strings.Contains(corpus, "package main") {
		t.Fatalf("the embedded source corpus holds %d files and does not contain this package; every "+
			"assertion below would be vacuous", files)
	}
	text := corpus
	for _, path := range directRequirements {
		if !strings.Contains(text, `"`+path+`"`) && !strings.Contains(text, `"`+path+`/`) {
			t.Errorf("go.mod declares %s direct, and no file in this module imports it", path)
		}
	}
}

// readEmbeddedSources concatenates every embedded Go file and reports how many
// it read, so an empty corpus is a loud failure rather than a vacuous pass.
func readEmbeddedSources(t *testing.T) (string, int) {
	t.Helper()
	var corpus strings.Builder
	files := 0
	entries, err := moduleSources.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the embedded source: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		body, err := moduleSources.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("reading the embedded %s: %v", e.Name(), err)
		}
		corpus.Write(body)
		files++
	}
	return corpus.String(), files
}

// parseRequirements reads go.mod's require blocks into a map of module path to
// whether it is marked indirect.
//
// It parses the file rather than calling the module tooling on purpose: the
// question is what the CHECKED-IN FILE says, and resolving the module graph
// would answer a different one.
func parseRequirements(body string) map[string]bool {
	out := map[string]bool{}
	inBlock := false
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "require (":
			inBlock = true
			continue
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case strings.HasPrefix(trimmed, "require ") && !inBlock:
			trimmed = strings.TrimPrefix(trimmed, "require ")
		case !inBlock:
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		out[fields[0]] = slices.Contains(fields, "indirect")
	}
	return out
}
