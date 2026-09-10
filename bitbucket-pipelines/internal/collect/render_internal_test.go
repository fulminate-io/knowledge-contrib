// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// render_internal_test.go — A RESOURCE WHOSE DETAIL CANNOT BE RENDERED IS
// REFUSED, NOT FLATTENED.
//
// WHY THIS IS AN INTERNAL TEST AND WHY THE ARM IS UNREACHABLE FROM OUTSIDE.
// Every value the six converters marshal today is one of this package's own API
// structs — strings, bools, ints and structs of those — and encoding/json cannot
// fail on any of them, so no recorded response, no injected status and no
// fixture can drive a converter into this arm. What can drive it is a CHANGE: a
// marshaled type gaining a channel, a function, a cyclic pointer or a
// json.Marshaler of its own. That change produces neither a compile error nor a
// red test elsewhere; it produces a node with an EMPTY content, in production,
// on a collect that reported success.
//
// So the arm is exercised where it can be: at the one function every converter
// renders through, with a value json genuinely cannot marshal.

// unmarshalable is a value encoding/json refuses. A channel is the shortest one
// and needs no cooperation from a Marshaler.
type unmarshalable struct {
	Ch chan int `json:"ch"`
}

// TestAValueThatCannotBeRenderedIsRefusedAndRecorded.
func TestAValueThatCannotBeRenderedIsRefusedAndRecorded(t *testing.T) {
	var failures reads

	content, ok := renderContent(&failures, "acme/api pipeline default", unmarshalable{})
	if ok {
		t.Fatal("a value json cannot marshal was accepted; the caller would emit a node whose " +
			"content is the empty string, which downstream is indistinguishable from a resource " +
			"the provider said nothing about")
	}
	if content != "" {
		t.Errorf("the refused render returned %q rather than nothing", content)
	}

	err := failures.err("bitbucket-pipelines-config")
	if err == nil {
		t.Fatal("the refusal was not recorded, so the walk would still assert it saw everything")
	}
	// IT IS THE PARTIAL CLASS. The provider answered; this collector could not
	// carry what it answered, which is a failure of this run rather than of the
	// credential or of the rate limit.
	if !errors.Is(err, ErrPartial) {
		t.Errorf("a render failure was not classified as a partial read: %v", err)
	}
	if errors.Is(err, ErrDenied) || errors.Is(err, ErrRateLimited) {
		t.Errorf("a render failure was classified as a refusal or a rate limit: %v", err)
	}
	// AND THE REASON NAMES THE RESOURCE, so an operator can go and look at it.
	for _, want := range []string{"acme/api pipeline default", "rendering its detail"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the reason does not name %q: %v", want, err)
		}
	}
}

// TestAValueThatRendersIsAccepted is the same-run known positive: renderContent
// refuses what json refuses rather than refusing everything, and it returns the
// document a node carries.
func TestAValueThatRendersIsAccepted(t *testing.T) {
	var failures reads

	content, ok := renderContent(&failures, "acme/api", variableDetail{
		Key: "API_KEY", Scope: scopeRepository, Secured: true,
	})
	if !ok {
		t.Fatal("an ordinary detail struct was refused")
	}
	if content != `{"key":"API_KEY","scope":"repository","secured":true}` {
		t.Errorf("the rendered content is %q", content)
	}
	if err := failures.err("bitbucket-variables"); err != nil {
		t.Errorf("a successful render recorded a failure: %v", err)
	}
}

// TestEveryConverterRendersThroughTheOneFunction is the structural half, and it
// is what keeps the rows above from being about a helper nobody calls.
//
// A CONVERTER THAT WENT BACK TO A BARE MARSHAL would pass every behavioral test
// in this package: its node's content would be right on every input a fixture
// can supply, and wrong only on the input no fixture can supply.
//
// THE ASSERTION IS SPELLING-INDEPENDENT, and that is the point. A line matcher
// for `x, _ := json.Marshal(v)` was measured to miss two spellings of the same
// defect — binding the error and ignoring it, and assigning to an already
// declared pair — both of which produce exactly the failure this exists to stop.
// So what is asserted is not a shape of the discard but the ONE PERMITTED
// HOLDER: no function in this package's non-test source may call json.Marshal
// except renderContent, which returns the error. Every discard spelling, present
// and future, reds it.
//
// IT TRAVELS WITH THE MODULE. A corpus check in this repository asserts a
// narrower version of the class tree-wide; this one runs in the published
// repository too, where that check does not.
func TestEveryConverterRendersThroughTheOneFunction(t *testing.T) {
	files := packageSourceFiles(t)
	if len(files) == 0 {
		t.Fatal("the census found no source files in this package; it is not reading anything")
	}

	// THE KNOWN POSITIVE, through the same resolver in the same run, over all
	// three spellings — so a resolver that found nothing still fails loud.
	assertTheCensusFindsEverySpelling(t)

	var holders []string
	for path, source := range files {
		holders = append(holders, marshalHolders(t, path, source)...)
	}
	if len(holders) == 0 {
		t.Fatal("the census found no json.Marshal call at all in this package; renderContent " +
			"holds one, so a zero here means the resolver is not reading the source")
	}
	for _, holder := range holders {
		if strings.HasSuffix(holder, ":renderContent") {
			continue
		}
		t.Errorf("%s calls json.Marshal. Every node's content is rendered through renderContent, "+
			"which returns the error and lets the caller refuse the resource; a marshal anywhere "+
			"else is one whose error can be dropped, and a node whose content is the empty "+
			"string is indistinguishable downstream from a resource the provider said nothing "+
			"about, on a collect that reports success", holder)
	}
}

// assertTheCensusFindsEverySpelling drives the resolver over planted sources
// carrying each way the discard is written, so the census's zero is a
// measurement rather than a matcher that never fires.
func assertTheCensusFindsEverySpelling(t *testing.T) {
	t.Helper()
	for _, planted := range []struct{ name, source string }{
		{"the two-value short declaration", `package p
func plantedConverter(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}`},
		{"the error bound and ignored", `package p
func plantedConverter(v any) string {
	raw, err := json.Marshal(v)
	_ = err
	return string(raw)
}`},
		{"the assignment to a declared pair", `package p
func plantedConverter(v any) string {
	var raw []byte
	raw, _ = json.Marshal(v)
	return string(raw)
}`},
	} {
		holders := marshalHolders(t, "planted.go", planted.source)
		if len(holders) != 1 || !strings.HasSuffix(holders[0], ":plantedConverter") {
			t.Fatalf("the resolver found %v in %s; it must find the one planted holder, or its "+
				"zero over this package means nothing", holders, planted.name)
		}
	}
}

// marshalHolders is every function in one source file that calls json.Marshal,
// as "<file>:<function>".
//
// IT READS THE ENCLOSING FUNCTION rather than the assignment, which is what
// makes it blind to how the result is bound — the property the test's name
// promises and a line matcher cannot keep.
func marshalHolders(t *testing.T, path, source string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var holders []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Marshal" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "json" {
				return true
			}
			holders = append(holders, filepath.Base(path)+":"+fn.Name.Name)
			return false
		})
	}
	return holders
}

// packageSourceFiles is every non-test Go file of this package, by path.
func packageSourceFiles(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading this package's directory: %v", err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		out[name] = string(raw)
	}
	return out
}
