// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"fmt"
	"slices"
	"testing"
)

// empty_sensitive_test.go — WHICH DECLARED NAMES TELL PRESENT-AND-EMPTY APART
// FROM ABSENT, measured through this collector's own resolution rather than
// asserted.
//
// WHY THE QUESTION MATTERS OUTSIDE THIS MODULE. A worked entry in a README that
// renders `${NAME:-}` hands the child that name present and empty, because the
// reference is expanded by the process serving the collect and resolves to the
// empty string when that process does not hold the name. For a name this
// collector treats as absent that is inert; for one it refuses, it is a collect
// that never runs. The describe declaration carries the answer per name, and the
// installer's documentation gate reads it — so this test is what keeps that
// declaration true.
//
// IT IS A DERIVATION, NOT A TRANSCRIPTION. The expected set is read from the
// declaration this module serves; the measured set is produced by driving every
// declared name through LoadSettings. A name that goes inert and a name that
// starts discriminating both red here, by name, which is the property a
// hand-written list beside the map could not have.

// loadOutcome is what one environment produces, reduced to a comparable value:
// the refusal if there is one, otherwise the whole resolved settings struct.
func loadOutcome(t *testing.T, env map[string]string) string {
	t.Helper()
	s, err := LoadSettings(func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	})
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("settings: %+v", s)
}

func TestEveryDeclaredNameEmptyVersusAbsent(t *testing.T) {
	declared := Names()
	if len(declared) == 0 {
		t.Fatal("this collector declares no environment names; the sweep below would pass vacuously")
	}

	// THE EXPECTATION, read from the declaration this module SERVES. Nothing here
	// enumerates a name: if the map changes, this changes with it, and the
	// comparison at the end is between two things derived from different places.
	want := make([]string, 0, len(declared))
	for _, e := range DescribedEnvironment() {
		if e.EmptySensitive {
			want = append(want, e.Name)
		}
	}
	slices.Sort(want)

	baseline := loadOutcome(t, map[string]string{})

	var got []string
	for _, name := range declared {
		outcome := loadOutcome(t, map[string]string{name: ""})
		if outcome == baseline {
			continue
		}
		got = append(got, name)
		// EVERY REFUSAL NAMES ITS VARIABLE. An operator reading "something in the
		// environment is wrong" cannot act; the whole point of refusing rather than
		// coercing is that the message says which line to remove.
		if !containsName(outcome, name) {
			t.Errorf("%s discriminates but its outcome does not name it: %s", name, outcome)
		}
	}
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("the names that tell present-and-empty apart from absent are %v, but the declaration marks %v\n"+
			"a name measured here and not marked ships a README the installer gate will admit and a collect that then fails;\n"+
			"a name marked here and not measured refuses a worked entry for no reason", got, want)
	}

	// THE ANTI-VACUITY CONTROL, in the same run and through the same driver: a
	// non-empty value moves the outcome for a marked name, so a LoadSettings that
	// had stopped reading its environment entirely would fail this row rather than
	// report a clean sweep.
	if loadOutcome(t, map[string]string{EnvPassword: "a-value"}) == baseline {
		t.Errorf("control: %s set to a real value did not move the outcome; the resolver is reading nothing", EnvPassword)
	}

	// THE UNMARKED EIGHT TAKE A DIFFERENT GROUND, AND THE TEST SAYS SO RATHER THAN
	// SHARING THE MARKED HALF'S. The six proxy delegates and the two trust roots
	// are not read by this module at all: they are declared because the config
	// entry's env block is the child's WHOLE environment, and net/http's proxy
	// resolution and crypto/x509's trust-root readers consult them from that
	// environment directly. So their inertness under LoadSettings is STRUCTURAL —
	// nothing on this resolution path consults them — rather than a reader
	// treating empty as absent, and no same-run control that moved the outcome is
	// available for them. A control that DID move it would mean this module had
	// started reading the name, which is itself the red.
	//
	// What that leaves unestablished is stated rather than papered over: how their
	// own consumers treat empty versus absent is a property of net/http and
	// crypto/x509 and is not measured here.
	for _, e := range DescribedEnvironment() {
		if e.EmptySensitive {
			continue
		}
		if got := loadOutcome(t, map[string]string{e.Name: "a-real-value"}); got != baseline {
			t.Errorf("%s is unmarked on the ground that this module's own resolution never consults it, "+
				"but setting it to a real value moved the outcome: %s", e.Name, got)
		}
	}

	// The floor the split rests on: both halves are non-empty, so neither "every
	// name discriminates" nor "no name does" can satisfy the comparison above.
	if len(got) == 0 || len(got) == len(declared) {
		t.Errorf("the measured split is %d of %d, which is one of the two degenerate answers", len(got), len(declared))
	}
}

// containsName reports whether an outcome string mentions a variable name.
func containsName(outcome, name string) bool {
	for i := 0; i+len(name) <= len(outcome); i++ {
		if outcome[i:i+len(name)] == name {
			return true
		}
	}
	return false
}
