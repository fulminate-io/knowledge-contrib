// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

// kubernetes_master_probe_test.go — THE RE-EXEC INSTRUMENT for a variable read
// at PACKAGE INITIALISATION.
//
// WHY IT EXISTS. client-go reads KUBERNETES_MASTER once, in the initialiser of
// the package-level clientcmd.ClusterDefaults, before any test body runs. An
// in-process t.Setenv is therefore too late by construction: the variable would
// be unread in every arm and the three arms would agree for a reason that has
// nothing to do with the claim. A test written that way reports a confident zero
// and observes nothing, which is exactly the measurement artifact this file
// avoids.
//
// SO THE ARM IS A CHILD PROCESS, this same test binary re-executed with the
// variable in its environment, and it reports TWO values: the package default
// client-go computed from the variable, which is the instrument's own proof that
// the child's initialisation saw it, and the host this collector's own
// resolution produced, which is the thing under test.

// kubernetesMasterProbeEnv marks a run as the child arm. Its presence is what
// makes the probe test body do its work instead of skipping.
const kubernetesMasterProbeEnv = "KN_T41_KUBERNETES_MASTER_PROBE"

// kubernetesMasterProbeMarker prefixes the child's one line of output, so the
// parent reads a value rather than parsing the test runner's own chatter.
const kubernetesMasterProbeMarker = "KN_T41_PROBE_RESULT "

// kubernetesMasterProbeResult is what one child arm reports.
type kubernetesMasterProbeResult struct {
	// ClusterDefault is clientcmd.ClusterDefaults.Server as the child's package
	// initialisation computed it. It is the CONTROL: it moves when, and only
	// when, the child's init read the variable.
	ClusterDefault string `json:"cluster_default"`
	// Outcome is this collector's own resolution, reduced to a comparable value.
	Outcome string `json:"outcome"`
}

// TestKubernetesMasterProbeChild is the CHILD ARM. It skips in an ordinary run
// and does its work only when the parent re-executes this binary, so it costs a
// normal `go test` nothing and cannot be mistaken for a row of its own.
func TestKubernetesMasterProbeChild(t *testing.T) {
	if os.Getenv(kubernetesMasterProbeEnv) == "" {
		t.Skip("the child arm of the KUBERNETES_MASTER probe; the parent test re-executes this binary to run it")
	}
	out := kubernetesMasterProbeResult{
		ClusterDefault: clientcmd.ClusterDefaults.Server,
		Outcome:        resolutionOutcome(t),
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("encoding the probe result: %v", err)
	}
	os.Stdout.WriteString(kubernetesMasterProbeMarker + string(encoded) + "\n")
}

// runKubernetesMasterProbe re-executes this test binary with KUBERNETES_MASTER in
// the state named and returns what the child reported.
//
// THE CHILD'S ENVIRONMENT IS BUILT, NOT INHERITED. Every name that could move the
// resolution — the in-cluster pair, the kubeconfig path, the home directory — is
// pinned to the same value in all three arms, so the ONLY thing that differs
// between them is the variable under test. Inheriting the parent's environment
// would make a developer's own KUBECONFIG the difference between two arms.
//
// home IS THE CALLER'S AND IS THE SAME DIRECTORY FOR EVERY ARM. Taking a fresh
// t.TempDir() per arm was the first shape and it was WRONG: the resolution's
// refusal quotes the kubeconfig path it tried, so three arms with three homes
// produced three different strings and the comparison reported a difference the
// variable under test had nothing to do with. The instrument's own varying input
// is the classic way an inertness row manufactures a false positive.
func runKubernetesMasterProbe(t *testing.T, home, value string, set bool) kubernetesMasterProbeResult {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	env := []string{
		kubernetesMasterProbeEnv + "=1",
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
	}
	if set {
		env = append(env, "KUBERNETES_MASTER="+value)
	}
	cmd := exec.Command(self, "-test.run=^TestKubernetesMasterProbeChild$", "-test.v")
	cmd.Env = env
	// THE CHILD RUNS OUTSIDE THE WORKSPACE, and that is a measurement rather than
	// tidiness. A child's file opens are invisible to the go tool's cache key for
	// this package, so a child that read a workspace file would make this package
	// serve a cached pass after that file changed. Running it with its working
	// directory in the same temp directory its HOME points at makes any
	// workspace-relative read FAIL, so a passing probe is the control that says it
	// reads none: its inputs are the compiled-in package default, the temp home,
	// and the absolute service-account paths.
	cmd.Dir = home
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("the probe child exited %v (stderr: %s)\n%s", err, stderr.String(), stdout)
	}
	for line := range strings.SplitSeq(string(stdout), "\n") {
		if !strings.HasPrefix(line, kubernetesMasterProbeMarker) {
			continue
		}
		var res kubernetesMasterProbeResult
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, kubernetesMasterProbeMarker)), &res); err != nil {
			t.Fatalf("decoding the probe result %q: %v", line, err)
		}
		return res
	}
	t.Fatalf("the probe child reported no result line; its output was:\n%s", stdout)
	return kubernetesMasterProbeResult{}
}
