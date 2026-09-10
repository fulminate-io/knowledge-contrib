// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

// serve_error_test.go — WHAT THE BINARY DOES WHEN SERVING FAILS.
//
// THE ARM IS SMALL AND NOTHING ELSE REACHES IT. main writes the error to STDERR
// and exits NON-ZERO; the stdio suite re-execs this test binary and calls the
// framework itself, so it never runs main, and the version pin exercises the
// path that returns before serving. So a main that swallowed the error, or wrote
// it to stdout, or exited zero, would pass every other test in this module.
//
// WHY EACH HALF MATTERS TO AN OPERATOR. The spawning daemon reads the child's
// exit status and its stderr. A collector that failed silently with status ZERO
// is recorded as a successful collect that found nothing — which is the same
// shape as an empty workspace, and it is the reading this module spends its
// completeness assertion to prevent everywhere else. A message on STDOUT is
// worse than none: stdout is the protocol stream, so it corrupts the framing
// instead of explaining anything.

// TestAServeFailureExitsNonZeroWithTheReasonOnStderr.
//
// THE PLANTED FAILURE IS A MALFORMED FRAME. The transport reads newline-
// delimited JSON-RPC from stdin; a line that is not JSON makes the framework's
// serve return an error, which is the one input that reaches this arm without a
// test hook in the shipped binary.
func TestAServeFailureExitsNonZeroWithTheReasonOnStderr(t *testing.T) {
	binary := buildCollector(t, "")

	ctx, cancel := context.WithTimeout(t.Context(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader("this is not a json-rpc frame\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if ctx.Err() != nil {
		t.Fatalf("the binary did not return within %s on a malformed frame", versionTimeout)
	}
	if err == nil {
		t.Fatal("a serve failure exited ZERO. The spawning daemon reads the status, so a " +
			"collector that failed silently is recorded as a successful collect that found " +
			"nothing — indistinguishable from an empty workspace")
	}
	if stderr.Len() == 0 {
		t.Fatal("a serve failure wrote nothing to stderr; the daemon reads it and an operator " +
			"is left with a non-zero status and no reason")
	}
	// THE MESSAGE NAMES THIS COLLECTOR, because an operator's daemon spawns
	// several and the status alone says which one only by luck.
	if !strings.Contains(stderr.String(), "bitbucket-pipelines collector") {
		t.Errorf("the failure does not name this collector: %q", stderr.String())
	}
	// AND IT IS NOT ON STDOUT, which is the protocol stream.
	if strings.Contains(stdout.String(), "bitbucket-pipelines collector") {
		t.Errorf("the failure was written to stdout, which is the protocol stream: %q",
			stdout.String())
	}
}

// TestACleanServeExitsZeroWithNothingOnStderr is the same-run known positive.
// Without it the row above is satisfied by a binary that fails on every input.
func TestACleanServeExitsZeroWithNothingOnStderr(t *testing.T) {
	binary := buildCollector(t, "")

	ctx, cancel := context.WithTimeout(t.Context(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	// STDIN CLOSED: the transport reaches EOF at once and the serve returns
	// cleanly, which is what an operator's daemon does when it stops the child.
	cmd.Stdin = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("a clean serve exited non-zero: %v (stderr: %s)", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("a clean serve wrote to stderr: %q", stderr.String())
	}
}
