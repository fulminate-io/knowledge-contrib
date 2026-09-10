// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// env_process_test.go — THE ENVIRONMENT INTERFACE, OBSERVED IN A REAL CHILD.
//
// WHAT IS UNDER TEST IS THE CHILD'S ENVIRONMENT, so the child has to be a real
// process: a collector installed through the config file gets exactly the
// environment its entry's env block spells out, and no function call in this
// test's own process can observe that. The block is set on the child's command
// here exactly as a daemon would set it from an entry.
//
// THE PROBE IS BOUNDED TO PER-NAME LOOKUPS. It asks the child about the names
// the test names and never serializes the child's whole environment, because a
// probe that dumped it would put whatever the developer's shell holds into the
// test output.

// askChild spawns the test binary in its reporting mode with exactly the
// environment given, and returns what it saw for each requested name.
func askChild(t *testing.T, block map[string]string, names []string) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	requested, err := json.Marshal(names)
	if err != nil {
		t.Fatalf("encoding the requested names: %v", err)
	}

	// THE CHILD'S ENVIRONMENT IS EXACTLY THE BLOCK plus the two variables that
	// tell the test binary which mode to run in. Nothing is inherited: that is
	// the property under test, and building the slice rather than appending to
	// os.Environ() is what makes it true here.
	env := []string{
		childModeEnv + "=" + childModeReportEnv,
		childNamesEnv + "=" + string(requested),
	}
	for name, value := range block {
		env = append(env, name+"="+value)
	}

	cmd := exec.CommandContext(ctx, testBinary(t))
	cmd.Env = env
	cmd.Stderr = os.Stderr
	// THE CHILD RUNS OUTSIDE THE WORKSPACE. It is not isolation for its own
	// sake: the workspace's cache-blindness census asks whether a spawned
	// child reads anything a cache key should have covered, and a child whose
	// working directory is a scratch tree cannot resolve a workspace-relative
	// path at all. A child that needed one would fail here rather than pass
	// quietly against bytes no key sees.
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the reporting child: %v", err)
	}

	seen := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("the child reported an unreadable line: %q", line)
		}
		seen[name] = value
	}
	return seen
}

// TestChildEnvironment_IsExactlyTheBlock — the four row shapes of the env
// block, in one run so each is the others' control.
func TestChildEnvironment_IsExactlyTheBlock(t *testing.T) {
	const planted = "AZURE_COLLECTOR_TEST_PLANTED"
	// A variable in THIS process's environment, which must not reach the
	// child. Setting it here is what makes the fourth row a measurement rather
	// than an assumption.
	t.Setenv(planted, "from-the-parent")

	names := []string{"AZURE_TENANT_ID", "AZURE_CLIENT_SECRET", "HOME", planted}
	seen := askChild(t, map[string]string{
		"AZURE_TENANT_ID": "a-tenant",
		// DECLARED AND EMPTY, which is a different thing from undeclared: an
		// operator who wrote the name and left it blank has said something.
		"AZURE_CLIENT_SECRET": "",
	}, names)

	// (1) A name in the block arrives with the block's value.
	if got := seen["AZURE_TENANT_ID"]; got != "present:a-tenant" {
		t.Errorf("a declared variable arrived as %q, expected its value", got)
	}
	// (2) A name declared EMPTY arrives present and empty, distinguishably.
	if got := seen["AZURE_CLIENT_SECRET"]; got != "present:" {
		t.Errorf("a variable declared empty arrived as %q, expected present and empty", got)
	}
	// (3) A name absent from the block is absent in the child, whatever the
	// parent holds.
	if got := seen["HOME"]; got != "absent" {
		t.Errorf("a variable the block omits arrived as %q; the block is the child's whole environment", got)
	}
	// (4) THE NEGATIVE CONTROL the other three rest on: a variable set in the
	// spawning process never arrives. Without this row, rows 1 and 2 would
	// pass just as well against a child that inherited everything.
	if got := seen[planted]; got != "absent" {
		t.Errorf("a variable planted in the spawning process arrived in the child as %q", got)
	}
}

// TestChildEnvironment_AnEmptyBlockIsAnEmptyEnvironment. The degenerate case,
// and the one an operator hits first: an entry with no env block at all yields
// a collector that can authenticate with nothing, rather than one that quietly
// inherits the daemon's credentials.
func TestChildEnvironment_AnEmptyBlockIsAnEmptyEnvironment(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "from-the-parent")
	seen := askChild(t, nil, []string{"AZURE_TENANT_ID", "AZURE_CLIENT_ID", "PATH", "HOME"})
	for name, got := range seen {
		if got != "absent" {
			t.Errorf("with an empty block, %s arrived as %q", name, got)
		}
	}
}

// TestChildEnvironment_EveryDeclaredNameArrives walks the whole declared list
// for this host's target and asserts each one arrives, which is what makes the
// list an interface rather than a document.
//
// THE VALUES ARE TEST SENTINELS, never real credentials: what is asserted is
// that the name crossed, and a test that needed a real secret to prove that
// would be a test nobody could run.
func TestChildEnvironment_EveryDeclaredNameArrives(t *testing.T) {
	vars, err := EnvironmentNames(runtime.GOOS)
	if err != nil {
		t.Skipf("this host's target OS has no environment table: %v", err)
	}

	block := map[string]string{}
	names := make([]string, 0, len(vars))
	for i, v := range vars {
		block[v.Name] = sentinelFor(i)
		names = append(names, v.Name)
	}
	seen := askChild(t, block, names)

	for i, v := range vars {
		want := "present:" + sentinelFor(i)
		if got := seen[v.Name]; got != want {
			t.Errorf("%s arrived as %q, expected %q", v.Name, got, want)
		}
	}
}

// TestChildEnvironment_ADeclaredNameOmittedFromTheBlockIsAbsent is the
// mutation on the row above: drop one name from the block and the child cannot
// see it, which is what the operator who omits it will experience.
func TestChildEnvironment_ADeclaredNameOmittedFromTheBlockIsAbsent(t *testing.T) {
	vars, err := EnvironmentNames(runtime.GOOS)
	if err != nil {
		t.Skipf("this host's target OS has no environment table: %v", err)
	}
	const dropped = "AZURE_TENANT_ID"

	block := map[string]string{}
	names := make([]string, 0, len(vars))
	for _, v := range vars {
		names = append(names, v.Name)
		if v.Name == dropped {
			continue
		}
		block[v.Name] = "set"
	}
	seen := askChild(t, block, names)

	if got := seen[dropped]; got != "absent" {
		t.Errorf("%s was dropped from the block and arrived as %q", dropped, got)
	}
	// The same-run known positive: its neighbors did arrive, so the absence
	// above is the omission rather than a broken probe.
	if got := seen["AZURE_CLIENT_ID"]; got != "present:set" {
		t.Errorf("a name that WAS in the block arrived as %q, so the absence above proves nothing", got)
	}
}

// TestChildEnvironment_TheWindowsNamesAreNotDeclaredOnThisHost, and the darwin
// trust-store row. Both are rows the table can only state per target, and both
// are asserted here through the table rather than through a branch that would
// never execute on this runner.
func TestChildEnvironment_TheWindowsNamesAreNotDeclaredOnThisHost(t *testing.T) {
	if runtime.GOOS == OSWindows {
		t.Skip("this host IS windows, where the pair is declared")
	}
	vars, err := EnvironmentNames(runtime.GOOS)
	if err != nil {
		t.Skipf("this host's target OS has no environment table: %v", err)
	}
	declared := map[string]bool{}
	for _, v := range vars {
		declared[v.Name] = true
	}
	for _, name := range []string{"SYSTEMROOT", "ProgramData"} {
		if declared[name] {
			t.Errorf("%s is declared on %s, where nothing reads it", name, runtime.GOOS)
		}
	}
	if runtime.GOOS == OSDarwin && declared["SSL_CERT_FILE"] {
		t.Error("SSL_CERT_FILE is declared on darwin, whose roots come from the system keychain rather than a bundle")
	}
}

func sentinelFor(i int) string {
	return "sentinel-" + strings.Repeat("x", i%3) + string(rune('a'+i%26))
}
