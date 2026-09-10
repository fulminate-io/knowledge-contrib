// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// callsite_census_test.go — THE SHARED PARSE the two call-site censuses run on,
// and the reason both of them exist.
//
// THE DEFECT CLASS THIS FILE ANSWERS. Two of this collector's requirements —
// "every list reads to exhaustion" and "every failed read reaches the
// completeness verdict" — were implemented as a shared helper and a boundary,
// and were tested THERE. paginate has seven tests over its four token
// conventions; runServiceWalks has a partial-walk test. Both were green while
// four SDK calls never entered paginate and six read failures never reached the
// verdict, because nothing asserted which CALL SITES use them. A test of a helper
// reads like a test of the requirement and is not one.
//
// SO THE CARRIER IS A CENSUS OVER THE SITES. It parses this package's own
// non-test sources, finds every `w.clients.<Service>.<Operation>(...)` call, and
// asks a question of each one that the helper's own tests cannot ask. A new
// service walk written next year is covered the day it is written, with nobody
// remembering these rules.
//
// IT PARSES RATHER THAN GREPS on purpose. The question "is this call lexically
// inside a paginate call" is about the syntax tree; a regular expression over
// text cannot answer it, and a census that answers it wrongly in the permissive
// direction is worse than no census.

// sdkCallSite is one `w.clients.<Service>.<Operation>(...)` call in this package.
type sdkCallSite struct {
	// Service is the field name on Clients, e.g. "ELBv2".
	Service string
	// Operation is the SDK method, e.g. "DescribeListeners".
	Operation string
	// SDKPackage is the import path of the SDK package the operation belongs to,
	// e.g. "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2".
	//
	// IT IS RESOLVED FROM THE FILE'S OWN IMPORT TABLE, never guessed from the
	// qualifier. Every field on Clients is a NARROW INTERFACE rather than the
	// SDK's concrete client, so the field type names nothing about the SDK; and
	// the qualifier is not the last path segment either — elasticloadbalancingv2
	// is imported as elbv2 in the file that calls it. The only correct source is
	// the import spec the qualifier resolves to in that file.
	SDKPackage string
	// File and Line locate it for the failure message.
	File string
	Line int
	// InsidePaginate is true when the call is lexically within a paginate(...)
	// call — that is, when it is the thing paginate drives.
	InsidePaginate bool
	// ErrorReaches records what the enclosing function does with this call's
	// error. See errorDisposition.
	ErrorReaches errorDisposition
}

// errorDisposition is what happens to a call's error.
type errorDisposition int

const (
	// errUnknown means the census could not classify it, which is a FAILURE
	// rather than a pass: an unclassifiable disposition is exactly where a
	// swallowed error hides.
	errUnknown errorDisposition = iota
	// errReturned means the error leaves the function, so a service walk's
	// failure reaches runServiceWalks.
	errReturned
	// errRecorded means the function hands it to recordSubreadFailure, so a
	// per-resource failure reaches the completeness verdict by the other route.
	errRecorded
)

func (d errorDisposition) String() string {
	switch d {
	case errReturned:
		return "returned"
	case errRecorded:
		return "recorded"
	default:
		return "NEITHER"
	}
}

// sdkCallSites parses this package's non-test sources and returns every SDK call
// site in a stable order.
func sdkCallSites(t *testing.T) []sdkCallSite {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("the census found no non-test Go files in this package, so it would pass on nothing")
	}

	fset := token.NewFileSet()
	var sites []sdkCallSite
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		sites = append(sites, callSitesInFile(fset, file, name)...)
	}
	return sites
}

// callSitesInFile walks one file's declarations, tracking both the enclosing
// function (for the error disposition) and whether the current position is inside
// a paginate call.
func callSitesInFile(fset *token.FileSet, file *ast.File, name string) []sdkCallSite {
	var sites []sdkCallSite
	imports := importsOf(file)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// THE DISPOSITION IS A PROPERTY OF THE ENCLOSING FUNCTION, computed once:
		// a function that calls recordSubreadFailure anywhere in its body routes
		// its read failures there, and one whose body returns an error value
		// propagates them. Both are computed before the walk so a call site can
		// be stamped as it is found.
		records := containsCallNamed(fn.Body, "recordSubreadFailure")
		returnsErr := functionReturnsError(fn)
		resolve := func(operation string) string { return sdkPackageFor(fn.Body, imports, operation) }

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isCallNamed(call, "paginate") {
				// EVERYTHING BELOW THIS NODE IS INSIDE THE PAGINATOR. ast.Inspect
				// has no exit hook to lower a depth counter on, so the subtree is
				// walked separately with the flag raised and the outer walk is
				// told to skip it.
				for _, arg := range call.Args {
					sites = append(sites, callSitesInSubtree(fset, arg, name, true, records, returnsErr, resolve)...)
				}
				return false
			}
			if svc, op, ok := sdkCall(call); ok {
				sites = append(sites, sdkCallSite{
					Service: svc, Operation: op, SDKPackage: resolve(op),
					File: name, Line: fset.Position(call.Pos()).Line,
					InsidePaginate: false,
					ErrorReaches:   dispositionFor(records, returnsErr),
				})
			}
			return true
		})
	}
	return sites
}

// callSitesInSubtree collects the SDK calls under one node with a fixed
// inside-paginate answer.
func callSitesInSubtree(
	fset *token.FileSet, root ast.Node, name string, insidePaginate, records, returnsErr bool,
	resolve func(operation string) string,
) []sdkCallSite {
	var sites []sdkCallSite
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if svc, op, ok := sdkCall(call); ok {
			sites = append(sites, sdkCallSite{
				Service: svc, Operation: op, SDKPackage: resolve(op),
				File: name, Line: fset.Position(call.Pos()).Line,
				InsidePaginate: insidePaginate,
				ErrorReaches:   dispositionFor(records, returnsErr),
			})
		}
		return true
	})
	return sites
}
