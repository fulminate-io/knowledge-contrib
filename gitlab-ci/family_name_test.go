// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/installentry"
)

// family_name_test.go — TWO NAMES THIS MODULE MAY NOT REGISTER UNDER, censused
// over its own source.
//
// `cicd` IS A BUILT-IN GRAPH TYPE. The client refuses to register it by that
// name, so a collector could not claim it however it was spelled — the
// registration fails and no collect ever runs.
//
// `gitlab` IS WORSE, because it SUCCEEDS. A compiled-in collector holds that
// name, and the client's post-collect linker is keyed on exactly those names — so
// a family registered under the bare provider name would have a linker run over a
// graph whose data it was never written for, silently, with no error anywhere.
//
// SO WHY IS THIS A CENSUS AND NOT ONE ASSERTION ON THE FAMILY CONSTANT. Because
// both strings legitimately appear in this module for OTHER reasons: `cicd` is the
// node source tag, naming the domain a node came from, and `gitlab` is the
// provider metadata value and the first segment of every node id. An assertion
// that simply forbade the literals would be red on a correct build. What this
// asserts instead is that each appears ONLY at the sites named below, each with
// its own reason — so a third site, which is where a family name would be
// introduced, fails by name.

// theSitesEachNameMayAppearAt is the allowlist, module-relative, with the reason
// each entry is admitted.
var theSitesEachNameMayAppearAt = map[string]map[string]string{
	"cicd": {
		"internal/glgraph/vocab.go": "the node `source` field: the DOMAIN a node came " +
			"from, which is not the family this collector registers under",
	},
	"gitlab": {
		"internal/glgraph/vocab.go": "the `provider` metadata value on every node",
	},
}

// TestNeitherForbiddenNameAppearsOutsideItsDeclaredSites is the census.
func TestNeitherForbiddenNameAppearsOutsideItsDeclaredSites(t *testing.T) {
	root := moduleDirectory(t)
	sites := stringLiteralSites(t, root, root, []string{"cicd", "gitlab"})

	// THE KNOWN POSITIVE: the two legitimate sites must actually be found, or the
	// census is not reading the module and its silence about a third would mean
	// nothing.
	for name, allowed := range theSitesEachNameMayAppearAt {
		for file := range allowed {
			if !hasSite(sites, name, file) {
				t.Errorf("the census found no %q literal in %s, where one is declared to live; "+
					"its verdict about any other file means nothing", name, file)
			}
		}
	}

	for _, site := range sites {
		if _, allowed := theSitesEachNameMayAppearAt[site.literal][site.file]; allowed {
			continue
		}
		t.Errorf("%s:%d carries the literal %q, which is not one of the sites it is admitted at "+
			"(%v). A collector registered under that name either cannot register at all, or "+
			"registers successfully and has a linker written for another provider run over its "+
			"graph.", site.file, site.line, site.literal,
			mapKeys(theSitesEachNameMayAppearAt[site.literal]))
	}
}

// TestTheCensusFindsAPlantedLiteral is the second half of the known positive: the
// matcher fires on a file that carries one.
func TestTheCensusFindsAPlantedLiteral(t *testing.T) {
	const planted = `package p

const family = "cicd"

func f() string { return "gitlab" }
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "planted.go"), []byte(planted), 0o600); err != nil {
		t.Fatalf("writing the planted control: %v", err)
	}

	sites := stringLiteralSites(t, dir, dir, []string{"cicd", "gitlab"})
	if len(sites) != 2 {
		t.Fatalf("the census found %d literals in the planted control, want 2: %v", len(sites), sites)
	}
}

// TestTheFamilyNameIsDeclaredExactlyOnce. The entry's key IS the graph family, so
// a second declaration of it is a second answer to what this collector is called.
func TestTheFamilyNameIsDeclaredExactlyOnce(t *testing.T) {
	root := moduleDirectory(t)
	sites := stringLiteralSites(t, root, root, []string{installentry.FamilyName})

	if len(sites) != 1 {
		t.Errorf("the family name %q is written at %d sites, want exactly 1 — the install entry's "+
			"own constant: %v", installentry.FamilyName, len(sites), sites)
	}
	if len(sites) == 1 && sites[0].file != "internal/installentry/installentry.go" {
		t.Errorf("the family name is declared in %s rather than in the install entry", sites[0].file)
	}
}

// literalSite is one string literal found in the module's own source.
type literalSite struct {
	file    string
	line    int
	literal string
}

func (s literalSite) String() string { return fmt.Sprintf("%s:%d(%q)", s.file, s.line, s.literal) }

func hasSite(sites []literalSite, literal, file string) bool {
	for _, site := range sites {
		if site.literal == literal && site.file == file {
			return true
		}
	}
	return false
}

// stringLiteralSites parses every non-test Go file under root and returns every
// string literal whose value is one of the named ones.
//
// IT USES THE GO PARSER rather than a text search because the subject is a
// LITERAL: a text search would match the same word in a comment, in a doc block
// and inside a longer string, and this module's comments discuss both names at
// length.
func stringLiteralSites(t *testing.T, root, relativeTo string, names []string) []literalSite {
	t.Helper()
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}

	var sites []literalSite
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		relative, err := filepath.Rel(relativeTo, path)
		if err != nil {
			relative = path
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value := strings.Trim(literal.Value, "`\"")
			if wanted[value] {
				sites = append(sites, literalSite{
					file: relative, line: fset.Position(literal.Pos()).Line, literal: value,
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return sites
}

// moduleDirectory is this module's own directory.
func moduleDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the module: %v", err)
	}
	return dir
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
