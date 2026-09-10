// SPDX-License-Identifier: Apache-2.0

package enventry_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/enventry"
)

// enventry_test.go — the declaration IS the closure, asserted by censusing the
// module's own source.
//
// A NAME THE COLLECTOR READS AND THE ENTRY OMITS IS A NAME THE CHILD DOES NOT
// HAVE. A stdio collector receives exactly the environment its config entry
// declares — the daemon copies nothing and adds nothing — and the install
// table is built from this declaration. So a name read but not declared is a
// variable an operator sets and the collector never sees, and a name declared
// but not read is a row in an install table that buys nothing.

// TestTheClosureIsThreeNamesInTwoClasses.
func TestTheClosureIsThreeNamesInTwoClasses(t *testing.T) {
	names := enventry.Names()
	want := []string{
		"BITBUCKET_USERNAME",
		"BITBUCKET_APP_PASSWORD",
		"BITBUCKET_PIPELINE_HISTORY_DEPTH",
	}
	if !slices.Equal(names, want) {
		t.Errorf("the declared closure is %v, want %v in consultation order", names, want)
	}

	classes := enventry.Classes()
	if len(classes) != len(names) {
		t.Errorf("%d names are declared and %d are classed", len(names), len(classes))
	}
	for name, want := range map[string]enventry.Class{
		"BITBUCKET_USERNAME":               enventry.ClassSecret,
		"BITBUCKET_APP_PASSWORD":           enventry.ClassSecret,
		"BITBUCKET_PIPELINE_HISTORY_DEPTH": enventry.ClassSelector,
	} {
		if classes[name] != want {
			t.Errorf("%s is classed %q, want %q", name, classes[name], want)
		}
	}
}

// TestThisIsAMixedClassCollector. It is the first one, and the fact is what its
// install-table row and its worked entry are shaped by: an entry that carries an
// environment block ONLY when the selector is set, and never either credential.
func TestThisIsAMixedClassCollector(t *testing.T) {
	var secrets, selectors int
	for _, class := range enventry.Classes() {
		switch class {
		case enventry.ClassSecret:
			secrets++
		case enventry.ClassSelector:
			selectors++
		default:
			t.Errorf("a name is classed %q, which is neither class this collector declares", class)
		}
	}
	if secrets != 2 || selectors != 1 {
		t.Errorf("the closure is %d secret and %d selector, want 2 and 1", secrets, selectors)
	}
}

// TestIsDeclaredAnswersForBothDirections.
func TestIsDeclaredAnswersForBothDirections(t *testing.T) {
	for _, name := range enventry.Names() {
		if !enventry.IsDeclared(name) {
			t.Errorf("%q is in Names and IsDeclared says otherwise", name)
		}
	}
	for _, absent := range []string{
		"BITBUCKET_TOKEN", "GITHUB_TOKEN", "HOME", "PATH", "BITBUCKET_USERNAME_",
	} {
		if enventry.IsDeclared(absent) {
			t.Errorf("%q is not read by this collector and IsDeclared says it is", absent)
		}
	}
}

// TestTheNamesReadAndTheNamesDeclaredAreTheSameSet is the census, over the
// module's own source.
//
// IT READS THE ARGUMENT OF EVERY ENVIRONMENT LOOKUP, so a name introduced
// somewhere else in the module — a new tuning knob, a proxy variable, a home
// directory — is a red here rather than a variable the install table never
// carries.
func TestTheNamesReadAndTheNamesDeclaredAreTheSameSet(t *testing.T) {
	read := namesReadByTheModule(t)

	// THE KNOWN POSITIVE. Without it, a census that resolved no constants would
	// find nothing read and pass while the module read three names.
	if len(read) == 0 {
		t.Fatal("the census found no environment lookup in the module at all; it is not reading " +
			"the source")
	}

	declared := map[string]bool{}
	for _, name := range enventry.Names() {
		declared[name] = true
	}
	for name := range read {
		if !declared[name] {
			t.Errorf("the module reads %q and this package does not declare it, so the install "+
				"table will not carry it and the child will not have it", name)
		}
	}
	for name := range declared {
		if !read[name] {
			t.Errorf("this package declares %q and no code in the module reads it", name)
		}
	}
}

// namesReadByTheModule is every environment variable name the module looks up,
// resolved through this package's own constants.
//
// THE LOOKUPS NAME CONSTANTS RATHER THAN LITERALS, which is the point of the
// declaration, so the census resolves a selector expression against this
// package's exported names rather than reading a string out of the call.
func namesReadByTheModule(t *testing.T) map[string]bool {
	t.Helper()
	constants := map[string]string{
		"UsernameVariable":     enventry.UsernameVariable,
		"AppPasswordVariable":  enventry.AppPasswordVariable,
		"HistoryDepthVariable": enventry.HistoryDepthVariable,
	}
	lookup := regexp.MustCompile(`^(Getenv|LookupEnv)$`)

	out := map[string]bool{}
	for _, path := range moduleGoFiles(t) {
		// TEST FILES ARE EXCLUDED. A test sets and reads names deliberately —
		// including names it plants to prove a refusal — and counting those would
		// make this census assert about the suite rather than about the collector.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !lookup.MatchString(fn.Sel.Name) {
				return true
			}
			if len(call.Args) != 1 {
				return true
			}
			out[resolveName(t, call.Args[0], constants, path, fset)] = true
			return true
		})
	}
	delete(out, "")
	return out
}

// resolveName turns one lookup argument into the name it reads, failing loudly
// on an argument this census cannot resolve rather than skipping it.
func resolveName(
	t *testing.T, arg ast.Expr, constants map[string]string, path string, fset *token.FileSet,
) string {
	t.Helper()
	switch expr := arg.(type) {
	case *ast.SelectorExpr:
		if value, known := constants[expr.Sel.Name]; known {
			return value
		}
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			value, err := strconv.Unquote(expr.Value)
			if err == nil {
				// A LITERAL IS ITSELF A FINDING and the census returns it rather
				// than ignoring it: the declaration exists so no site spells a name
				// itself, and the assertion above will name it.
				return value
			}
		}
	}
	t.Errorf("%s:%d: an environment lookup takes an argument this census cannot resolve; a name "+
		"it cannot read is a name it cannot assert is declared",
		relativeToModule(t, path), fset.Position(arg.Pos()).Line)
	return ""
}

// moduleGoFiles is every Go file in the module, found by walking up to the
// go.mod this package belongs to.
func moduleGoFiles(t *testing.T) []string {
	t.Helper()
	root := moduleRoot(t)
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	return files
}

// moduleRoot walks up from this package to the directory carrying go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating this package: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above this package; the census has no module to read")
		}
		dir = parent
	}
}

func relativeToModule(t *testing.T, path string) string {
	t.Helper()
	relative, err := filepath.Rel(moduleRoot(t), path)
	if err != nil {
		return path
	}
	return relative
}
