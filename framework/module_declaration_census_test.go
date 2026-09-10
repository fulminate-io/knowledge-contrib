// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// module_declaration_census_test.go — EVERY COLLECTOR MODULE IN THIS TREE
// DECLARES ITSELF.
//
// THE PIN IS FOR THE MODULES THAT ARE NOT HERE YET. Three more collector
// modules are in flight on other branches, and three language ports come after
// them; the describe tool is REQUIRED, so a module that lands without a
// declaration is a collector no operator can register. A row naming today's
// eight would say nothing about the ninth — so this census enumerates modules by
// THE PRESENCE OF A go.mod under cmd/collectors, which is the same authority the
// collector test target uses, and reds the moment one of them lands without a
// Describe method.
//
// IT READS SOURCE RATHER THAN LINKING THE MODULES. A collector module cannot be
// imported from here: each is its own Go module with its own dependency set, and
// building eight of them to ask one structural question would make this census
// slower than the suite it guards. The compiler already answers the harder
// half — a module whose Describe is missing or wrongly typed does not build at
// all, and its own suite says so — so what this adds is the module nobody
// remembered to look at.
//
// ITS OWN KNOWN POSITIVE IS THE MODULE COUNT: a census that discovered nothing
// passes exactly as loudly as one that discovered everything and found no gap,
// so a floor below today's set is asserted first.

// nonCollectorModules are the module directories under cmd/collectors that serve
// no tool: this framework, and the shared libraries collectors import. They are
// excluded BY NAME rather than by an allowlist of the ones that are collectors,
// so a new collector is picked up automatically and a new shared module is not.
var nonCollectorModules = []string{"framework", "common"}

// generatorModuleFinder is the installer table generator's own module discovery,
// lifted from scripts/gen-collector-tables.sh so this census is checked against a
// SECOND INDEPENDENT IMPLEMENTATION of "which directories are the collector
// modules" rather than against a number.
//
// ITS ROOT IS A PARAMETER WHERE THE GENERATOR'S IS THE REPOSITORY. The generator
// walks "$ROOT/cmd/collectors" because it only ever runs in the repository; this
// census also runs in the PUBLISHED layout, where the modules sit directly under
// the tree root and no cmd/collectors path exists. Copying that path literally
// made the pipeline find nothing there, and "found nothing" is the one answer a
// cross-check must never accept quietly. The pipeline is otherwise the
// generator's: the same find, the same relative-path sed, the same two-name
// exclusion, the same sort — and the depth bounds hold in both layouts, because
// a module's go.mod is two components below the collectors directory either way.
//
// WHY THIS REPLACED A FLOOR. The known positive here used to be a hand-written
// floor: "at least ten modules, or the discovery is broken". A floor weakens on
// its own with every landing — it was ten against eleven modules the day this was
// written — and it can only ever catch a discovery that found almost nothing. It
// cannot catch the discovery that found ten of eleven, which is the failure that
// actually matters, because a module the walk misses is a module whose missing
// declaration nothing here reports.
//
// A SECOND IMPLEMENTATION CATCHES BOTH. The generator finds modules with `find`
// and a path-shaped exclusion; this file walks with filepath.WalkDir and a
// top-segment exclusion. They agree today at eleven; a module either walk misses
// makes them disagree by name, and the count is whatever the tree holds rather
// than a number anyone has to maintain.
const generatorModuleFinder = `find "$DIR" -name go.mod -mindepth 2 -maxdepth 3 -exec dirname {} \; |` +
	` sed "s|^$DIR/||" | grep -vE '^(framework|common(/.*)?)$' | sort`

// generatorModules runs that finder and returns what it found.
func generatorModules(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("bash", "-c", generatorModuleFinder)
	cmd.Env = append(os.Environ(), "DIR="+root)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the generator's own module discovery: %v", err)
	}
	var modules []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			modules = append(modules, line)
		}
	}
	return modules
}

func TestEveryCollectorModuleDeclaresItself(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving the collectors directory: %v", err)
	}
	modules := collectorModules(t, root)

	// THE KNOWN POSITIVE IS AGREEMENT WITH A SECOND WALK, not a floor. A census
	// that discovered nothing passes exactly as loudly as one that discovered
	// everything and found no gap, and one that discovered all but one module
	// passes more quietly still — so what is asserted is that this walk and the
	// installer generator's independent one name the SAME SET.
	generated := generatorModules(t, root)
	if len(generated) == 0 {
		t.Fatalf("the generator's own module discovery found nothing under %s; the cross-check has no authority "+
			"and this census would pass over an empty walk", root)
	}
	if !slices.Equal(modules, generated) {
		t.Fatalf("this census walks %v and the installer table generator's own discovery finds %v.\n"+
			"They are two implementations of one question — which directories under cmd/collectors are the "+
			"collector modules — and a module only one of them sees is a module whose declaration one of the two "+
			"gates never checks.", modules, generated)
	}
	for _, module := range modules {
		if !declaresItself(t, filepath.Join(root, module)) {
			t.Errorf("the collector module %q implements no Describe() method, so it serves no declaration — "+
				"the contract requires the describe tool, and `knowledge collector add` refuses a provider without it",
				module)
		}
	}
}

// collectorModules returns every collector module directory under root, by the
// presence of a go.mod.
func collectorModules(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "go.mod" {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		top := strings.Split(rel, string(filepath.Separator))[0]
		if slices.Contains(nonCollectorModules, top) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s for collector modules: %v", root, err)
	}
	slices.Sort(out)
	return out
}

// declaresItself reports whether any non-test Go file in the module tree
// declares a method named Describe returning a Declaration.
//
// IT MATCHES ON THE RETURN TYPE, not on the name alone: `Describe` is an
// ordinary word and a helper of that name proves nothing about the contract.
func declaresItself(t *testing.T, moduleDir string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(moduleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			//nolint:nilerr // an unparsable file is the compiler's finding, not this census's.
			return nil
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Describe" {
				continue
			}
			if returnsDeclaration(fn) {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", moduleDir, err)
	}
	return found
}

// returnsDeclaration reports whether fn returns exactly one value whose type
// names a Declaration, qualified by the framework package or not.
func returnsDeclaration(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	switch typ := fn.Type.Results.List[0].Type.(type) {
	case *ast.Ident:
		return typ.Name == "Declaration"
	case *ast.SelectorExpr:
		return typ.Sel.Name == "Declaration"
	default:
		return false
	}
}
