// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"bytes"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version_test.go — the argv switch, exercised on a REAL CHILD PROCESS.
//
// Every case here re-execs this test binary in fixture mode, the way dialStdio
// does, because the thing under test is what a collector binary does with the
// argv a daemon or an operator hands it. An in-process call to printVersion
// would assert the formatter and nothing about the entry point.
//
// THE BANNER IS MULTI-TOKEN ON PURPOSE and the assertions below read its LAST
// FIELD. The install script extracts the version the same way (first line, last
// field), so a banner of one bare token would let any extraction pass and hide
// a broken comparison — the reason scripts/install_test.sh makes its client stub
// emit the production shape rather than a bare tag.

// runFixtureBinary re-execs this test binary in fixture mode with args and
// returns its stdout, its stderr and its exit error. Stdin is closed, so a child
// that reaches the stdio server reads EOF and exits instead of blocking.
func runFixtureBinary(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), self, args...)
	cmd.Env = append(os.Environ(), fixtureModeEnv+"="+string(fixtureConforming), fixtureToolEnv+"=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return stdout.String(), stderr.String(), runErr
}

// TestVersionFlagPrintsStampedToken is the flag's own row: an exact first
// --version prints a non-empty token and exits 0, on a binary that parsed no
// argv at all before this arm existed.
func TestVersionFlagPrintsStampedToken(t *testing.T) {
	stdout, stderr, err := runFixtureBinary(t, "--version")
	if err != nil {
		t.Fatalf("--version should exit 0, got %v (stderr: %s)", err, stderr)
	}
	first := strings.SplitN(strings.TrimSpace(stdout), "\n", 2)[0]
	fields := strings.Fields(first)
	if len(fields) < 2 {
		t.Fatalf("the banner must carry more than a bare token so an extraction cannot pass vacuously; got %q", stdout)
	}
	token := fields[len(fields)-1]
	if token == "" {
		t.Fatalf("--version printed no version token: %q", stdout)
	}
	// The default is what an unstamped build reports (the row for a stamped
	// build runs on the shell arc, which can pass -ldflags).
	if token != "dev" {
		t.Fatalf("an unstamped build must report the dev default, got %q", token)
	}
}

// TestVersionArmDoesNotEatEntryArgs guards the property the config contract
// depends on: a collector entry may carry args, the daemon passes them through,
// and a collector that short-circuits on any of them serves nothing.
func TestVersionArmDoesNotEatEntryArgs(t *testing.T) {
	for _, args := range [][]string{
		{"--region", "us-east-1"},
		// --version in a NON-FIRST position is an ordinary argument.
		{"--region", "--version"},
	} {
		self, err := os.Executable()
		if err != nil {
			t.Fatalf("resolving the test binary: %v", err)
		}
		cmd := exec.Command(self, args...)
		cmd.Env = append(os.Environ(), fixtureModeEnv+"="+string(fixtureConforming), fixtureToolEnv+"=")
		cmd.Stderr = os.Stderr

		client := mcp.NewClient(&mcp.Implementation{Name: "framework-suite", Version: "v1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatalf("a collector spawned with args %v must still serve: %v", args, err)
		}
		tools, err := session.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatalf("listing tools over a collector spawned with args %v: %v", args, err)
		}
		// BY NAME, NOT BY COUNT. What this row asserts is that the version arm did
		// not eat the entry's args and the collector went on to serve; the collect
		// tool being present proves that exactly as a count did, and it keeps
		// proving it when the contract gains a tool.
		served := make([]string, 0, len(tools.Tools))
		for _, tool := range tools.Tools {
			served = append(served, tool.Name)
		}
		if !slices.Contains(served, DefaultToolName) {
			t.Fatalf("args %v: the collector serves %v, which does not include the collect tool %q",
				args, served, DefaultToolName)
		}
		if err := session.Close(); err != nil {
			t.Fatalf("closing the session: %v", err)
		}
	}
}

// TestNormalRunWritesNothingToStdout is the protocol-stream guard: stdout is the
// JSON-RPC framing, so the version arm must return BEFORE anything serves and a
// run with no argv switch must write no banner at all.
func TestNormalRunWritesNothingToStdout(t *testing.T) {
	stdout, stderr, err := runFixtureBinary(t)
	if err != nil {
		t.Fatalf("a fixture run with closed stdin should exit 0, got %v (stderr: %s)", err, stderr)
	}
	if stdout != "" {
		t.Fatalf("a normal run wrote %q to stdout; stdout is the protocol stream", stdout)
	}
}
