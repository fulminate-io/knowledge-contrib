// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/installentry"
)

// family_census_test.go — THIS MODULE NEVER CLAIMS THE BUILT-IN GRAPH TYPE NAME.
//
// `cicd` is a BUILT-IN graph type: the client's own validator refuses to
// register it, and the refusal is emitted before any wire call is dispatched. So
// a collector cannot claim the name however it is spelled — and this module
// names nothing else and attempts nothing else.
//
// WHY A CENSUS AND NOT ONE ASSERTION ON THE FAMILY CONSTANT. The constant is one
// site; the failure this guards against is a `cicd` literal appearing somewhere
// ELSE later — a second family constant, a default, a config-entry key in a
// worked example, a fallback in an install helper. A shape census over every
// file catches the site that has not been written yet.
//
// THE ONE EXEMPTION IS DECLARED RATHER THAN PATTERN-MATCHED, and it is real: the
// node `source` field carries the tag `cicd` deliberately, because that field
// names the DOMAIN a node came from rather than the module that produced it. A
// consumer reading CI/CD resources across providers filters on it.

// exemptCICDLiterals are the sites where the literal `cicd` is CORRECT, by file
// and by the reason it is there. A new entry needs its own reason.
var exemptCICDLiterals = map[string]string{
	filepath.Join("internal", "bbgraph", "vocab.go"): "SourceTag: the node `source` field names " +
		"the DOMAIN the node came from rather than the collector family, and every CI/CD " +
		"provider's nodes carry it",
	filepath.Join("internal", "walk", "walk_test.go"): "the assertion that the provider metadata " +
		"is NOT the family name while the source tag IS the domain tag",
	"family_census_test.go": "this census's own subject, including the planted control it is " +
		"driven over",
}

// TestNoFileClaimsTheBuiltInFamilyName is the census.
func TestNoFileClaimsTheBuiltInFamilyName(t *testing.T) {
	files := moduleGoFiles(t)
	if len(files) == 0 {
		t.Fatal("the census found no Go files; it is not reading the module")
	}
	assertTheCensusDetectsTheLiteral(t)

	var hits []string
	for _, path := range files {
		relative := relativeToModule(t, path)
		if _, exempt := exemptCICDLiterals[relative]; exempt {
			continue
		}
		for _, line := range literalLines(t, path, "cicd") {
			hits = append(hits, relative+":"+strconv.Itoa(line))
		}
	}
	if len(hits) > 0 {
		t.Errorf("%d site(s) carry the literal %q outside the declared exemptions:\n  %s\n"+
			"That name is a built-in graph type and the client refuses to register it; this "+
			"collector's family is %q.",
			len(hits), "cicd", strings.Join(hits, "\n  "), installentry.FamilyName)
	}
}

// TestTheDeclaredFamilyIsThisCollectorsOwn is the positive half: the module does
// name a family, and it is the one the README's worked entry is keyed by.
func TestTheDeclaredFamilyIsThisCollectorsOwn(t *testing.T) {
	if installentry.FamilyName != "bitbucket-pipelines" {
		t.Errorf("the declared family is %q", installentry.FamilyName)
	}
	if _, keyed := installentry.Worked().Collectors[installentry.FamilyName]; !keyed {
		t.Error("the worked entry is not keyed by the declared family")
	}
	if _, claimed := installentry.Worked().Collectors["cicd"]; claimed {
		t.Error("the worked entry claims the built-in graph type name")
	}
}

// TestTheExemptionsAreRealRatherThanStale. An exemption naming a file that no
// longer carries the literal is an exemption that would hide a NEW one added to
// that file later.
func TestTheExemptionsAreRealRatherThanStale(t *testing.T) {
	for relative, reason := range exemptCICDLiterals {
		found := false
		for _, path := range moduleGoFiles(t) {
			if relativeToModule(t, path) != relative {
				continue
			}
			found = len(literalLines(t, path, "cicd")) > 0
		}
		if !found {
			t.Errorf("%s is exempted (%s) but carries no such literal; a stale exemption hides "+
				"the next one added to that file", relative, reason)
		}
	}
}

// assertTheCensusDetectsTheLiteral is the KNOWN POSITIVE: the matcher is driven
// over a planted source that carries the literal, in this same run, so a census
// that read nothing cannot report a clean module.
func assertTheCensusDetectsTheLiteral(t *testing.T) {
	t.Helper()
	const planted = `package p
const family = "cicd"
`
	parsed, err := parser.ParseFile(token.NewFileSet(), "planted.go", planted, 0)
	if err != nil {
		t.Fatalf("parsing the planted control: %v", err)
	}
	var found int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if isLiteral(n, "cicd") {
			found++
		}
		return true
	})
	if found != 1 {
		t.Fatalf("the planted control produced %d hits, want 1; the census is not detecting the "+
			"literal and its zero below would mean nothing", found)
	}
}

// literalLines is every line of a file carrying this exact string literal.
func literalLines(t *testing.T, path, want string) []int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		if isLiteral(n, want) {
			lines = append(lines, fset.Position(n.Pos()).Line)
		}
		return true
	})
	return lines
}

// isLiteral reports whether a node is exactly this string literal. It reads the
// UNQUOTED value, so a literal spelled with escapes is caught too.
func isLiteral(n ast.Node, want string) bool {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	return err == nil && value == want
}
