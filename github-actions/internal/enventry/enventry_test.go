// SPDX-License-Identifier: Apache-2.0

package enventry_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/enventry"
)

// enventry_test.go — THE ENVIRONMENT CLOSURE, censused over this module's own
// source.
//
// WHY A CENSUS AND NOT A LIST. The declaration in this package is a claim about
// the whole module: these are the names it reads, and there are no others. A test
// that only re-read the declaration would assert the claim against itself.
//
// WHAT THE CENSUS MEASURES IS THE READ SITES, not the literal names, and that is
// a consequence of how this module reads the environment: the resolver takes the
// lookup function as a PARAMETER and asks it for the declared names in turn, so
// there is no `os.Getenv("GITHUB_TOKEN")` anywhere to find. What the census can
// establish — and what makes the claim true — is that the whole module contains
// exactly ONE place that touches the process environment at all. The names that
// one place asks for are then asserted at run time by the resolver's own test,
// which records every name it consults and compares the list against this
// declaration.
//
// A DIRECT READ ADDED LATER IS CAUGHT BY BOTH HALVES: the census reports the new
// site as outside the one allowed file, and where the read carries a literal name
// the census checks that name against the declaration too.

// theOneFileThatReadsTheEnvironment is the module-relative path of the only file
// permitted to touch the process environment, with the reason.
//
// IT IS THE CREDENTIAL FILE. Everything else in this module is written against
// injected values — the provider clients, the enumerations, the walk — so a
// second file reading the environment would be a second, undeclared input to a
// collector whose whole environment is what its config entry names.
const theOneFileThatReadsTheEnvironment = "internal/ghclients/ghclients.go"

// TestTheClosureIsExactlyTwoCredentialNames is the declaration itself.
func TestTheClosureIsExactlyTwoCredentialNames(t *testing.T) {
	names := enventry.Names()
	if want := []string{"GITHUB_TOKEN", "GH_TOKEN"}; !slices.Equal(names, want) {
		t.Errorf("the declared closure is %v, want %v in consultation order", names, want)
	}
	classes := enventry.Classes()
	for _, name := range names {
		if classes[name] != enventry.ClassSecret {
			t.Errorf("%q is declared as %q; both names are credentials, and a credential's value "+
				"is never written into a config file in any state", name, classes[name])
		}
	}
	if len(classes) != len(names) {
		t.Errorf("%d names are classed and %d are declared", len(classes), len(names))
	}
}

// TestExactlyOneFileInThisModuleTouchesTheEnvironment is the census.
func TestExactlyOneFileInThisModuleTouchesTheEnvironment(t *testing.T) {
	sites := environmentReadSites(t, moduleRoot(t), moduleRoot(t))

	if len(sites) == 0 {
		t.Fatal("the census found no environment read anywhere in this module. This collector " +
			"reads a credential from its environment, so a zero here means the census is not " +
			"reading the module rather than that the module is clean")
	}
	for _, site := range sites {
		if site.file != theOneFileThatReadsTheEnvironment {
			t.Errorf("%s:%d reads the process environment, and the only file permitted to is %s. "+
				"A stdio collector's whole environment is what its config entry declares, so a "+
				"second reader is a second undeclared input",
				site.file, site.line, theOneFileThatReadsTheEnvironment)
		}
		if site.name != "" && !enventry.IsDeclared(site.name) {
			t.Errorf("%s:%d reads %q, which this collector's entry does not declare",
				site.file, site.line, site.name)
		}
	}
}

// TestAPlantedReadIsDetected is the census's known positive, in the same run. A
// census that read no file, or whose matcher never fired, would report a clean
// module perfectly.
func TestAPlantedReadIsDetected(t *testing.T) {
	const planted = `package p

import "os"

func f() (string, string) {
	direct := os.Getenv("PLANTED_THIRD_NAME")
	indirect := os.LookupEnv
	value, _ := indirect("PLANTED_FOURTH_NAME")
	return direct, value
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "planted.go"), []byte(planted), 0o600); err != nil {
		t.Fatalf("writing the planted control: %v", err)
	}

	sites := environmentReadSites(t, dir, dir)
	if len(sites) != 2 {
		t.Fatalf("the census found %d reads in the planted control, want 2 — one direct call and "+
			"one function reference; its result over this module means nothing otherwise: %v",
			len(sites), sites)
	}
	// The direct call's literal name is resolved, which is the half that checks a
	// future direct read against the declaration.
	if !slices.ContainsFunc(sites, func(s readSite) bool { return s.name == "PLANTED_THIRD_NAME" }) {
		t.Errorf("the census did not resolve the planted literal name: %v", sites)
	}
}

// readSite is one place the process environment is touched.
type readSite struct {
	file string
	line int
	// name is the literal variable name where the site is a call carrying one,
	// and empty where the site is a function reference or a computed name.
	name string
}

func (s readSite) String() string { return fmt.Sprintf("%s:%d(%s)", s.file, s.line, s.name) }

// environmentReadSites parses every non-test Go file under root and returns every
// reference to os.Getenv or os.LookupEnv, called or not.
//
// IT USES THE GO PARSER rather than a text search because the subject is a
// reference to a specific function: a text search would also match a mention in a
// comment or a string, and it could not tell a call from a name in prose.
func environmentReadSites(t *testing.T, root, relativeTo string) []readSite {
	t.Helper()
	var sites []readSite
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
			// A file the census cannot parse is a hole in it, so it is a failure
			// rather than a skip.
			t.Fatalf("parsing %s: %v", path, err)
		}
		relative, err := filepath.Rel(relativeTo, path)
		if err != nil {
			relative = path
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || !isEnvironmentRead(selector) {
				return true
			}
			sites = append(sites, readSite{
				file: relative,
				line: fset.Position(selector.Pos()).Line,
				name: literalNameOfEnclosingCall(file, selector),
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return sites
}

// isEnvironmentRead reports whether a selector names one of the two ways the
// standard library reads the process environment.
func isEnvironmentRead(selector *ast.SelectorExpr) bool {
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "os" {
		return false
	}
	return selector.Sel.Name == "Getenv" || selector.Sel.Name == "LookupEnv"
}

// literalNameOfEnclosingCall returns the literal variable name where the selector
// is the callee of a call carrying one, and empty otherwise.
func literalNameOfEnclosingCall(file *ast.File, selector *ast.SelectorExpr) string {
	var name string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || call.Fun != ast.Expr(selector) || len(call.Args) != 1 {
			return true
		}
		if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
			name = strings.Trim(literal.Value, `"`)
		}
		return true
	})
	return name
}

// moduleRoot is this module's directory, found by walking up to the go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the module: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory; the census has no module to read")
		}
		dir = parent
	}
}
