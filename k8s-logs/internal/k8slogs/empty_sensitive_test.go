// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"k8s.io/client-go/rest"
)

// empty_sensitive_test.go — WHICH DECLARED NAMES TELL PRESENT-AND-EMPTY APART
// FROM ABSENT, measured through this collector's own resolution.
//
// WHY THE QUESTION MATTERS OUTSIDE THIS MODULE. A worked entry that renders
// `${NAME:-}` hands the child that name present and empty, because the reference
// is expanded by the process serving the collect and resolves to the empty
// string when that process does not hold the name. This collector's resolver
// branches on the PRESENCE of the service-host variable, so present-and-empty
// takes the in-pod arm and produces a different refusal from absent — the same
// arm a real value takes. The describe declaration carries that answer per name
// and the installer's documentation gate reads it; this test keeps it true.

// resolveOutcome drives Resolver.Config with a kubeconfig that cannot resolve
// and an in-cluster loader that always fails, so the only thing that can move
// the result is the routing branch under test.
func resolveOutcome(t *testing.T) string {
	t.Helper()
	broken := func() (*rest.Config, error) {
		return nil, fmt.Errorf("open /var/run/secrets: no such file or directory")
	}
	_, err := Resolver{InCluster: broken}.Config("")
	if err == nil {
		t.Fatal("resolution succeeded with both sources failing; this driver cannot observe the branch")
	}
	return err.Error()
}

// TestTheServiceHostNameTellsEmptyApartFromAbsent is the measurement the mark
// rests on, and it drives all three states rather than two: present-and-empty
// must be byte-identical to a REAL VALUE and DIFFERENT from absent. Two states
// would leave "empty behaves oddly" indistinguishable from "empty is its own
// third thing".
func TestTheServiceHostNameTellsEmptyApartFromAbsent(t *testing.T) {
	// Every arm shares this: a kubeconfig path that does not exist, so the
	// kubeconfig source fails and the branch below is the only live variable.
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	t.Setenv("KUBECONFIG", missing)
	os.Unsetenv(EnvKubernetesServiceHost)
	absent := resolveOutcome(t)

	t.Setenv("KUBECONFIG", missing)
	t.Setenv(EnvKubernetesServiceHost, "")
	empty := resolveOutcome(t)

	t.Setenv("KUBECONFIG", missing)
	t.Setenv(EnvKubernetesServiceHost, "10.96.0.1")
	real := resolveOutcome(t)

	if empty == absent {
		t.Errorf("%s present-and-empty produced the same refusal as absent; the mark on it would be false\nboth: %s",
			EnvKubernetesServiceHost, empty)
	}
	if empty != real {
		t.Errorf("%s present-and-empty is not byte-identical to a real value, so the branch is not a presence branch\n"+
			"empty: %s\nreal:  %s", EnvKubernetesServiceHost, empty, real)
	}
	// The two arms named, so a refusal that stopped naming the mount would red
	// here rather than pass on inequality alone.
	if !contains(absent, "no credentials") {
		t.Errorf("the absent arm is not the combined refusal: %s", absent)
	}
	if !contains(empty, "service account") {
		t.Errorf("the present-and-empty arm is not the in-cluster refusal: %s", empty)
	}
}

// TestTheDeclarationMarksExactlyTheDiscriminatingNames is the derivation: the
// declaration this module serves must mark the name measured above and nothing
// else. It reds when a mark is added for a name that does not discriminate and
// when the one that does loses its mark.
func TestTheDeclarationMarksExactlyTheDiscriminatingNames(t *testing.T) {
	var marked []string
	for _, e := range DescribedEnvironment() {
		if e.EmptySensitive {
			marked = append(marked, e.Name)
		}
	}
	slices.Sort(marked)
	want := []string{EnvKubernetesServiceHost}
	if !slices.Equal(marked, want) {
		t.Errorf("the declaration marks %v, but the measured discriminating set is %v", marked, want)
	}
	// The control: the declaration is not empty, so a DescribedEnvironment that
	// returned nothing at all would not satisfy the row above.
	if len(DescribedEnvironment()) < 2 {
		t.Error("the declaration carries fewer than two names; this comparison would be near-vacuous")
	}
	// AND THE MARK IS ON A not-carried NAME, which is the whole reason the
	// installer reads the marks off their own answer rather than off the class
	// table: that table drops this class by construction, so this mark would have
	// nowhere to travel.
	for _, e := range DescribedEnvironment() {
		if e.Name == EnvKubernetesServiceHost && e.Class != "not-carried" {
			t.Errorf("%s is declared %q; the mark's carrier assumes it stays not-carried", e.Name, e.Class)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
