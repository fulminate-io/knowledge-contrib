// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// census_test.go — the two MODULE-LOCAL CENSUSES that carry this module's
// structural requirements, because neither is expressible as a shape check on
// its own.
//
// ONE: THIS COLLECTOR SPAWNS NO SUBPROCESS. It reads the Kubernetes API through
// client-go and never by shelling out to kubectl. A corpus check keyed on the
// call would catch `exec.Command("kubectl", ...)` under that spelling and would
// NOT catch the same call through a package imported under an alias, because a
// pattern language matches the receiver's TEXT and an alias changes it. The
// census below reads the IMPORT DECLARATIONS, so it resolves an alias to the
// path it names and the alias gap closes.
//
// TWO: THIS MODULE IMPORTS NOTHING OF OURS BUT THE TWO COMMON MODULES. The
// compiler refuses cmd/knowledge, cmd/knowledge-server and gen/ today, but only
// because they belong to modules this one does not require — a property a future
// require would silently remove, and the framework and correlation requires are
// proof that adding one is a thing this module does. The census states the rule
// rather than resting on the toolchain's current answer.
//
// BOTH ARE WALKED WITH go/parser OVER THE MODULE'S OWN NON-TEST SOURCE.
// _test.go files are excluded deliberately: the stdio round trip re-execs this
// test binary, which is a spawn by design and one no deployed collector
// performs.

// spawnPackages are the packages whose use is a process spawn or can be one.
//
// os/exec is listed whole: every exported thing in it spawns. os and syscall are
// listed because they carry the raw spawn entry points, and for those the census
// also checks WHICH member is used — os in particular is imported by ordinary
// code for reasons that have nothing to do with spawning, so flagging the import
// alone would flag every file and mean nothing.
var spawnPackages = map[string]spawnRule{
	"os/exec": {wholePackage: true},
	"os":      {members: []string{"StartProcess"}},
	"syscall": {members: []string{"Exec", "ForkExec", "StartProcess", "CreateProcess"}},
}

type spawnRule struct {
	// wholePackage flags any use of the package at all.
	wholePackage bool
	// members flags only these selectors.
	members []string
}

// knowledgeModulePrefixes are the trees this module's purity lock is about.
//
// TWO ENTRIES, AND THE SECOND IS NOT REDUNDANT WITH THE FIRST IN EVERY TREE THIS
// FILE LIVES IN. scripts/sync-to-contrib.sh publishes these modules to
// knowledge-contrib and rewrites github.com/fulminate-io/knowledge-contrib
// to that repository's own path, so in the published tree the second entry
// becomes the published prefix and matches every import of ours, while the first
// matches only the client and server paths that must never appear. With the
// upstream prefix alone this census would read ZERO imports there and report a
// clean module — which is exactly what the known positive at the end of
// TestOnlyTheCommonModulesAreImportedFromTheKnowledgeTree exists to catch, and
// it did catch it.
var knowledgeModulePrefixes = []string{
	"github.com/fulminate-io/knowledge/",
	"github.com/fulminate-io/knowledge-contrib/",
}

// isKnowledgeImport reports whether an import path is one of ours, under either
// prefix above.
func isKnowledgeImport(path string) bool {
	for _, prefix := range knowledgeModulePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// allowedKnowledgeImports are the knowledge-tree paths this module may import.
// Each is ADMITTED rather than tolerated: a census written as a bare "no
// knowledge-tree import" assertion would be red on a correct module.
//
// TWO COMMON MODULES AND THIS MODULE ITSELF, and nothing else. The framework is
// the MCP serving and contract layer; the correlation module is the
// cross-service detector every logs collector needs, which lives in a module of
// its own precisely BECAUSE no collector may import a sibling. That rule is what
// the sibling arm below exists to hold: a path under cmd/collectors that is not
// one of these is another provider's, and importing it would be the coupling the
// common modules exist to prevent.
var allowedKnowledgeImports = []string{
	"github.com/fulminate-io/knowledge-contrib/common/correlation",
	"github.com/fulminate-io/knowledge-contrib/framework",
	"github.com/fulminate-io/knowledge-contrib/k8s-logs",
}

// TestNoSubprocessSpawn is the census.
func TestNoSubprocessSpawn(t *testing.T) {
	files := moduleSourceFiles(t)
	if len(files) < 5 {
		t.Fatalf("the census walked %d non-test source files; the module has more than that, so the walk "+
			"read the wrong tree and a clean result would mean nothing", len(files))
	}

	var findings []string
	for _, path := range files {
		findings = append(findings, spawnFindings(t, path)...)
	}
	if len(findings) != 0 {
		t.Fatalf("this collector spawns a process:\n  %s\n"+
			"It reads the Kubernetes API through client-go and never by shelling out",
			strings.Join(findings, "\n  "))
	}
	t.Logf("census: %d non-test source files walked, no process spawn", len(files))
}

// TestTheSpawnCensusIsNotInert is its KNOWN POSITIVE. A census that reads
// nothing is indistinguishable from a census that found nothing, so the same
// walker is run over a file that DOES spawn — including through an alias, which
// is the case a call-shape check cannot see.
func TestTheSpawnCensusIsNotInert(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a plain exec.Command", `package p
import "os/exec"
func run() { _ = exec.Command("kubectl", "logs", "-n", "dev", "api-1") }`},
		{"os/exec under an alias", `package p
import sh "os/exec"
func run() { _ = sh.Command("kubectl", "logs") }`},
		{"syscall.Exec under an alias", `package p
import sys "syscall"
func run() { _ = sys.Exec("/bin/kubectl", nil, nil) }`},
		{"os.StartProcess", `package p
import "os"
func run() { _, _ = os.StartProcess("/bin/kubectl", nil, nil) }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The name becomes a file name, so every separator in it is
			// flattened: a subtest called "os/exec under an alias" would
			// otherwise name a directory that does not exist.
			safe := strings.NewReplacer(" ", "_", "/", "_", ".", "_").Replace(tc.name)
			path := filepath.Join(dir, safe+".go")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := spawnFindings(t, path); len(got) == 0 {
				t.Fatalf("the census did not flag a spawning file; it would report this module clean "+
					"whatever it contained. Source:\n%s", tc.src)
			}
		})
	}

	// The NEAR MISS: `os` used for everything it is legitimately used for must
	// NOT be flagged, or the census flags every file and discriminates nothing.
	clean := filepath.Join(dir, "clean.go")
	src := `package p
import (
	"os"
	"os/signal"
	"syscall"
)
func run() {
	_ = os.Getenv("HOME")
	_, _ = os.Stat("/tmp")
	_ = signal.Ignore(syscall.SIGTERM)
}`
	if err := os.WriteFile(clean, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := spawnFindings(t, clean); len(got) != 0 {
		t.Fatalf("the census flagged ordinary use of os and syscall: %v", got)
	}
}

// TestOnlyTheCommonModulesAreImportedFromTheKnowledgeTree is the purity census.
func TestOnlyTheCommonModulesAreImportedFromTheKnowledgeTree(t *testing.T) {
	files := moduleSourceFiles(t)
	seen := map[string]struct{}{}
	var refused []string
	for _, path := range files {
		for _, imp := range importPaths(t, path) {
			if !isKnowledgeImport(imp) {
				continue
			}
			seen[imp] = struct{}{}
			if !allowedKnowledgeImport(imp) {
				refused = append(refused, fmt.Sprintf("%s imports %s", path, imp))
			}
		}
	}
	if len(refused) != 0 {
		sort.Strings(refused)
		t.Fatalf("this module imports the knowledge tree outside the framework and the common modules:\n  %s\n"+
			"The collector contract crossing is JSON over MCP, never a Go type",
			strings.Join(refused, "\n  "))
	}

	// The known positive: the framework IS imported, so a clean result means the
	// walk read imports rather than finding none.
	for _, expected := range []string{
		"github.com/fulminate-io/knowledge-contrib/framework",
		"github.com/fulminate-io/knowledge-contrib/common/correlation",
	} {
		if _, ok := seen[expected]; !ok {
			t.Fatalf("the census saw no %s import at all; this module serves MCP through the framework and "+
				"correlates through the common detector, so seeing neither means the walk read nothing", expected)
		}
	}
	t.Logf("census: knowledge-tree imports seen: %v", sortedSet(seen))
}

// TestThePurityCensusRefusesTheForbiddenTrees is that census's known positive,
// over synthetic files naming each forbidden path.
//
// THE SIBLING COLLECTORS ARE IN THE LIST and they are the reason the common
// modules exist. A collector reaching into another provider's module would
// couple two verticles that are meant to be independent, and it would do so
// through a path that looks superficially like the two admitted ones.
func TestThePurityCensusRefusesTheForbiddenTrees(t *testing.T) {
	for _, imp := range []string{
		"github.com/fulminate-io/knowledge/cmd/knowledge/internal/collector/logs",
		"github.com/fulminate-io/knowledge/cmd/knowledge-server/internal/store",
		"github.com/fulminate-io/knowledge/gen/knowledge/v1",
		"github.com/fulminate-io/knowledge-contrib/stackdriver",
		"github.com/fulminate-io/knowledge-contrib/loki",
		"github.com/fulminate-io/knowledge-contrib/cloudwatch",
		"github.com/fulminate-io/knowledge-contrib/k8s",
	} {
		if allowedKnowledgeImport(imp) {
			t.Errorf("%s is admitted by the purity census; the module may import only the framework and "+
				"cmd/collectors/common/*", imp)
		}
	}
	for _, imp := range allowedKnowledgeImports {
		if !allowedKnowledgeImport(imp) {
			t.Errorf("%s is refused by the purity census; it is on the allowlist", imp)
		}
	}
	// A prefix that merely LOOKS like the framework must not be admitted.
	if allowedKnowledgeImport("github.com/fulminate-io/knowledge-contrib/frameworkextra") {
		t.Error("a path that only shares a prefix with the framework is admitted; the match must be on a " +
			"whole path segment")
	}
}

// allowedKnowledgeImport reports whether a knowledge-tree import path is on the
// allowlist, matching whole path segments so a sibling sharing a prefix is not
// admitted by accident.
func allowedKnowledgeImport(imp string) bool {
	for _, allowed := range allowedKnowledgeImports {
		if imp == allowed || strings.HasPrefix(imp, allowed+"/") {
			return true
		}
	}
	return false
}

// spawnFindings returns one line per spawn site in a file.
func spawnFindings(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	// local name -> import path, so an aliased import resolves to what it names.
	byLocal := map[string]string{}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("%s: unquoting import %s: %v", path, imp.Path.Value, err)
		}
		local := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			local = imp.Name.Name
		}
		byLocal[local] = p
	}

	var findings []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		rule, watched := spawnPackages[byLocal[ident.Name]]
		if !watched {
			return true
		}
		if rule.wholePackage || containsString(rule.members, sel.Sel.Name) {
			findings = append(findings, fmt.Sprintf("%s:%d %s.%s (package %s)",
				path, fset.Position(sel.Pos()).Line, ident.Name, sel.Sel.Name, byLocal[ident.Name]))
		}
		return true
	})
	return findings
}

// importPaths returns one file's import paths.
func importPaths(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	out := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("%s: unquoting import %s: %v", path, imp.Path.Value, err)
		}
		out = append(out, p)
	}
	return out
}

// moduleSourceFiles lists this module's non-test .go files.
//
// THE WALK'S ROOT IS THE LITERAL ".", not a path a helper returned, and the
// paths it yields are relative to it. `go test` runs this package with the
// working directory already at the module root, so the literal is the correct
// root; deriving it from a resolver would make every path below a
// helper-derived one, which is the shape the repository's cache-blindness
// census asks new tests to avoid. assertModuleRoot is what checks the premise.
func moduleSourceFiles(t *testing.T) []string {
	t.Helper()
	assertModuleRoot(t)
	var out []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking this module's source: %v", err)
	}
	sort.Strings(out)
	return out
}

// assertModuleRoot checks the premise the literal walk root rests on: that the
// working directory really is this module's own. Both reads are from LITERAL
// relative paths, so nothing below them is derived from a resolver's answer.
//
// It is a guard rather than a resolver on purpose. A walk-up that FOUND the
// module root would be a resolver, and every path built from its answer would
// be a read whose subject the test cache cannot see.
func assertModuleRoot(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != moduleDirName {
		t.Fatalf("the census is running in %s rather than in this module's own directory; a census walking "+
			"another module's tree reports findings that are not this module's", dir)
	}
	if _, err := os.Stat("go.mod"); err != nil {
		t.Fatalf("this module's go.mod is not in the census's working directory %s: %v", dir, err)
	}
}

// moduleDirName is this module's directory name, which is what the census
// checks its working directory against.
const moduleDirName = "k8s-logs"

func containsString(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

func sortedSet(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
