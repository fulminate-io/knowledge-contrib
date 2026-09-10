// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"
	"testing"
)

// env_test.go — the declared environment as a value, and the four things the
// spawn contract claims about it, observed through a REAL spawned child.

// TestDeclaredEnvIsTheTargetOSTable pins the set per target operating system.
//
// IT IS A TABLE AND NOT A runtime.GOOS BRANCH, which is what makes the Windows
// arm observable at all: no runner in this repository runs Go tests on Windows,
// so a runtime branch would be dead code on every machine that could exercise
// it while a function taking the system as DATA is exercised everywhere.
func TestDeclaredEnvIsTheTargetOSTable(t *testing.T) {
	posix := DeclaredEnv(TargetPOSIX)
	windows := DeclaredEnv(TargetWindows)

	if !slices.IsSorted(posix) || !slices.IsSorted(windows) {
		t.Error("the declared list is not sorted; an entry generated from it would reorder between runs")
	}
	if len(posix) != len(windows) {
		t.Errorf("the two targets declare %d and %d names; they differ by the home variable alone",
			len(posix), len(windows))
	}

	// THE COUNT IS PINNED, and it is what makes dropping ANY single name a red
	// rather than a silent narrowing. Twenty-four names the credential chain
	// reads, twenty that tune how requests are made, five per-service endpoint
	// overrides and one home variable. A name removed from any of the four
	// groups moves this number; a name added moves it too, which is the point —
	// a dependency bump that starts reading a new name is a decision, not a
	// drift.
	const declaredNameCount = 24 + 20 + 5 + 1
	if len(posix) != declaredNameCount {
		t.Errorf("the declared list carries %d names, want %d; a name added or dropped is a decision to "+
			"record, and the group counts behind this number are in env.go", len(posix), declaredNameCount)
	}
	if len(credentialChainEnv) != 24 || len(behaviorTuningEnv) != 20 || len(perServiceEndpointEnv) != 5 {
		t.Errorf("the groups hold %d credential-chain, %d tuning and %d endpoint names, want 24, 20 and 5",
			len(credentialChainEnv), len(behaviorTuningEnv), len(perServiceEndpointEnv))
	}

	// THE HOME VARIABLE IS LOAD-BEARING AND IS NOT AN AWS NAME. Without it the
	// shared credentials and config files are unreachable, so no profile, no
	// single-sign-on session and no assumed role resolves, however many AWS
	// names are declared.
	if !slices.Contains(posix, "HOME") {
		t.Error("the POSIX list does not declare HOME")
	}
	if slices.Contains(posix, "USERPROFILE") {
		t.Error("the POSIX list declares USERPROFILE, which is the Windows home variable")
	}
	if !slices.Contains(windows, "USERPROFILE") {
		t.Error("the Windows list does not declare USERPROFILE, which stands in for HOME there")
	}
	if slices.Contains(windows, "HOME") {
		t.Error("the Windows list declares HOME rather than USERPROFILE")
	}

	// The four per-service endpoint overrides belong to modules the CREDENTIAL
	// CHAIN builds clients for, not to the logs API. They exist as no literal
	// in the configuration package — it composes the name at run time — so a
	// search for quoted names could not have produced them.
	for _, name := range []string{
		"AWS_ENDPOINT_URL_CLOUDWATCH_LOGS",
		"AWS_ENDPOINT_URL_STS",
		"AWS_ENDPOINT_URL_SSO",
		"AWS_ENDPOINT_URL_SSO_OIDC",
		"AWS_ENDPOINT_URL_SIGNIN",
	} {
		if !slices.Contains(posix, name) {
			t.Errorf("the declared list does not carry %s", name)
		}
	}
}

// TestDeclaredEnvExcludesThePlatformNamesDeliberately states the exclusion as
// an assertion rather than leaving it as an absence a reader has to notice.
//
// The cost is real and silent: on a host whose certificate bundle is not at a
// compiled-in default path, or behind a proxy, an operator who has not listed
// these cannot reach the endpoint at all — and the failure is a TRANSPORT
// error, which the credential arm does not cover. That is why the transport
// arm below is owed.
func TestDeclaredEnvExcludesThePlatformNamesDeliberately(t *testing.T) {
	declared := DeclaredEnv(TargetPOSIX)
	for _, name := range []string{
		"SSL_CERT_FILE", "SSL_CERT_DIR",
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy",
	} {
		if slices.Contains(declared, name) {
			t.Errorf("%s is declared; the eight platform names are excluded with their cost stated, "+
				"and adding one is a configuration-entry edit rather than a code change", name)
		}
	}
	// PATH is excluded for the same reason and with the same stated limit: a
	// shared-config profile using a credential process with a bare command name
	// fails loudly at the chain, and an absolute path still works.
	if slices.Contains(declared, "PATH") {
		t.Error("PATH is declared; the credential-process limit is a stated one, not an oversight")
	}
}

// TestTheChildsEnvironmentIsExactlyItsBlock covers the four shapes the spawn
// contract claims, through a real child process.
func TestTheChildsEnvironmentIsExactlyItsBlock(t *testing.T) {
	// A NAME PLANTED IN THIS PROCESS and NOT in the block. It is the negative
	// control the other three rest on: the child's environment is the block,
	// never an inheritance from whoever spawned it.
	const plantedName = "KN_CLOUDWATCH_PLANTED_IN_THE_PARENT"
	t.Setenv(plantedName, "parent-value")

	block := map[string]string{
		"AWS_REGION":        "us-east-1",
		"AWS_ACCESS_KEY_ID": "",
	}
	probe := dialStdioProviderWithEnv(t, modeEnvProbe, block)
	got := probe.probeEnv(t, "AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_PROFILE", plantedName)

	// (a) A NAME IN THE BLOCK ARRIVES WITH THE BLOCK'S VALUE.
	if !got["AWS_REGION"].Present || got["AWS_REGION"].Value != "us-east-1" {
		t.Errorf("AWS_REGION in the child = %+v, want present with the block's value", got["AWS_REGION"])
	}

	// (b) A NAME ABSENT FROM THE BLOCK IS ABSENT IN THE CHILD, and the child
	// can tell absent from empty: AWS_ACCESS_KEY_ID is in the block with an
	// EMPTY value and is present, while AWS_PROFILE is in no block and is not.
	if got["AWS_PROFILE"].Present {
		t.Errorf("AWS_PROFILE reached the child though the block does not carry it")
	}
	if !got["AWS_ACCESS_KEY_ID"].Present {
		t.Error("a name in the block with an EMPTY value read as absent; absent and empty are different states")
	}
	if got["AWS_ACCESS_KEY_ID"].Value != "" {
		t.Errorf("AWS_ACCESS_KEY_ID = %q, want the block's empty value", got["AWS_ACCESS_KEY_ID"].Value)
	}

	// (d) A VARIABLE PLANTED IN THE SPAWNING PROCESS NEVER ARRIVES.
	if got[plantedName].Present {
		t.Errorf("%s reached the child from the spawning process's own environment; the block is the "+
			"child's WHOLE environment", plantedName)
	}
}

// TestAnEmptyBlockIsAnEmptyChildEnvironment is shape (c), separated because it
// is the state an operator who declared nothing is in — and the state the
// credential arm's refusal is produced from.
func TestAnEmptyBlockIsAnEmptyChildEnvironment(t *testing.T) {
	probe := dialStdioProviderWithEnv(t, modeEnvProbe, nil)
	got := probe.probeEnv(t, "AWS_REGION", "HOME", "PATH", "AWS_ACCESS_KEY_ID")
	for name, obs := range got {
		if obs.Present {
			t.Errorf("%s is present in a child spawned with an empty block", name)
		}
	}
	// THE KNOWN POSITIVE in the same run: the probe really does observe names
	// when they are there, so the zeros above are absence rather than a broken
	// probe.
	control := dialStdioProviderWithEnv(t, modeEnvProbe, map[string]string{"AWS_REGION": "eu-west-1"})
	if obs := control.probeEnv(t, "AWS_REGION")["AWS_REGION"]; !obs.Present || obs.Value != "eu-west-1" {
		t.Errorf("the control observed %+v; the probe cannot see a name that IS present", obs)
	}
}
