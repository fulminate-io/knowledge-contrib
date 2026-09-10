// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// standalone_test.go — THIS MODULE'S go.mod DESCRIBES THIS MODULE, whether or
// not a workspace is helping.
//
// THE DEFECT THIS EXISTS TO CATCH, and it shipped. A test file here imported a
// package go.mod did not require. Inside the workspace that resolves anyway,
// because another member's requirements satisfy it, so every build, vet and
// test passed and CI never saw anything — the collector legs all run in the
// workspace. Outside it the module refuses to load at all:
//
//	$ GOWORK=off go test ./...
//	go: updates to go.mod needed; to update it:
//	        go mod tidy
//
// WHY THAT MATTERS RATHER THAN BEING A DEVELOPER INCONVENIENCE. This module is
// published as a standalone collector: an operator building it has no
// workspace, and the module's whole dependency-purity story is a claim about
// what IT requires. A go.mod that is only complete in company is not a
// description of this module, and the first person to find out is whoever
// builds it alone.
//
// WHY A SUBPROCESS. `go mod tidy -diff` is the toolchain's own answer to "would
// tidying change anything", and it has no library form. The alternative is
// re-deriving the module graph in a test, which would be a second, worse
// implementation of the thing being checked.

// TestGoModIsCompleteStandalone asserts that tidying this module outside the
// workspace would change nothing.
func TestGoModIsCompleteStandalone(t *testing.T) {
	// THE PARENT OPENS BOTH FILES ITSELF, and that is not decoration. The go
	// tool records what THIS process opened when it keys the cached result, and
	// a child's reads are never the parent's — so without these two opens a
	// stored PASS would survive an edit to either file, which is the one input
	// this test is about.
	//
	// Both paths are LITERALS rather than a slice walked by a range variable: a
	// read whose name the workspace's cache census cannot resolve statically is
	// a read it must carry as unprovable residue, and there is nothing to prove
	// here — the two names are known.
	if _, err := os.ReadFile("go.mod"); err != nil {
		t.Fatalf("this module's go.mod could not be read: %v", err)
	}
	if _, err := os.ReadFile("go.sum"); err != nil {
		t.Fatalf("this module's go.sum could not be read: %v", err)
	}

	cmd := exec.Command("go", "mod", "tidy", "-diff")
	// GOWORK=off IS THE WHOLE POINT: with the workspace on, another member's
	// requirements stand in for this module's and the answer is always yes.
	// Everything else the toolchain needs is inherited, because a go command
	// needs its own environment to find its cache and its module cache.
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return
	}

	t.Fatalf("go.mod does not describe this module on its own: `GOWORK=off go mod tidy -diff` reports a "+
		"change.\n\nRun `GOWORK=off go mod tidy` in this directory and commit the result.\n\n"+
		"Inside the workspace this is invisible, because another member's requirements resolve the "+
		"import; outside it the module refuses to load at all, and an operator building this collector "+
		"standalone is the first to find out.\n\n%s", strings.TrimSpace(string(out)))
}
