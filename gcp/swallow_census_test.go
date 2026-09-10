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
)

// swallow_census_test.go — THE SWALLOWED-ERROR CENSUS over this whole module.
//
// THE DEFECT CLASS. A read whose failure is discarded drops whatever that read
// carried while the walk still reports a complete collect. It landed once here,
// on a dataset detail read: `if detail, err := ...Get(...); err == nil { ... }`
// with no else, which dropped the dataset's encryption relationship and said
// nothing. The same hazard is handled the opposite way three files over, so this
// was an inconsistency rather than a decision.
//
// WHY A CENSUS RATHER THAN A UNIT TEST. The site is inside a method that needs a
// real client, so no offline test can drive it. What IS checkable offline is the
// SHAPE, over every file in the module at once, which also catches the next one.
//
// WHY THIS AND NOT THE PATTERN TOOL. The structural matcher reports two files in
// this module as partially parsed, so its zero comes with an explicit warning
// that a construct inside a recovered region could be missing from the tree it
// walked. The Go parser used here parses every file completely, so its zero is
// a measurement rather than a hedge. The matcher was run too, with a planted
// positive, and agreed.

// swallowedShape describes one way an error is discarded.
type swallowedShape struct {
	name string
	// found reports whether this node is an instance of the shape.
	found func(ast.Node) bool
}

// swallowedShapes are the two forms this class takes in Go. Both are checked,
// because the second is where the first hides after a trivial rewrite.
var swallowedShapes = []swallowedShape{
	{
		// `if v, err := f(); err == nil { ... }` with NO else: the failure arm
		// does not exist, so the failure is discarded.
		name: "a two-value if-init taking only the err == nil arm, with no else",
		found: func(n ast.Node) bool {
			stmt, ok := n.(*ast.IfStmt)
			if !ok || stmt.Init == nil || stmt.Else != nil {
				return false
			}
			return isErrEqualsNil(stmt.Cond)
		},
	},
	{
		// `if err != nil { continue }`: the failure is swallowed by skipping the
		// item, which is the same loss with a different spelling.
		name: "an error branch whose only statement is continue",
		found: func(n ast.Node) bool {
			stmt, ok := n.(*ast.IfStmt)
			if !ok || !isErrNotEqualsNil(stmt.Cond) || stmt.Body == nil {
				return false
			}
			if len(stmt.Body.List) != 1 {
				return false
			}
			branch, ok := stmt.Body.List[0].(*ast.BranchStmt)
			return ok && branch.Tok == token.CONTINUE
		},
	},
	{
		// `_ = err`: an error explicitly assigned to the blank identifier. It is
		// the PLAINEST member of the class and the census did not carry it —
		// measured on this module, ten separate completeness-recording calls were
		// replaced with `_ = err` and this census reported clean on every one.
		//
		// THE LEFT-HAND SIDE IS THE WHOLE TEST, and it has to be: an error
		// assigned to a named variable is not discarded, it is held, and whether
		// the holder checks it is a different question this shape cannot answer.
		// A blank identifier says the value is going nowhere, which is the class.
		name: "an error assigned to the blank identifier",
		found: func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return false
			}
			lhs, ok := assign.Lhs[0].(*ast.Ident)
			if !ok || lhs.Name != "_" {
				return false
			}
			// The right-hand side reads as an error: an identifier whose name says
			// so. A call is deliberately NOT matched — `_ = f()` discards whatever
			// f returns and most of those are not errors, so matching it would
			// report the ordinary deferred-close idiom as a swallow.
			rhs, ok := assign.Rhs[0].(*ast.Ident)
			return ok && strings.Contains(strings.ToLower(rhs.Name), "err")
		},
	},
}

func isErrEqualsNil(cond ast.Expr) bool { return comparesErrToNil(cond, token.EQL) }

func isErrNotEqualsNil(cond ast.Expr) bool { return comparesErrToNil(cond, token.NEQ) }

func comparesErrToNil(cond ast.Expr, op token.Token) bool {
	binary, ok := cond.(*ast.BinaryExpr)
	if !ok || binary.Op != op {
		return false
	}
	left, leftOK := binary.X.(*ast.Ident)
	right, rightOK := binary.Y.(*ast.Ident)
	if !leftOK || !rightOK || right.Name != "nil" {
		return false
	}
	// Any identifier whose name reads as an error, so a rename to cfgErr or
	// listErr does not slip past.
	return strings.Contains(strings.ToLower(left.Name), "err")
}

// moduleGoFiles is every Go file in this module, tests included: a swallow in a
// test helper hides one just as well.
func moduleGoFiles(t *testing.T) []string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the module: %v", err)
	}
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

// TestNoErrorIsSwallowedAnywhereInThisModule is the census.
//
// THE ONE ALLOWED EXCEPTION is stated below rather than left implicit: nothing
// currently qualifies, and a future one belongs here with the reason it is a
// recorded degrade rather than an oversight.
var allowedSwallows = map[string]string{}

func TestNoErrorIsSwallowedAnywhereInThisModule(t *testing.T) {
	files := moduleGoFiles(t)

	// THE KNOWN POSITIVE, so a walk that read nothing cannot pass. The parser is
	// driven over a source that DOES carry each shape, in this same run.
	//
	// ONE CONTROL PER SHAPE, and that is not symmetry for its own sake: a single
	// control proves one matcher and leaves every other shape an assertion. The
	// third shape below was added after ten deliberate `_ = err` mutations in a
	// sibling module went undetected by a census that declared two shapes and
	// controlled one.
	planted := []string{
		`package p
func f() {
	if v, err := g(); err == nil {
		_ = v
	}
}`,
		`package p
func f() {
	for range 3 {
		if err := g(); err != nil {
			continue
		}
	}
}`,
		`package p
func f() {
	err := g()
	_ = err
}`,
	}
	if len(planted) != len(swallowedShapes) {
		t.Fatalf("%d planted controls for %d shapes; a shape with no control is a matcher this "+
			"census never proves fires", len(planted), len(swallowedShapes))
	}
	for i, shape := range swallowedShapes {
		parsed, err := parser.ParseFile(token.NewFileSet(), "planted.go", planted[i], 0)
		if err != nil {
			t.Fatalf("parsing the planted control for %q: %v", shape.name, err)
		}
		var plantedHits int
		ast.Inspect(parsed, func(n ast.Node) bool {
			if n != nil && shape.found(n) {
				plantedHits++
			}
			return true
		})
		if plantedHits != 1 {
			t.Fatalf("the planted control for %q produced %d hits, want 1; the census is not "+
				"detecting the shape and its zero below would mean nothing", shape.name, plantedHits)
		}
	}

	if len(files) == 0 {
		t.Fatal("the census found no Go files; it is not reading the module")
	}

	var hits []string
	for _, path := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// A file this census cannot parse is a hole in it, so it is a
			// failure rather than a skip.
			t.Fatalf("parsing %s: %v", path, err)
		}
		rel := relativeToModule(t, path)
		if reason, allowed := allowedSwallows[rel]; allowed {
			t.Logf("%s: swallow allowed — %s", rel, reason)
			continue
		}
		for _, shape := range swallowedShapes {
			ast.Inspect(file, func(n ast.Node) bool {
				if n != nil && shape.found(n) {
					hits = append(hits, fmt.Sprintf("%s:%d — %s",
						rel, fset.Position(n.Pos()).Line, shape.name))
				}
				return true
			})
		}
	}
	if len(hits) > 0 {
		t.Errorf("%d error(s) are discarded in this module:\n  %s\n"+
			"A read whose failure is dropped loses whatever it carried while the walk still "+
			"reports a complete collect. Return the error and let the caller classify it.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

func relativeToModule(t *testing.T, path string) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
