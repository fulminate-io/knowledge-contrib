// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// version_test.go — THE VERSION BANNER THIS BINARY REPORTS, observed by building
// it and running it.
//
// WHY THE BINARY AND NOT THE SYMBOL. The stamp is injected at link time with a
// -X flag naming a symbol by its full import path, and a WRONG path is SILENT:
// the link succeeds and the default is left in place, so a build that stamped
// nothing is indistinguishable from a correct one until the binary runs. The
// framework's own comment says exactly that. A test that called a Go function
// would therefore be asserting about the one thing that cannot go wrong here.
//
// AND WHY THE OUTPUT AND NOT THE EXIT STATUS. Measured on both sides of the
// framework change that added this flag: before it, the binary read no argv at
// all, served stdio, saw EOF and returned — exit 0 with ZERO bytes on stdout.
// After it, exit 0 with the banner. The status is 0 either way, so a pin
// asserting on it would have passed against a binary that answered nothing.
//
// THE CHILD'S STDIN IS CLOSED AND THE WAIT IS BOUNDED, which is what makes the
// unstamped arm fail cleanly instead of hanging: a binary that ignores --version
// falls through to serving the protocol on stdin and waits forever for a frame.

// versionTimeout bounds the child. A binary answering --version writes one line
// and exits immediately; anything that takes longer is a binary that fell
// through to serving, which is the failure this bound turns into a message.
const versionTimeout = 30 * time.Second

// buildCollector builds this module's binary into a scratch directory and
// returns its path. The optional ldflags stamp the framework's version symbol.
func buildCollector(t *testing.T, ldflags string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "knowledge-collector-github-actions")
	args := []string{"build", "-o", binary}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, ".")

	build := exec.Command("go", args...)
	var stderr bytes.Buffer
	build.Stderr = &stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building this collector: %v\n%s", err, stderr.String())
	}
	return binary
}

// runVersion runs a built binary with --version in argv position 1, with stdin
// closed and the wait bounded, and returns what it wrote to stdout.
func runVersion(t *testing.T, binary string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), versionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--version")
	// CLOSED, not inherited. An inherited stdin from the test runner is a pipe
	// nobody writes to, so a binary that ignored the flag would block on it until
	// the bound fired; closed, it reaches EOF at once and the failure is the
	// empty output rather than the timeout.
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Logf("the binary exited with %v (stderr: %s)", err, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("the binary did not answer --version within %s; it fell through to serving the "+
			"protocol", versionTimeout)
	}
	return stdout.String()
}

// TestTheBinaryAnswersTheVersionFlagWithItsStamp is the pin.
//
// It asserts the PROPERTY the install script's up-to-date check reads: the first
// line is non-empty and its LAST FIELD is the stamp. The banner string is not
// written out as a literal here — that would pin this module to a wording the
// framework owns and would red on a harmless rewording, while saying nothing
// about the field the script actually compares.
func TestTheBinaryAnswersTheVersionFlagWithItsStamp(t *testing.T) {
	const stamp = "v0.0.0-version-pin"
	binary := buildCollector(t,
		"-X github.com/fulminate-io/knowledge-contrib/framework.version="+stamp)

	out := runVersion(t, binary)
	if out == "" {
		t.Fatal("the binary answered --version with no output at all. The install script's " +
			"up-to-date check reads the first line of this, so a silent binary makes every " +
			"installed copy look out of date and be reinstalled on every run")
	}
	first := strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)[0]
	if strings.TrimSpace(first) == "" {
		t.Fatalf("the first line of the banner is blank: %q", out)
	}
	fields := strings.Fields(first)
	if got := fields[len(fields)-1]; got != stamp {
		t.Errorf("the last field of the banner's first line is %q, and the -X flag stamped %q; "+
			"that field is what the install script compares against the release tag", got, stamp)
	}
	// The banner also names the tool this collector serves, which is what makes
	// it more than a bare stamp: an extraction heuristic reading the last field
	// of a ONE-token line would pass whatever it did.
	if len(fields) < 3 {
		t.Errorf("the banner's first line is %q, %d field(s); a one-token banner would let any "+
			"extraction pass", first, len(fields))
	}
	if !strings.Contains(first, "collect") {
		t.Errorf("the banner does not name the tool this collector serves: %q", first)
	}
}

// TestAnUnstampedBuildReportsTheFrameworksDefault is the same-run control for
// the arm above: the stamp really is coming from the -X flag and not from a
// constant that would read the same either way.
func TestAnUnstampedBuildReportsTheFrameworksDefault(t *testing.T) {
	out := runVersion(t, buildCollector(t, ""))
	if out == "" {
		t.Fatal("an unstamped binary answered --version with nothing")
	}
	fields := strings.Fields(strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)[0])
	if got := fields[len(fields)-1]; got == "v0.0.0-version-pin" {
		t.Errorf("an unstamped build reported the stamped test's own version %q, so the arm above "+
			"is not measuring the -X flag", got)
	}
}

// TestTheVersionFlagIsMatchedInFirstPositionOnly. A config entry may carry args
// of its own and the daemon passes them verbatim, so a looser match would eat
// one of them and serve nothing. The observable is that a binary handed the flag
// in SECOND position does not answer it: it falls through to serving, which the
// bounded wait then reports.
func TestTheVersionFlagIsMatchedInFirstPositionOnly(t *testing.T) {
	binary := buildCollector(t, "")

	ctx, cancel := context.WithTimeout(t.Context(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--serve", "--version")
	cmd.Stdin = nil
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	if strings.Contains(stdout.String(), "knowledge-collector") {
		t.Errorf("the flag was answered in second position: %q. A collector's own args are passed "+
			"verbatim by the daemon, so a looser match consumes one of them", stdout.String())
	}
}
