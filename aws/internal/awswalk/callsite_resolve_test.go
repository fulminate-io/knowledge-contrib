// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"go/ast"
	"strings"
)

// callsite_resolve_test.go — HOW A CALL SITE IS RESOLVED TO ITS SDK PACKAGE, and
// why neither the receiver nor the method name can answer it.
//
// EVERY FIELD ON Clients IS A NARROW INTERFACE rather than the SDK's concrete
// client, which is what lets the whole walk be tested without a credential — and
// it means the field type names nothing about the SDK. The method name does not
// either. What DOES name it is the operation's input literal, which every one of
// these calls constructs in the same function, and whose package qualifier
// resolves through that file's own import table.
//
// THE IMPORT TABLE IS NOT OPTIONAL: elasticloadbalancingv2 is imported as elbv2
// in the file that calls it, so taking the last path segment of a guess would
// resolve to a module that does not exist.

// importsOf maps each import's local name to its path for one file, taking the
// EXPLICIT ALIAS where there is one.
func importsOf(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		local := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			local = path[i+1:]
		}
		if spec.Name != nil {
			local = spec.Name.Name
		}
		out[local] = path
	}
	return out
}

// sdkPackageFor resolves the SDK package an operation belongs to, by finding the
// composite literal of its INPUT type inside the enclosing function and looking
// that literal's package qualifier up in the file's imports.
//
// THE INPUT LITERAL IS THE ONLY LOCAL EVIDENCE. The call is made through a narrow
// interface, so neither the receiver nor the method says which SDK package the
// operation lives in. Every one of these calls constructs its own
// `<pkg>.<Operation>Input{...}` somewhere in the same function — sometimes inline
// as the argument, sometimes as a variable built a few lines above — and that
// literal names the package. An unresolved package returns empty and the census
// treats it as a failure rather than skipping the site.
func sdkPackageFor(body ast.Node, imports map[string]string, operation string) string {
	want := operation + "Input"
	found := ""
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || found != "" {
			return found == ""
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != want {
			return true
		}
		qualifier, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		found = imports[qualifier.Name]
		return found == ""
	})
	return found
}

// dispositionFor turns the two facts about an enclosing function into one
// disposition. RECORDED wins over RETURNED because a function that records is
// making the deliberate per-resource choice; a function that does neither is the
// defect.
func dispositionFor(records, returnsErr bool) errorDisposition {
	switch {
	case records:
		return errRecorded
	case returnsErr:
		return errReturned
	default:
		return errUnknown
	}
}

// sdkCall reports whether a call expression is `w.clients.<Service>.<Op>(...)`
// and returns the service field and operation names.
func sdkCall(call *ast.CallExpr) (service, operation string, ok bool) {
	// The shape is SelectorExpr{ X: SelectorExpr{ X: SelectorExpr{X: w, Sel: clients}, Sel: Service }, Sel: Op }.
	op, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	svc, ok := op.X.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	clients, ok := svc.X.(*ast.SelectorExpr)
	if !ok || clients.Sel.Name != "clients" {
		return "", "", false
	}
	return svc.Sel.Name, op.Sel.Name, true
}

// isCallNamed reports whether a call is to a plain identifier of the given name.
func isCallNamed(call *ast.CallExpr, name string) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == name
}

// containsCallNamed reports whether a subtree calls the named function, either as
// a bare identifier or as a method on a receiver.
func containsCallNamed(root ast.Node, name string) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if fun.Name == name {
				found = true
			}
		case *ast.SelectorExpr:
			if fun.Sel.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// functionReturnsError reports whether a function declares an error result, which
// is what lets its caller see a read failure.
func functionReturnsError(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, r := range fn.Type.Results.List {
		if id, ok := r.Type.(*ast.Ident); ok && id.Name == "error" {
			return true
		}
	}
	return false
}
