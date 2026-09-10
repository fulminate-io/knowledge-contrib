// SPDX-License-Identifier: Apache-2.0

package main

import (
	"embed"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// moduleSources is this module's own Go source, embedded at BUILD time.
//
// WHY AN EMBED RATHER THAN A DIRECTORY WALK, and it is not only about avoiding
// a filesystem read. `go test` keys a package's cached result on the files a
// run OPENED, and a walk that opens files during the test is a read the tool
// cannot see when it decides whether a stored PASS is still valid — so a census
// implemented as a walk can report a stored PASS against source it never read.
// An embed is a BUILD input: every file below is part of this package's build,
// the cache key already covers all of them, and editing any one of them
// invalidates the stored result by construction.
//
// The pattern takes _test.go files too, which censusImports filters out; the
// scope rule is stated at that function.
//
//go:embed *.go
var moduleSources embed.FS

// import_census_test.go — R1-b: WHAT THIS MODULE IS ALLOWED TO IMPORT, by a
// go/parser census over its own non-test files.
//
// TWO RULES, ONE INSTRUMENT.
//
// RULE 1 — NO SPAWN. This module reads a cluster through the Kubernetes SDK and
// never shells out: not to kubectl, not to gcloud, not to anything. A process
// cannot be spawned from Go without importing one of the packages that can
// spawn it, so pinning WHO IMPORTS THOSE PACKAGES bounds the whole class at
// once, whatever spelling a future author reaches for. The allowlist here is
// EMPTY: no non-test file in this module may import os/exec.
//
// RULE 2 — ONE PATH OF OURS, NAMED. The only github.com/fulminate-io/knowledge
// path a non-test file here may import is the collector framework. Nothing
// under cmd/knowledge, nothing under cmd/knowledge-server, and no gen/ path.
// The internal-package half of that is compiler-enforced; the gen/ half IS NOT,
// which is why it needs a census. The admitted path is asserted BY NAME rather
// than as an absence, so an import of a DIFFERENT knowledge path cannot pass as
// the framework.
//
// WHY THIS IS A PORT AND NOT A REDUNDANCY. The client module carries the same
// census, and it walks up to ITS OWN module root — so it censuses cmd/knowledge
// and can never see a module under cmd/collectors. The instrument is the right
// one and its scope is the wrong one; porting it is what makes it cover this
// module.
//
// ONE CAVEAT THE CENSUS DELIBERATELY DOES NOT COVER: it reads OUR OWN FILES,
// never the module graph. client-go's GKE credential plugin legitimately
// imports os/exec to run gcloud, which is the whole reason PATH is on this
// module's declared environment. A census over the dependency graph would flag
// that and prove nothing.
//
// SCOPE: non-test files only. A _test.go file is not compiled into the shipped
// binary; the fixtures here legitimately import test-only packages.

// spawnPackages are the import paths from which a process can be started.
// os/exec is the ordinary route; the primitives below it (os.StartProcess,
// syscall.Exec, syscall.ForkExec) live in packages nearly every file imports
// for unrelated reasons and are covered by corpus checks on the CALL instead.
var spawnPackages = map[string]bool{"os/exec": true}

// spawnImporters is the allowlist, and IT IS DELIBERATELY EMPTY. Adding an
// entry here is the deliberate act of saying this collector now runs a program,
// which it has no reason to do.
var spawnImporters = map[string]string{}

// ourModulePrefixes are the import prefixes of this repository's own modules.
//
// TWO ENTRIES, AND THE SECOND IS NOT REDUNDANT WITH THE FIRST IN EVERY TREE THIS
// FILE LIVES IN. scripts/sync-to-contrib.sh publishes these modules to
// knowledge-contrib and rewrites github.com/fulminate-io/knowledge-contrib
// to that repository's own path, so in the published tree the second entry
// becomes the published prefix and matches every import of ours, while the first
// still matches the cmd/knowledge and cmd/knowledge-server paths rule 2 refuses.
// With the upstream prefix alone this census would read ZERO imports there and
// report a clean module; the known positive at the end of rule 2 caught exactly
// that.
var ourModulePrefixes = []string{
	"github.com/fulminate-io/knowledge/",
	"github.com/fulminate-io/knowledge-contrib/",
}

// isOurImport reports whether an import path is one of ours, under either prefix
// above.
func isOurImport(path string) bool {
	for _, prefix := range ourModulePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// admittedOurImports is the exhaustive set of our own packages a non-test file
// in this module may import. It is a NAMED SET rather than a refusal list: a
// new path of ours has to be admitted here explicitly, so it cannot arrive by
// resembling something already allowed.
var admittedOurImports = map[string]string{
	"github.com/fulminate-io/knowledge-contrib/framework": "the MCP serving and contract layer every custom collector is built on",
}

func TestImportCensus_NoNonTestFileSpawnsAProcess(t *testing.T) {
	found, files := censusImports(t)

	// KNOWN POSITIVE: the walk actually read this module. Without it an empty
	// census — a mistyped root, a skip rule that ate everything — reads as a
	// clean result, which is the exact failure this test exists to prevent.
	require.Contains(t, files, "collector.go",
		"the census did not see a file it is certain about; the walk read the wrong tree")
	require.Greater(t, len(files), 10, "the census read this module's files, not one of them")

	var unexpected []string
	for file, imports := range found {
		for _, imported := range imports {
			if spawnPackages[imported] {
				unexpected = append(unexpected, file+" imports "+imported)
			}
		}
	}
	sort.Strings(unexpected)
	assert.Empty(t, unexpected,
		"this collector reads a cluster through the Kubernetes SDK and never runs a program. "+
			"A kubectl, gcloud or shelled kubeconfig read is exactly the class this census bounds: %v", unexpected)
	assert.Empty(t, spawnImporters, "the spawn allowlist is empty and adding to it is a deliberate act")
}

func TestImportCensus_TheOnlyPathOfOursIsTheFramework(t *testing.T) {
	found, files := censusImports(t)
	require.NotEmpty(t, files)

	var unexpected []string
	sawFramework := false
	for file, imports := range found {
		for _, imported := range imports {
			if !isOurImport(imported) {
				continue
			}
			if _, ok := admittedOurImports[imported]; ok {
				sawFramework = sawFramework || imported == "github.com/fulminate-io/knowledge-contrib/framework"
				continue
			}
			unexpected = append(unexpected, file+" imports "+imported)
		}
	}
	sort.Strings(unexpected)

	assert.Empty(t, unexpected,
		"a non-test file in this module may import only the collector framework of ours. "+
			"cmd/knowledge and cmd/knowledge-server are refused by the compiler; a gen/ path is NOT, "+
			"which is what this census covers: %v", unexpected)

	// KNOWN POSITIVE for rule 2, and it is what makes the assertion above
	// non-vacuous: the admitted path IS imported, so a census that found no
	// imports of ours at all would fail here rather than pass silently.
	require.True(t, sawFramework,
		"no non-test file imports the framework; this module is built on it, so the census "+
			"read the wrong tree or the module stopped using it")
}

// TestImportCensus_AdmittedSetIsNotStale keeps the named set a census rather
// than a standing permission.
func TestImportCensus_AdmittedSetIsNotStale(t *testing.T) {
	found, _ := censusImports(t)
	imported := map[string]bool{}
	for _, imports := range found {
		for _, path := range imports {
			imported[path] = true
		}
	}
	for path, reason := range admittedOurImports {
		assert.True(t, imported[path],
			"%s is admitted (%q) but no non-test file imports it; remove it so the set stays a census", path, reason)
	}
}

// censusImports parses every non-test Go file in this module and returns its
// imports keyed by module-relative path, plus the file list.
//
// IT READS THE UNQUOTED IMPORT PATH, NEVER THE LOCAL NAME. An aliased or blank
// import is the same import, and reading the alias is exactly how a
// spelling-based audit is fooled.
func censusImports(t *testing.T) (map[string][]string, []string) {
	t.Helper()

	entries, err := moduleSources.ReadDir(".")
	require.NoError(t, err)

	found := map[string][]string{}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := moduleSources.ReadFile(name)
		require.NoError(t, readErr)

		file, parseErr := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		require.NoError(t, parseErr)

		files = append(files, name)
		for _, spec := range file.Imports {
			imported, unquoteErr := strconv.Unquote(spec.Path.Value)
			require.NoError(t, unquoteErr)
			found[name] = append(found[name], imported)
		}
	}
	sort.Strings(files)
	return found, files
}
