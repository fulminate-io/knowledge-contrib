// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"os"
	"strings"
	"testing"
)

// env_test.go — the environment interface, asserted against the closure census
// rather than against a transcription of itself.
//
// A TEST COMPARING THE LIST TO A COPY OF THE LIST WOULD PASS FOREVER. What
// makes these rows mean something is that the expectations below are the
// CENSUS: the names the Azure credential chain, MSAL, crypto/x509 and net/http
// read on the code paths this collector takes, each with the reader that reads
// it. The two directions are checked separately, because a one-way check
// catches only half the ways the list can be wrong.

// censusDeclared is what the closure census says this collector's entry must
// carry on a non-Windows target, keyed by the reader that reads it.
//
// Provenance, per name: the thirteen AZURE_* names and AZURE_SUBSCRIPTION_ID
// are azidentity's own and this collector's own; the eight identity names are
// MSAL's, which azidentity delegates managed identity to; AZURE_SDK_GO_LOGGING
// is the SDK logging layer's; MSAL_FORCE_REGION and REGION_NAME are MSAL's
// region detection; the six proxy names are x/net/http/httpproxy's, reached
// through net/http's ProxyFromEnvironment, which the Azure transport installs
// by default; PATH is what the chain's three developer-tool arms need to find
// the tool they shell out to.
var censusDeclared = []string{
	"AZURE_ADDITIONALLY_ALLOWED_TENANTS",
	"AZURE_AUTHORITY_HOST",
	"AZURE_CLIENT_CERTIFICATE_PASSWORD",
	"AZURE_CLIENT_CERTIFICATE_PATH",
	"AZURE_CLIENT_ID",
	"AZURE_CLIENT_SECRET",
	"AZURE_CLIENT_SEND_CERTIFICATE_CHAIN",
	"AZURE_FEDERATED_TOKEN_FILE",
	"AZURE_PASSWORD",
	"AZURE_POD_IDENTITY_AUTHORITY_HOST",
	"AZURE_REGIONAL_AUTHORITY_NAME",
	"AZURE_SDK_GO_LOGGING",
	"AZURE_SUBSCRIPTION_ID",
	"AZURE_TENANT_ID",
	"AZURE_TOKEN_CREDENTIALS",
	"AZURE_USERNAME",
	"DEFAULT_IDENTITY_CLIENT_ID",
	"HTTPS_PROXY",
	"HTTP_PROXY",
	"IDENTITY_ENDPOINT",
	"IDENTITY_HEADER",
	"IDENTITY_SERVER_THUMBPRINT",
	"IMDS_ENDPOINT",
	"MSAL_FORCE_REGION",
	"MSI_ENDPOINT",
	"MSI_SECRET",
	"NO_PROXY",
	"PATH",
	"REGION_NAME",
	"http_proxy",
	"https_proxy",
	"no_proxy",
}

// TestEnvironmentNames_MatchesTheClosureCensus runs the diff in BOTH
// directions. Declared-minus-census catches a name nothing reads; census-minus-
// declared catches a reader whose name the entry never carries, which is the
// direction that actually breaks an install.
func TestEnvironmentNames_MatchesTheClosureCensus(t *testing.T) {
	for _, tc := range []struct {
		goos  string
		extra []string
	}{
		{OSLinux, []string{"HOME", "SSL_CERT_DIR", "SSL_CERT_FILE"}},
		{OSDarwin, []string{"HOME"}},
		{OSWindows, []string{"ProgramData", "SYSTEMROOT"}},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			got, err := EnvironmentNames(tc.goos)
			if err != nil {
				t.Fatalf("EnvironmentNames(%q): %v", tc.goos, err)
			}
			want := map[string]bool{}
			for _, n := range append(append([]string(nil), censusDeclared...), tc.extra...) {
				want[n] = true
			}
			have := map[string]bool{}
			for _, v := range got {
				have[v.Name] = true
				if v.Reason == "" {
					t.Errorf("%s: declared with no reason saying what it does", v.Name)
				}
			}
			for name := range want {
				if !have[name] {
					t.Errorf("the census reads %s and the entry does not declare it", name)
				}
			}
			for name := range have {
				if !want[name] {
					t.Errorf("the entry declares %s and the census names no reader for it", name)
				}
			}
		})
	}
}

// TestEnvironmentNames_TargetOSRowsDiffer is the control on the table's whole
// reason for taking a parameter: if the three targets produced the same list,
// the parameter would be decorative and an inline branch would do.
func TestEnvironmentNames_TargetOSRowsDiffer(t *testing.T) {
	names := func(goos string) map[string]bool {
		vars, err := EnvironmentNames(goos)
		if err != nil {
			t.Fatalf("EnvironmentNames(%q): %v", goos, err)
		}
		out := map[string]bool{}
		for _, v := range vars {
			out[v.Name] = true
		}
		return out
	}
	linux, darwin, windows := names(OSLinux), names(OSDarwin), names(OSWindows)

	// The Windows-only pair is on the Windows row and on neither other.
	for _, name := range []string{"SYSTEMROOT", "ProgramData"} {
		if !windows[name] {
			t.Errorf("windows: %s missing", name)
		}
		if linux[name] || darwin[name] {
			t.Errorf("%s appears on a non-windows target, where nothing reads it", name)
		}
	}
	// The unix trust roots are on linux and NOT on darwin: crypto/x509's
	// root_unix.go excludes darwin, whose roots come from the system keychain.
	for _, name := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if !linux[name] {
			t.Errorf("linux: %s missing", name)
		}
		if darwin[name] {
			t.Errorf("darwin: %s declared, but crypto/x509's root_unix.go excludes darwin and nothing reads it there", name)
		}
		if windows[name] {
			t.Errorf("windows: %s declared, but root_unix.go excludes windows too", name)
		}
	}
	// HOME is on both posix targets and on neither Windows one.
	if !linux["HOME"] || !darwin["HOME"] {
		t.Error("HOME missing from a posix target")
	}
	if windows["HOME"] {
		t.Error("windows: HOME declared, where the developer-tool arms read SYSTEMROOT instead")
	}
}

// TestEnvironmentNames_RefusesAnUnknownTarget is the bad-input arm: a target
// this table does not describe is refused rather than served the portable
// subset, and the refusal names what it does describe.
func TestEnvironmentNames_RefusesAnUnknownTarget(t *testing.T) {
	for _, goos := range []string{"", "freebsd", "plan9", "Windows"} {
		t.Run(goos, func(t *testing.T) {
			got, err := EnvironmentNames(goos)
			if err == nil {
				t.Fatalf("EnvironmentNames(%q) returned %d names and no error", goos, len(got))
			}
			if got != nil {
				t.Errorf("EnvironmentNames(%q) returned names beside its error", goos)
			}
			for _, want := range []string{OSLinux, OSDarwin, OSWindows} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name the supported target %q: %v", want, err)
				}
			}
		})
	}
}

// TestExcludedEnvironmentNames_StatesACostForEveryName is the other half of the
// disposition rule: a name the closure reads is declared with a reason or
// excluded with a cost, and there is no third class.
func TestExcludedEnvironmentNames_StatesACostForEveryName(t *testing.T) {
	excluded := ExcludedEnvironmentNames()
	if len(excluded) == 0 {
		t.Fatal("no name is excluded, which cannot be right: the closure reads names this collector does not need")
	}
	declared := map[string]bool{}
	for _, goos := range []string{OSLinux, OSDarwin, OSWindows} {
		vars, err := EnvironmentNames(goos)
		if err != nil {
			t.Fatalf("EnvironmentNames(%q): %v", goos, err)
		}
		for _, v := range vars {
			declared[v.Name] = true
		}
	}
	for _, v := range excluded {
		if v.Reason == "" {
			t.Errorf("%s is excluded with no cost stated", v.Name)
		}
		if declared[v.Name] {
			t.Errorf("%s is both declared and excluded", v.Name)
		}
	}
}

// TestExcludedEnvironmentNames_CarriesTheControl. GOOGLE_APPLICATION_CREDENTIALS
// is in this module's dependency closure and is read by nothing on any path
// this collector takes. A census that admitted every name it found would have
// declared it, so its presence in the EXCLUDED table with a path argument is
// what shows the census discriminates rather than enumerates.
func TestExcludedEnvironmentNames_CarriesTheControl(t *testing.T) {
	var reason string
	for _, v := range ExcludedEnvironmentNames() {
		if v.Name == "GOOGLE_APPLICATION_CREDENTIALS" {
			reason = v.Reason
		}
	}
	if reason == "" {
		t.Fatal("GOOGLE_APPLICATION_CREDENTIALS is not in the excluded table; " +
			"it is in this module's closure through the MCP SDK's x/oauth2 dependency and needs a stated disposition")
	}
	if !strings.Contains(reason, "Google") {
		t.Errorf("its exclusion does not say why it is off this collector's path: %q", reason)
	}
}

// TestExampleEntry_RendersTheTable is the generator's own row: the entry an
// operator copies carries the behavior block R2(c) needs and exactly the
// variables the table declares for that target.
func TestExampleEntry_RendersTheTable(t *testing.T) {
	entry, err := ExampleEntry(OSLinux, "/home/you/.knowledge/bin/knowledge-collector-azure")
	if err != nil {
		t.Fatalf("ExampleEntry: %v", err)
	}
	for _, want := range []string{
		`"type": "stdio"`,
		`"command": "/home/you/.knowledge/bin/knowledge-collector-azure"`,
		`"tool": "collect"`,
		`"summarizable": true`,
		`"embeddable": true`,
		`"syncable": true`,
		`"bm25_fields"`,
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("the example entry does not carry %s:\n%s", want, entry)
		}
	}
	vars, err := EnvironmentNames(OSLinux)
	if err != nil {
		t.Fatalf("EnvironmentNames: %v", err)
	}
	for _, v := range vars {
		if !strings.Contains(entry, `"`+v.Name+`"`) {
			t.Errorf("the example entry omits the declared variable %s", v.Name)
		}
	}
	if _, err := ExampleEntry("freebsd", "x"); err == nil {
		t.Error("ExampleEntry accepted a target the table does not describe")
	}
}

// TestReadmeCarriesTheGeneratedEntry pins the shipped documentation to the
// generator, which is what stops the two drifting: a name added to the table
// and not to the README fails here, and so does the reverse.
func TestReadmeCarriesTheGeneratedEntry(t *testing.T) {
	fenceTestCacheOnReadme(t)

	readme, err := os.ReadFile(readmeRelPath)
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	entry, err := ExampleEntry(OSLinux, exampleCommand)
	if err != nil {
		t.Fatalf("ExampleEntry: %v", err)
	}
	if !strings.Contains(string(readme), strings.TrimRight(entry, "\n")) {
		t.Errorf("README.md does not carry the generated linux entry verbatim; expected to find:\n%s", entry)
	}
	// The Windows-only names belong in the README too, or an operator on
	// Windows reads a table that silently omits what their host needs.
	for _, name := range []string{"SYSTEMROOT", "ProgramData"} {
		if !strings.Contains(string(readme), name) {
			t.Errorf("README.md does not mention the windows-only variable %s", name)
		}
	}
	// And every excluded name is accounted for in prose, with its cost.
	for _, v := range ExcludedEnvironmentNames() {
		if !strings.Contains(string(readme), v.Name) {
			t.Errorf("README.md does not state the disposition of the excluded variable %s", v.Name)
		}
	}
}

// exampleCommand is the command the README's worked entry names.
const exampleCommand = "/home/you/.knowledge/bin/knowledge-collector-azure"
