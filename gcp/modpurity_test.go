// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

// modpurity_test.go — THE MODULE'S DEPENDENCY PURITY, asserted against its own
// go.mod.
//
// WHY THIS IS A TEST AND NOT A CORPUS CHECK. The subject is go.mod, which is not
// Go source, so a pattern matched against a parsed syntax tree cannot reach it.
// A shape check also carries one pattern and one filter and reads no file path,
// while the assertion here is per-require with a REASON per admitted entry —
// which is a test's shape rather than a check's.
//
// WHY IT IS A TEST AND NOT A COMPILER GUARANTEE. Half the rule enforces itself:
// the toolchain refuses another module's `internal` outright, so no file here
// can import the client's or the server's internals however hard it tries. What
// does NOT enforce itself is a `require` — a module can depend on the client
// module and pull its whole surface in without importing anything private. That
// is the half this asserts.
//
// WHY THE RULE EXISTS. This collector speaks the collector contract over MCP, a
// JSON crossing, and it does not speak the knowledge wire. A dependency on the
// client would put this collector back inside the binary the project exists to
// take it out of, and it would do so silently: the build would still work.

// ourModulePrefix is the module path every module in this repository shares.
const ourModulePrefix = "github.com/fulminate-io/knowledge"

// theOnlyAdmittedRequire is the ONE module of ours this collector may require,
// with the reason. It is the common MCP serving and contract layer; a collector
// that carried its own would have re-derived what that module settled.
//
// WRITTEN AS ONE WHOLE LITERAL, not as ourModulePrefix + the rest, and that is
// load-bearing rather than a style choice. scripts/sync-to-contrib.sh publishes
// these modules to knowledge-contrib by rewriting the module path
// github.com/fulminate-io/knowledge-contrib across *.go, go.mod and
// go.sum; a path split across a concatenation is invisible to that rewrite, so
// the published module would require the contrib framework while this constant
// still named the upstream one, and both tests below would fail in the published
// repository with the module perfectly correct. The refusal set further down
// keeps the upstream spelling deliberately: cmd/knowledge and
// cmd/knowledge-server are not published here and must stay refused by their
// real names.
const theOnlyAdmittedRequire = "github.com/fulminate-io/knowledge-contrib/framework"

func loadModFile(t *testing.T) *modfile.File {
	t.Helper()
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("reading this module's go.mod: %v", err)
	}
	parsed, err := modfile.Parse("go.mod", raw, nil)
	if err != nil {
		t.Fatalf("parsing this module's go.mod: %v", err)
	}
	return parsed
}

// TestTheFrameworkIsTheOnlyRequireOfOurs is the assertion the whole file exists
// for.
func TestTheFrameworkIsTheOnlyRequireOfOurs(t *testing.T) {
	parsed := loadModFile(t)

	var ours []string
	for _, req := range parsed.Require {
		if strings.HasPrefix(req.Mod.Path, ourModulePrefix) {
			ours = append(ours, req.Mod.Path)
		}
	}

	// THE KNOWN POSITIVE. Without it, a go.mod this test failed to parse, or one
	// whose requires it read as empty, would pass silently.
	if len(ours) == 0 {
		t.Fatalf("this module requires nothing under %q, not even the collector framework; "+
			"the assertion below would then check nothing", ourModulePrefix)
	}

	for _, path := range ours {
		if path == theOnlyAdmittedRequire {
			continue
		}
		t.Errorf("this module requires %q. The ONLY module of ours a collector may require is "+
			"the collector framework (%q): a collector speaks the contract over MCP and does not "+
			"speak the knowledge wire, so requiring the client or the server would put this "+
			"collector back inside the binary it exists to be outside of.",
			path, theOnlyAdmittedRequire)
	}
}

// TestTheFrameworkRequireCarriesItsReplace pins the pair. A require without the
// replace fails even the workspace build; a workspace-resolved import with
// NEITHER builds locally and then cannot be tidied and writes no go.sum, which
// the cache-key script names by explicit path and hard-errors on. Both halves
// are needed, so both are asserted.
func TestTheFrameworkRequireCarriesItsReplace(t *testing.T) {
	parsed := loadModFile(t)

	var required bool
	for _, req := range parsed.Require {
		if req.Mod.Path == theOnlyAdmittedRequire {
			required = true
		}
	}
	if !required {
		t.Errorf("this module does not require %q", theOnlyAdmittedRequire)
	}

	var replaced bool
	for _, rep := range parsed.Replace {
		if rep.Old.Path != theOnlyAdmittedRequire {
			continue
		}
		replaced = true
		if !strings.HasPrefix(rep.New.Path, "../") {
			t.Errorf("the framework replace points at %q; it must be the sibling directory in "+
				"this repository, not a published version", rep.New.Path)
		}
	}
	if !replaced {
		t.Errorf("this module requires %q with no replace directive. A bare require of an "+
			"unpublished module cannot resolve.", theOnlyAdmittedRequire)
	}
}

// TestNoRequireNamesTheClientOrTheServer states the forbidden set explicitly
// rather than only by exclusion, so a reader of a failure sees the actual rule
// and a future author who widens the admitted set has to delete a named
// assertion rather than edit one string.
func TestNoRequireNamesTheClientOrTheServer(t *testing.T) {
	parsed := loadModFile(t)
	forbidden := map[string]string{
		ourModulePrefix: "the root contract module: a collector speaks MCP, not the knowledge wire",
		ourModulePrefix + "/cmd/knowledge": "the client, whose collectors this module exists to " +
			"replace",
		ourModulePrefix + "/cmd/knowledge-server": "the server, which a collector never talks to",
	}
	for _, req := range parsed.Require {
		if reason, bad := forbidden[req.Mod.Path]; bad {
			t.Errorf("this module requires %q, which is %s", req.Mod.Path, reason)
		}
	}
}
