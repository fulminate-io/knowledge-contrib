// SPDX-License-Identifier: Apache-2.0

package main

import (
	"runtime"
	"slices"
	"testing"
)

// empty_sensitive_unmeasured_test.go — THE TWO ROWS THAT DO NOT REST ON THE
// SWEEP'S GROUND, split out of empty_sensitive_test.go so each half stays
// readable and neither is mistaken for the other.
//
// empty_sensitive_test.go asserts that no declared name tells present-and-empty
// apart from absent, driven through this collector's own resolvers with a
// same-run non-empty control per arm. That is one KIND of inertness: a reader on
// this path treats the two states alike. The rows here are the names for which
// that sentence is not the true one, and saying so is the point:
//
//   - KUBERNETES_MASTER is inert STRUCTURALLY. Nothing on this path reads it in
//     any state, so no control that moved the result exists, and the instrument
//     has to be a child process because the read happens at package
//     initialisation.
//   - SEVEN NAMES have no live control on the hosts this suite runs on. Their row
//     states what the source establishes and no more, and refuses a mark for them,
//     because a mark would be a claim no run here can carry.

// TestKubernetesMasterIsInertSTRUCTURALLY is the one declared name that cannot
// take the ground the sweep above rests on, and it gets its own row for that
// reason rather than being folded into a count.
//
// THE OTHER NAMES ARE INERT BECAUSE A READER ON THIS PATH TREATS EMPTY AS ABSENT.
// KUBERNETES_MASTER is inert because NOTHING ON THIS PATH READS IT IN ANY STATE.
// client-go reads it exactly once, at PACKAGE INITIALISATION, into the
// package-level clientcmd.ClusterDefaults variable, in a function the library
// marks deprecated; that value reaches a client only through a caller's own
// ConfigOverrides.ClusterDefaults, and this collector passes a fresh
// ConfigOverrides carrying nothing but CurrentContext (client.go). So there is no
// same-run control available for this name that could move the result, and a
// control that DID move it would mean the module had started reading it — which
// is itself the red this row exists to produce.
//
// THE INSTRUMENT IS A RE-EXEC, and it has to be. The read happens at package
// initialisation, so a t.Setenv inside the running process is too late by
// construction and would yield a vacuous zero: the variable would never be read
// in any arm and every state would agree for the wrong reason. The child below is
// this test binary re-executed with the variable in its environment, and it
// reports clientcmd.ClusterDefaults.Server as PROOF THAT ITS INIT SAW THE
// VARIABLE, beside the host this collector's own resolution produced.
func TestKubernetesMasterIsInertSTRUCTURALLY(t *testing.T) {
	// A NAME THAT LEFT THE ALLOWLIST IS A RED HERE, not a skip, and the neighbor
	// below already treats the identical situation that way. A skip is unreachable
	// today — KUBERNETES_MASTER is on the every-OS list and envAllowlist only
	// appends per-target extras — so it could fire only after a source change,
	// which is precisely when a silent skip is worst: this file would go on
	// describing a name the collector had dropped, and nothing would say so.
	if !slices.Contains(envAllowlist(runtime.GOOS), "KUBERNETES_MASTER") {
		t.Errorf("KUBERNETES_MASTER is no longer declared on %s; this file's statement about it is stale and must "+
			"be revised or removed rather than left describing a name the collector dropped", runtime.GOOS)
		return
	}
	// ONE home directory for every arm. The resolution quotes the kubeconfig path
	// it used, so a per-arm temp directory would make the three arms differ by
	// their own instrument rather than by the variable under test.
	//
	// AND IT CARRIES A RESOLVABLE KUBECONFIG, which is what makes the comparison
	// worth taking. The claim is about clientcmd.NewDefaultClientConfig: the
	// package-level ClusterDefaults this variable feeds can reach a client only
	// through a caller's own ConfigOverrides, and that site sits AFTER the
	// kubeconfig has been read. With no kubeconfig on disk all three arms return
	// at the read error, the site the claim is about is never reached, and the
	// three equal strings say nothing about it. With the fixture in place each arm
	// resolves a HOST, so the equality below is taken where the variable could
	// have acted.
	home := t.TempDir()
	writeMinimalKubeconfigUnderHome(t, home)
	results := make(map[string]kubernetesMasterProbeResult, 3)
	for _, a := range []struct {
		label string
		value string
		set   bool
	}{
		{"absent", "", false},
		{"empty", "", true},
		{"set", "https://master.example.invalid:6443", true},
	} {
		results[a.label] = runKubernetesMasterProbe(t, home, a.value, a.set)
	}

	// THE INSTRUMENT'S OWN CONTROL, and without it this row proves nothing: the
	// child's init must be SHOWN to have read the variable. clientcmd's package
	// default moves from its built-in localhost value to the set host, which is
	// only possible if getDefaultServer ran and saw it.
	if results["set"].ClusterDefault == results["absent"].ClusterDefault {
		t.Fatalf("the probe's own control failed: clientcmd.ClusterDefaults.Server is %q with the variable set and "+
			"%q with it absent, so the child's package initialisation did not read it and every arm below is vacuous",
			results["set"].ClusterDefault, results["absent"].ClusterDefault)
	}

	// EVERY ARM REACHED A BUILT CLIENT, checked before the arms are compared: an
	// arm that fell back to a refusal would make the comparison the weak one this
	// fixture exists to replace, and three equal refusals look exactly like three
	// equal resolutions until you read them.
	for _, label := range []string{"absent", "empty", "set"} {
		assertResolvesTheFixture(t, "the KUBERNETES_MASTER probe arm "+label, results[label].Outcome)
		t.Logf("arm %s: clientcmd default=%q  this collector resolved=%q",
			label, results[label].ClusterDefault, results[label].Outcome)
	}

	// AND YET THIS COLLECTOR'S RESOLUTION DOES NOT MOVE, in any of the three
	// states. That is the structural inertness: the library read the value and it
	// reaches nothing this collector builds.
	for _, label := range []string{"empty", "set"} {
		if results[label].Outcome != results["absent"].Outcome {
			t.Errorf("with KUBERNETES_MASTER %s, this collector resolved %q; absent resolves %q. "+
				"Nothing on this collector's path is supposed to read the name, so a difference here means it "+
				"started reading it and the name now needs a mark",
				label, results[label].Outcome, results["absent"].Outcome)
		}
	}
}

// TestTheNamesWithNoLiveControlStateOnlyWhatTheSourceEstablishes is the honesty
// row. Seven declared names have no live control available on the hosts this
// suite runs on, and each is inert for a DIFFERENT and narrower reason than the
// sweep's. The test states what is established per name and asserts the one thing
// it can: that each is still declared, so a reader of this file is looking at the
// current set rather than a frozen comment.
//
//   - DISABLE_HTTP2, HTTP2_READ_IDLE_TIMEOUT_SECONDS and HTTP2_PING_TIMEOUT_SECONDS
//     ARE read, on this collector's transport path, at apimachinery
//     pkg/util/net/http.go — each an os.Getenv guarded by `len(s) > 0`. THE GUARD
//     IS THE ANSWER: present-and-empty takes the same branch as absent by
//     construction. What is not established is a live http2 decision, because
//     observing one needs a real TLS connection to a real http2 server.
//   - SSL_CERT_FILE and SSL_CERT_DIR are read by crypto/x509 on linux and NOT on
//     darwin: root_unix.go carries a build constraint that includes linux and
//     excludes darwin, and darwin takes its roots from the Security framework. So
//     "not read" is true on darwin and FALSE on the CI runner, and their
//     empty-versus-absent behavior is established on NEITHER target.
//   - CLOUDSDK_CONFIG and PATH reach a spawned gcloud credential plugin that no
//     arm of this suite exercises. PATH is read on every OS by the exec lookup
//     that would spawn it, so "not read" is false for it too; the honest statement
//     is that no arm spawns the plugin.
//
// Measuring any of these is outside this ticket by its own scope line, so this
// row manufactures no control the research could not obtain and reports no
// unmeasured name as a measured zero.
func TestTheNamesWithNoLiveControlStateOnlyWhatTheSourceEstablishes(t *testing.T) {
	declared := envAllowlist(runtime.GOOS)
	for _, name := range []string{
		"DISABLE_HTTP2", "HTTP2_READ_IDLE_TIMEOUT_SECONDS", "HTTP2_PING_TIMEOUT_SECONDS",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
		"CLOUDSDK_CONFIG", "PATH",
	} {
		if name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR" {
			// These two are declared per target, exactly as the build constraint
			// that reads them is: they are on the unix list and off the darwin one.
			if runtime.GOOS == "darwin" {
				if slices.Contains(declared, name) {
					t.Errorf("%s is declared on darwin, where crypto/x509's readers are excluded by build constraint", name)
				}
				continue
			}
		}
		if !slices.Contains(declared, name) {
			t.Errorf("%s is no longer declared on %s; this file's statement about it is stale and must be revised "+
				"or removed rather than left describing a name the collector dropped", name, runtime.GOOS)
		}
		// AND NONE OF THEM IS MARKED. Their inertness is not established by
		// measurement, so a mark would be a claim this suite cannot support either;
		// unmarked is what the source reads above establish.
		for _, e := range describedEnvironment() {
			if e.Name == name && e.EmptySensitive {
				t.Errorf("%s is marked empty-sensitive, but no run on this host establishes that it discriminates; "+
					"the marks this collector carries must each rest on a measurement", name)
			}
		}
	}
}
