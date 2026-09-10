// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"os"
	"slices"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// empty_sensitive_test.go — WHICH DECLARED NAMES TELL PRESENT-AND-EMPTY APART
// FROM ABSENT, measured through this collector's own resolution.
//
// WHY THE QUESTION MATTERS OUTSIDE THIS MODULE. A worked entry that renders
// `${NAME:-}` hands the child that name present and empty, because the reference
// is expanded by the process serving the collect and resolves to the empty string
// when that process does not hold the name. For a name this collector treats as
// absent that is inert; for one its credential chain refuses at CONSTRUCTION, it
// is a collect that never starts. The describe declaration carries the answer per
// name and the installer's documentation gate reads it, so this test is what
// keeps that declaration true.
//
// THE TARGET OS IS NAMED, NEVER TAKEN FROM THE RUNNER. EnvironmentNames returns
// 35 names for linux, 34 for windows and 33 for darwin — the unix trust roots and
// the POSIX home name differ by target — so a sweep taking runtime.GOOS measures
// a different denominator on every machine and a count assertion beside it means
// nothing. This test fixes linux, the target the CI runner and the release matrix
// both use, and says so.
const emptySensitiveTargetOS = OSLinux

// clearDeclaredEnvironment removes every name this collector declares for the
// named target from the process environment, restoring each at the end of the
// test, so every arm below starts from the same baseline whatever the developer's
// shell holds.
//
// t.Setenv is what registers the restoration; the Unsetenv after it is what makes
// the baseline ABSENT rather than empty, which is the distinction under test.
func clearDeclaredEnvironment(t *testing.T, names []EnvVar) {
	t.Helper()
	for _, v := range names {
		t.Setenv(v.Name, "")
		os.Unsetenv(v.Name)
	}
}

// credentialOutcome is what the chain's CONSTRUCTOR produces, reduced to a
// comparable value. Construction is the whole observation: azidentity refuses a
// malformed AZURE_TOKEN_CREDENTIALS here, before any network call, which is why
// this row needs no Azure account and makes no request.
func credentialOutcome() string {
	if _, err := azidentity.NewDefaultAzureCredential(nil); err != nil {
		return "error: " + err.Error()
	}
	return "constructed"
}

func TestEveryDeclaredNameEmptyVersusAbsent(t *testing.T) {
	declared, err := EnvironmentNames(emptySensitiveTargetOS)
	if err != nil {
		t.Fatalf("EnvironmentNames(%q): %v", emptySensitiveTargetOS, err)
	}
	if len(declared) == 0 {
		t.Fatalf("this collector declares no names for %s; the sweep would pass vacuously", emptySensitiveTargetOS)
	}
	clearDeclaredEnvironment(t, declared)

	// THE EXPECTATION, read from the declaration this module SERVES rather than
	// from a list beside it. The two sets are derived from different places, so the
	// comparison at the end is a real one.
	var want []string
	for _, e := range DescribedEnvironment() {
		if e.EmptySensitive {
			want = append(want, e.Name)
		}
	}
	slices.Sort(want)

	baseline := credentialOutcome()

	var got []string
	for _, v := range declared {
		os.Setenv(v.Name, "")
		outcome := credentialOutcome()
		os.Unsetenv(v.Name)
		if outcome != baseline {
			got = append(got, v.Name)
		}
	}
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("on %s, the names that tell present-and-empty apart from absent are %v, but the declaration marks %v\n"+
			"a name measured here and not marked ships a README the installer gate will admit and a collect that then fails;\n"+
			"a name marked here and not measured refuses a worked entry for no reason", emptySensitiveTargetOS, got, want)
	}

	// THE SAME-RUN KNOWN POSITIVE, through the same instrument: a NON-EMPTY value
	// the chain refuses moves the outcome, so a constructor that had stopped
	// reading its environment would fail this row rather than report a clean sweep.
	os.Setenv("AZURE_TOKEN_CREDENTIALS", "not-a-credential-set")
	moved := credentialOutcome()
	os.Unsetenv("AZURE_TOKEN_CREDENTIALS")
	if moved == baseline {
		t.Error("control: AZURE_TOKEN_CREDENTIALS set to a nonsense value did not move the constructor; " +
			"the sweep above is measuring nothing")
	}
}

// TestTheSubscriptionNameTreatsEmptyAsAbsent is the module's OWN read, the other
// half of "this collector's resolution": the credential chain is a dependency,
// and the one name this module reads itself is resolved here.
//
// IT IS UNMARKED AND THE TEST SAYS WHY: the read is `ok && v != ""`, so a present
// but empty value takes the same arm as an absent one and the refusal is
// byte-identical. A change to that guard reds this row.
func TestTheSubscriptionNameTreatsEmptyAsAbsent(t *testing.T) {
	absent := func(string) (string, bool) { return "", false }
	empty := func(name string) (string, bool) {
		if name == envSubscriptionID {
			return "", true
		}
		return "", false
	}
	set := func(name string) (string, bool) {
		if name == envSubscriptionID {
			return "00000000-0000-0000-0000-000000000001", true
		}
		return "", false
	}

	_, absentErr := resolveSubscriptionID("", Params{}, absent)
	_, emptyErr := resolveSubscriptionID("", Params{}, empty)
	if absentErr == nil || emptyErr == nil {
		t.Fatal("naming no subscription resolved one; this driver cannot observe the guard")
	}
	if absentErr.Error() != emptyErr.Error() {
		t.Errorf("%s present-and-empty produced a different refusal from absent, so it discriminates and is unmarked\n"+
			"absent: %v\nempty:  %v", envSubscriptionID, absentErr, emptyErr)
	}
	// The same-run known positive: a real value DOES move the result, so the row
	// above is not passing on a resolver that reads nothing.
	got, err := resolveSubscriptionID("", Params{}, set)
	if err != nil {
		t.Fatalf("control: a real subscription id was refused: %v", err)
	}
	if got != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("control: a real subscription id resolved to %q", got)
	}
}
