// SPDX-License-Identifier: Apache-2.0

package enventry_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/enventry"
)

// enventry_test.go — the environment block, as SET EQUALITY per target, with the
// omission set pinned.
//
// TWO CASES, NOT THREE. Exactly one name differs between them, and the case that
// carries it is a build target this repository ships and no runner here tests
// on — which is the whole reason the target is an argument rather than a read of
// the running platform. A branch on the running platform would leave one case
// unrunnable everywhere, and unrunnable is indistinguishable from correct.

// the names read on every target, written out here as the EXTERNAL expectation.
// A change to the computed set that is not also made here is a red, which is
// what makes the set a decision rather than whatever the code currently returns.
var commonExpectation = []string{
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

func TestNamesPerTargetIsSetEquality(t *testing.T) {
	for _, tc := range []struct {
		goos      string
		homeName  string
		otherHome string
	}{
		{"linux", "HOME", "APPDATA"},
		{"darwin", "HOME", "APPDATA"},
		{"windows", "APPDATA", "HOME"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			want := append(slices.Clone(commonExpectation), tc.homeName)
			slices.Sort(want)

			got := enventry.Names(tc.goos)
			if !slices.Equal(got, want) {
				t.Errorf("the declared set for %s is not the expected one.\ngot:  %v\nwant: %v",
					tc.goos, got, want)
			}
			// THE SAME-RUN CONTROL. Asserting the set contains the right home
			// variable proves little on its own; asserting the OTHER one is
			// absent in the same run is what shows the target argument decides
			// anything at all.
			if slices.Contains(got, tc.otherHome) {
				t.Errorf("the %s set contains %q, which belongs to the other case", tc.goos, tc.otherHome)
			}
		})
	}
}

// TestTheOmittedNamesAreAbsentFromEveryCase pins the omission set. Without it,
// the equality above catches a name added to or dropped from the declared set
// but NOT a dependency change that starts reading a name nobody declared. A
// pinned omission set turns that into an observable.
func TestTheOmittedNamesAreAbsentFromEveryCase(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		got := enventry.Names(goos)
		for omitted, reason := range enventry.DeliberatelyOmitted {
			if slices.Contains(got, omitted) {
				t.Errorf("%s: %q is in the declared set, but it is deliberately omitted because it %s",
					goos, omitted, reason)
			}
		}
	}
}

// TestEveryOmissionCarriesAReason keeps the omission set from becoming a bare
// list. A name left out with no reason is indistinguishable from one forgotten.
func TestEveryOmissionCarriesAReason(t *testing.T) {
	if len(enventry.DeliberatelyOmitted) == 0 {
		t.Fatal("the omission set is empty; the assertion above would check nothing")
	}
	for name, reason := range enventry.DeliberatelyOmitted {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is omitted with no reason", name)
		}
	}
}

// TestNamesAreSortedAndUnique keeps the rendered example stable: an operator
// diffing two generated entries should see their own edits and nothing else.
func TestNamesAreSortedAndUnique(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		got := enventry.Names(goos)
		if !slices.IsSorted(got) {
			t.Errorf("%s: the declared set is not sorted: %v", goos, got)
		}
		seen := map[string]bool{}
		for _, name := range got {
			if seen[name] {
				t.Errorf("%s: %q appears twice", goos, name)
			}
			seen[name] = true
		}
	}
}

func TestNamesReturnsACopy(t *testing.T) {
	first := enventry.Names("linux")
	first[0] = "MUTATED"
	if enventry.Names("linux")[0] == "MUTATED" {
		t.Error("Names returns the backing array; a caller can rewrite the declared set")
	}
}

// TestExampleRendersOneLinePerName is the documentation half: the guide shows
// this block, and it is generated rather than retyped so it cannot drift from
// the set above.
func TestExampleRendersOneLinePerName(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		example := enventry.Example(goos)
		lines := strings.Split(strings.TrimSpace(example), "\n")
		names := enventry.Names(goos)
		if len(lines) != len(names) {
			t.Fatalf("%s: the example has %d lines for %d names", goos, len(lines), len(names))
		}
		for i, name := range names {
			if !strings.HasPrefix(lines[i], name+" = ") {
				t.Errorf("%s: line %d is %q, want it to declare %q", goos, i, lines[i], name)
			}
		}
		// No line carries a real value. A worked example that shipped one would
		// either be wrong for every reader or be a credential.
		if strings.Contains(example, "/Users/") || strings.Contains(example, "AIza") {
			t.Errorf("%s: the rendered example carries what looks like a real value", goos)
		}
	}
}
