// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"
	"testing"
)

// env_test.go — the declared-environment table, asserted as a value computed
// from a target-OS PARAMETER.
//
// THAT SHAPE IS WHAT MAKES THE WINDOWS ARM TESTABLE AT ALL. Every runner that
// executes this suite is Linux or macOS, so a runtime.GOOS branch would leave the
// Windows arm unreachable on every machine that runs it — present in the diff,
// never executed, failing first on an operator's own machine.
//
// WHAT THIS FILE DOES NOT DO, STATED RATHER THAN IMPLIED. The plan asks for the
// declared set to be diffed against this module's DEPENDENCY CLOSURE in both
// directions: closure minus listed finds a name an un-configured entry would
// leave unset, and listed minus closure finds a name listed for no reader.
// NEITHER DIRECTION RUNS HERE. What runs is a diff against wantDeclaredUnixNames
// below, a literal external expectation, which catches a name silently added or
// dropped and catches nothing about whether anything reads it.
//
// WHY THE SUBSTITUTION. A closure census needs `go list -deps` over the module
// at its pinned versions and a constant-folding pass over every read site, which
// means a subprocess and a module-cache read from inside a unit test. This
// module's suite reaches no network, no credential and nothing outside its own
// module root, and the spawned-provider arm runs from a temp directory
// specifically to keep it that way; a census that shelled out would be the one
// thing here that does. The closure half belongs in a script at CI scope, run
// once per module rather than once per test binary, and it is NOT written.
//
// SO THE HONEST STATE IS: a name listed for no reader would not redden this
// suite. The listing decision's reasons are recorded per group in env.go, and
// the omissions carry their costs there; what is unverified is that each listed
// name still has a reader in the closure the module actually pins.

// TestTheTwoArmsDifferInExactlyOneName is the whole content of the OS split: the
// home directory the gcloud well-known credential file sits under.
func TestTheTwoArmsDifferInExactlyOneName(t *testing.T) {
	unix := declaredEnvNames(targetOSUnix)
	windows := declaredEnvNames(targetOSWindows)

	if !slices.Contains(unix, "HOME") || slices.Contains(unix, "APPDATA") {
		t.Errorf("the unix arm names HOME=%v APPDATA=%v",
			slices.Contains(unix, "HOME"), slices.Contains(unix, "APPDATA"))
	}
	if !slices.Contains(windows, "APPDATA") || slices.Contains(windows, "HOME") {
		t.Errorf("the windows arm names APPDATA=%v HOME=%v",
			slices.Contains(windows, "APPDATA"), slices.Contains(windows, "HOME"))
	}
	if len(unix) != len(windows) {
		t.Errorf("the arms are %d and %d names long, want the same length", len(unix), len(windows))
	}

	// EVERYTHING ELSE IS THE SAME on both arms, asserted as a set difference
	// rather than by comparing two hand-written lists.
	differs := symmetricDifference(unix, windows)
	slices.Sort(differs)
	if !slices.Equal(differs, []string{"APPDATA", "HOME"}) {
		t.Errorf("the arms differ in %v, want exactly HOME and APPDATA", differs)
	}
}

// wantDeclaredUnixNames is the declared set written out LITERALLY, and the
// literal is the point.
//
// A CENSUS THAT ITERATES THE SOURCE'S OWN GROUP VARIABLES PROVES NOTHING: delete
// a name from endpointNames and such a census deletes the assertion about it in
// the same edit, so it stays green while the collector quietly stops declaring
// the variable. This list is the external expectation that makes a dropped name
// a failure. Adding a name deliberately means editing it here too, which is the
// review moment the listing decision deserves.
var wantDeclaredUnixNames = []string{
	"EXPERIMENTAL_GOOGLE_API_USE_S2A",
	"GCE_METADATA_HOST",
	"GOOGLE_API_CERTIFICATE_CONFIG",
	"GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB",
	"GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB",
	"GOOGLE_API_USE_CLIENT_CERTIFICATE",
	"GOOGLE_API_USE_MTLS",
	"GOOGLE_API_USE_MTLS_ENDPOINT",
	"GOOGLE_APPLICATION_CREDENTIALS",
	"GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED",
	"GOOGLE_CLOUD_PROJECT",
	"GOOGLE_CLOUD_QUOTA_PROJECT",
	"GOOGLE_CLOUD_UNIVERSE_DOMAIN",
	"HOME",
	"HTTPS_PROXY",
	"HTTP_PROXY",
	"NO_PROXY",
	"S2A_TIMEOUT",
	"SSL_CERT_DIR",
	"SSL_CERT_FILE",
	"http_proxy",
	"https_proxy",
	"no_proxy",
}

// TestTheDeclaredSetIsExactlyTheNamesTheListingDecisionNamed compares the table
// against the literal above rather than against the source's own variables.
func TestTheDeclaredSetIsExactlyTheNamesTheListingDecisionNamed(t *testing.T) {
	got := declaredEnvNames(targetOSUnix)
	if !slices.Equal(got, wantDeclaredUnixNames) {
		missing := symmetricDifference(wantDeclaredUnixNames, got)
		slices.Sort(missing)
		t.Fatalf("the unix arm differs from the declared set in %v\n got: %v\nwant: %v",
			missing, got, wantDeclaredUnixNames)
	}

	// The windows arm is the same list with the one substitution.
	wantWindows := make([]string, 0, len(wantDeclaredUnixNames))
	for _, n := range wantDeclaredUnixNames {
		if n == "HOME" {
			n = "APPDATA"
		}
		wantWindows = append(wantWindows, n)
	}
	slices.Sort(wantWindows)
	if got := declaredEnvNames(targetOSWindows); !slices.Equal(got, wantWindows) {
		diff := symmetricDifference(wantWindows, got)
		slices.Sort(diff)
		t.Fatalf("the windows arm differs in %v", diff)
	}
}

// TestEveryDeclaredGroupIsPresent asserts that each group variable actually
// reaches the table, which is a different claim from the literal above: this one
// catches a group that was declared and then never appended.
func TestEveryDeclaredGroupIsPresent(t *testing.T) {
	unix := declaredEnvNames(targetOSUnix)
	for _, group := range [][]string{adcDiscoveryNames, proxyNames, trustRootNames, chainSelectorNames, endpointNames} {
		for _, name := range group {
			if !slices.Contains(unix, name) {
				t.Errorf("%s is missing from the declared set", name)
			}
		}
	}
	// The trust roots ride BOTH arms, inert on Windows rather than absent.
	windows := declaredEnvNames(targetOSWindows)
	for _, name := range trustRootNames {
		if !slices.Contains(windows, name) {
			t.Errorf("%s is missing from the windows arm", name)
		}
	}
}

// TestNoExcludedNameDriftsIntoTheDeclaredSet is the other direction of the diff.
// S2A_ACCESS_TOKEN is the sharpest: it is credential material rather than a
// switch, and its two companion SWITCHES are declared in the same run, so the
// exclusion reads as a line drawn at credentials rather than at S2A.
func TestNoExcludedNameDriftsIntoTheDeclaredSet(t *testing.T) {
	for _, os := range []targetOS{targetOSUnix, targetOSWindows} {
		declared := declaredEnvNames(os)
		for _, name := range excludedNames {
			if slices.Contains(declared, name) {
				t.Errorf("the excluded %s is declared on the %s arm", name, os)
			}
		}
	}
	// SAME-RUN CONTROL: the two S2A switches ARE declared, so an operator can
	// still see and set the mode even though the token stays out.
	unix := declaredEnvNames(targetOSUnix)
	for _, name := range []string{"EXPERIMENTAL_GOOGLE_API_USE_S2A", "S2A_TIMEOUT"} {
		if !slices.Contains(unix, name) {
			t.Errorf("the S2A switch %s is not declared, so the token's exclusion is not a line at credentials", name)
		}
	}
}

// TestTheDeclaredSetIsSortedAndFreeOfDuplicates keeps the table renderable into
// a config entry without a second pass.
func TestTheDeclaredSetIsSortedAndFreeOfDuplicates(t *testing.T) {
	for _, os := range []targetOS{targetOSUnix, targetOSWindows} {
		names := declaredEnvNames(os)
		if !slices.IsSorted(names) {
			t.Errorf("the %s arm is not sorted: %v", os, names)
		}
		seen := make(map[string]bool, len(names))
		for _, n := range names {
			if seen[n] {
				t.Errorf("the %s arm names %s twice", os, n)
			}
			seen[n] = true
		}
	}
}

// TestFourNamesThatLookLikeCredentialDiscoveryAreNotDeclared is the negative
// canary set: the names an author adds by guesswork because they look like ADC
// and are read by nothing on this collector's path. Their absence is asserted
// beside the two that ARE read, so the zero has a control.
func TestFourNamesThatLookLikeCredentialDiscoveryAreNotDeclared(t *testing.T) {
	unix := declaredEnvNames(targetOSUnix)
	for _, canary := range []string{"CLOUDSDK_CONFIG", "GCLOUD_PROJECT", "CLOUDSDK_CORE_PROJECT", "GCE_METADATA_IP"} {
		if slices.Contains(unix, canary) {
			t.Errorf("the canary %s is declared; it is read by nothing on this collector's path", canary)
		}
	}
	for _, real := range []string{"GOOGLE_CLOUD_PROJECT", "GOOGLE_APPLICATION_CREDENTIALS"} {
		if !slices.Contains(unix, real) {
			t.Fatalf("%s is not declared, so the canary assertions above have no control", real)
		}
	}
}

// TestNormalizeTargetOSMapsEveryNonWindowsGOOSToTheUnixArm covers the mapping
// from a Go GOOS value, including the two this suite actually runs on.
func TestNormalizeTargetOSMapsEveryNonWindowsGOOSToTheUnixArm(t *testing.T) {
	if got := normalizeTargetOS("windows"); got != targetOSWindows {
		t.Errorf("normalizeTargetOS(windows) = %s", got)
	}
	for _, goos := range []string{"linux", "darwin", "freebsd", "openbsd", "solaris", ""} {
		if got := normalizeTargetOS(goos); got != targetOSUnix {
			t.Errorf("normalizeTargetOS(%q) = %s, want the unix arm", goos, got)
		}
	}
}

// symmetricDifference returns the names present in exactly one of two sets.
func symmetricDifference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, n := range b {
		inB[n] = true
	}
	inA := make(map[string]bool, len(a))
	for _, n := range a {
		inA[n] = true
	}
	var out []string
	for _, n := range a {
		if !inB[n] {
			out = append(out, n)
		}
	}
	for _, n := range b {
		if !inA[n] {
			out = append(out, n)
		}
	}
	return out
}
