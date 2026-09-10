// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// swallow_census_test.go — THE SWALLOWED-ERROR CENSUS over this whole module.
//
// THE DEFECT CLASS, AND WHY IT MATTERS HERE MORE THAN ANYWHERE. A read whose
// failure is discarded drops whatever that read carried while the walk still
// reports a complete collect — and a complete collect is what lets the receiving
// server treat everything the collect did not carry as gone. The provider this
// module reproduces does exactly that at nine separate sites: it logs a refused
// or failed per-repository read and continues, with its walk still asserting it
// saw the whole organization. This module reports every one of those instead, and
// this census is the standing structural half of that: it fails on the SHAPE,
// over every file at once, which also catches the next one.
//
// WHY A CENSUS RATHER THAN A UNIT TEST. Each individual site is covered by an
// input-class test in the enumerations' own package. What no unit test covers is
// the site that has not been written yet, and that is what a shape census is for.
//
// WHY THE GO PARSER AND NOT THE STRUCTURAL MATCHER. The parser used here parses
// every file completely, so its zero is a measurement. The known positive below
// is driven through the same parser in the same run, so a census that read
// nothing cannot report a clean module.

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
		// does not exist, so the value is used on success and the failure is
		// discarded.
		//
		// THE BINDING IS PART OF THE SHAPE, and leaving it out was measured to
		// produce five false positives in this module's own tests. The inverse
		// idiom `if _, err := f(); err == nil { t.Fatal(...) }` is an ASSERTION
		// that a call fails: it binds no value, so there is no value being kept
		// while a failure is dropped. What makes the defect a defect is exactly
		// that pairing, so the matcher requires it.
		name: "a two-value if-init that keeps a value on the err == nil arm, with no else",
		found: func(n ast.Node) bool {
			stmt, ok := n.(*ast.IfStmt)
			if !ok || stmt.Init == nil || stmt.Else != nil {
				return false
			}
			return comparesErrToNil(stmt.Cond, token.EQL) && bindsAValue(stmt.Init)
		},
	},
	{
		// `if err != nil { continue }`: the failure is swallowed by skipping the
		// item, which is the same loss with a different spelling — and it is the
		// exact spelling the source provider uses on three of its nine sites.
		name: "an error branch whose only statement is continue",
		found: func(n ast.Node) bool {
			stmt, ok := n.(*ast.IfStmt)
			if !ok || !comparesErrToNil(stmt.Cond, token.NEQ) || stmt.Body == nil {
				return false
			}
			if len(stmt.Body.List) != 1 {
				return false
			}
			branch, ok := stmt.Body.List[0].(*ast.BranchStmt)
			return ok && branch.Tok == token.CONTINUE
		},
	},
}

// bindsAValue reports whether an if-init assigns a named value beside the error.
// A statement binding only `_` and `err` keeps nothing, so a dropped failure
// there costs no data.
func bindsAValue(init ast.Stmt) bool {
	assign, ok := init.(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name == "_" || strings.Contains(strings.ToLower(ident.Name), "err") {
			continue
		}
		return true
	}
	return false
}

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
	// Any identifier whose name reads as an error, so a rename to listErr or
	// decodeErr does not slip past.
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

// allowedSwallows is THE ONE ALLOWED EXCEPTION LIST, stated rather than left
// implicit: nothing currently qualifies, and a future entry belongs here with the
// reason it is a recorded degrade rather than an oversight.
var allowedSwallows = map[string]string{}

// TestNoErrorIsSwallowedAnywhereInThisModule is the census.
func TestNoErrorIsSwallowedAnywhereInThisModule(t *testing.T) {
	files := moduleGoFiles(t)

	// THE KNOWN POSITIVE, so a walk that read nothing cannot pass. The parser is
	// driven over a source that DOES carry each shape, in this same run.
	assertTheCensusDetectsItsOwnShapes(t)

	if len(files) == 0 {
		t.Fatal("the census found no Go files; it is not reading the module")
	}

	var hits []string
	for _, path := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// A file this census cannot parse is a hole in it, so it is a failure
			// rather than a skip.
			t.Fatalf("parsing %s: %v", path, err)
		}
		relative := relativeToModule(t, path)
		if reason, allowed := allowedSwallows[relative]; allowed {
			t.Logf("%s: swallow allowed — %s", relative, reason)
			continue
		}
		for _, shape := range swallowedShapes {
			ast.Inspect(file, func(n ast.Node) bool {
				if n != nil && shape.found(n) {
					hits = append(hits, fmt.Sprintf("%s:%d — %s",
						relative, fset.Position(n.Pos()).Line, shape.name))
				}
				return true
			})
		}
	}
	if len(hits) > 0 {
		t.Errorf("%d error(s) are discarded in this module:\n  %s\n"+
			"A read whose failure is dropped loses whatever it carried while the walk still "+
			"reports a complete collect, which is what lets the server delete what the collect "+
			"could not see. Return the error and let the caller classify it.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// assertTheCensusDetectsItsOwnShapes drives each shape over a planted source that
// carries it. A shape whose matcher never fires would report a clean module for
// the wrong reason, and a census with two shapes needs two controls rather than
// one.
func assertTheCensusDetectsItsOwnShapes(t *testing.T) {
	t.Helper()
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
	}
	for i, shape := range swallowedShapes {
		parsed, err := parser.ParseFile(token.NewFileSet(), "planted.go", planted[i], 0)
		if err != nil {
			t.Fatalf("parsing the planted control for %q: %v", shape.name, err)
		}
		var found int
		ast.Inspect(parsed, func(n ast.Node) bool {
			if n != nil && shape.found(n) {
				found++
			}
			return true
		})
		if found != 1 {
			t.Fatalf("the planted control for %q produced %d hits, want 1; the census is not "+
				"detecting the shape and its zero below would mean nothing", shape.name, found)
		}
	}
}

func relativeToModule(t *testing.T, path string) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		return path
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return relative
}

// incompleteReasonShapes are the ways framework.Incomplete can be called with a
// reason that says nothing.
//
// WHY THIS BELONGS BESIDE THE SWALLOW CENSUS. Both are the same defect one step
// apart: a swallowed error is a walk that does not know it missed something, and
// an EMPTY incompleteness reason is a walk that knows and cannot say. The
// framework refuses the second when the envelope is encoded, naming the
// collector — so an empty reason does not mark a collect, it FAILS one, turning
// a partial read into a failed collect at the point where the operator can least
// tell why.
//
// THE PROPERTY HOLDS TODAY BY COUNT: this module has exactly one call site and
// its argument is a non-empty format. What no unit test covers is the NEXT call
// site, which is what a shape census is for.
var incompleteReasonShapes = []swallowedShape{
	{
		name: "a framework.Incomplete call with no argument, or with an empty string literal",
		found: func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isFrameworkIncomplete(call.Fun) {
				return false
			}
			if len(call.Args) == 0 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			return ok && literal.Kind == token.STRING && isEmptyStringLiteral(literal.Value)
		},
	},
}

// isFrameworkIncomplete reports whether an expression names framework.Incomplete.
func isFrameworkIncomplete(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Incomplete" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "framework"
}

// isEmptyStringLiteral reports whether a string literal's VALUE is empty, in
// either quoting form, so a raw-string spelling is not a way past this.
func isEmptyStringLiteral(literal string) bool {
	unquoted, err := strconv.Unquote(literal)
	return err == nil && strings.TrimSpace(unquoted) == ""
}

// TestNoIncompleteWalkCarriesAnEmptyReason is the census.
func TestNoIncompleteWalkCarriesAnEmptyReason(t *testing.T) {
	files := moduleGoFiles(t)
	if len(files) == 0 {
		t.Fatal("the census found no Go files; it is not reading the module")
	}

	// THE KNOWN POSITIVE, driven through the same matcher in the same run over a
	// planted source that carries each spelling.
	for _, planted := range []string{
		"package p\nfunc f() any { return framework.Incomplete() }",
		"package p\nfunc f() any { return framework.Incomplete(\"\") }",
		"package p\nfunc f() any { return framework.Incomplete(`   `) }",
	} {
		parsed, err := parser.ParseFile(token.NewFileSet(), "planted.go", planted, 0)
		if err != nil {
			t.Fatalf("parsing the planted control: %v", err)
		}
		var found int
		ast.Inspect(parsed, func(n ast.Node) bool {
			if n != nil && incompleteReasonShapes[0].found(n) {
				found++
			}
			return true
		})
		if found != 1 {
			t.Fatalf("the planted control produced %d hits, want 1; the census is not detecting "+
				"the shape and its zero below would mean nothing", found)
		}
	}

	// AND THE CALL SITE EXISTS, so a census over a module that stopped calling
	// framework.Incomplete at all cannot report clean for the wrong reason.
	var callSites int
	var hits []string
	for _, path := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		relative := relativeToModule(t, path)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isFrameworkIncomplete(call.Fun) {
				return true
			}
			callSites++
			if incompleteReasonShapes[0].found(n) {
				hits = append(hits, fmt.Sprintf("%s:%d", relative, fset.Position(n.Pos()).Line))
			}
			return true
		})
	}
	if callSites == 0 {
		t.Fatal("this module calls framework.Incomplete nowhere. A walk with no incompleteness " +
			"arm asserts it saw the whole workspace on every input, which is what lets the " +
			"server delete what a refused read could not carry")
	}
	if len(hits) > 0 {
		t.Errorf("%d incompleteness assertion(s) carry an empty reason:\n  %s\n"+
			"The framework REFUSES a reasonless incomplete walk when the envelope is encoded, so "+
			"this does not mark a collect — it fails one, and the operator is told less than "+
			"they were before.", len(hits), strings.Join(hits, "\n  "))
	}
}
