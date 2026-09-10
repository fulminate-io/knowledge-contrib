// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"os"
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/enventry"
)

// empty_sensitive_test.go — WHICH DECLARED NAMES TELL PRESENT-AND-EMPTY APART
// FROM ABSENT, all three of them, measured through this module's own resolution.
//
// WHY THE QUESTION MATTERS OUTSIDE THIS MODULE. A worked entry that renders
// `${NAME:-}` hands the child that name present and empty, because the reference
// is expanded by the process serving the collect and resolves to the empty string
// when that process does not hold the name. For a name this collector treats as
// absent that is inert; for one it refuses, it is a collect that never runs. The
// describe declaration carries the answer per name and the installer's
// documentation gate reads it, so this test is what keeps that declaration true.
//
// ALL THREE NAMES ARE DRIVEN, not only the one that discriminates. The two
// credential halves are unmarked on a MEASURED ground rather than an unexamined
// one: `credentials` reads them with os.Getenv and guards on `== ""`, which
// cannot distinguish empty from absent, so both states produce the same refusal.
// Leaving them undriven would make "unmarked" a statement nobody checked.

// resolutionOutcome drives both of this module's environment readers and reduces
// the pair to one comparable value.
func resolutionOutcome() string {
	out := ""
	if _, _, err := credentials(); err != nil {
		out += "credentials error: " + err.Error()
	} else {
		out += "credentials ok"
	}
	if depth, err := historyDepth(); err != nil {
		out += " | depth error: " + err.Error()
	} else {
		out += " | depth ok: " + itoa(depth)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestEveryDeclaredNameEmptyVersusAbsent(t *testing.T) {
	declared := enventry.Names()
	if len(declared) == 0 {
		t.Fatal("this collector declares no environment names; the sweep would pass vacuously")
	}
	// Every arm starts from the same baseline whatever the developer's shell
	// holds. t.Setenv registers the restoration; the Unsetenv is what makes the
	// baseline ABSENT rather than empty, which is the distinction under test.
	for _, name := range declared {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}

	// THE EXPECTATION, read from the declaration this module SERVES.
	var want []string
	for _, e := range (Collector{}).Describe().Environment {
		if e.EmptySensitive {
			want = append(want, e.Name)
		}
	}
	slices.Sort(want)

	baseline := resolutionOutcome()

	var got []string
	for _, name := range declared {
		os.Setenv(name, "")
		outcome := resolutionOutcome()
		os.Unsetenv(name)
		if outcome == baseline {
			continue
		}
		got = append(got, name)
		if !containsSub(outcome, name) {
			t.Errorf("%s discriminates but its outcome does not name it: %s", name, outcome)
		}
	}
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("the names that tell present-and-empty apart from absent are %v, but the declaration marks %v\n"+
			"a name measured here and not marked ships a README the installer gate will admit and a collect that then fails;\n"+
			"a name marked here and not measured refuses a worked entry for no reason", got, want)
	}

	// THE SAME-RUN KNOWN POSITIVES, one per reader, so neither half of the outcome
	// above can be passing on a resolver that reads nothing.
	os.Setenv(enventry.UsernameVariable, "someone")
	os.Setenv(enventry.AppPasswordVariable, "a-password-value")
	credsMoved := resolutionOutcome()
	os.Unsetenv(enventry.UsernameVariable)
	os.Unsetenv(enventry.AppPasswordVariable)
	if credsMoved == baseline {
		t.Error("control: both credential halves set to real values did not move the outcome")
	}

	os.Setenv(enventry.HistoryDepthVariable, "7")
	depthMoved := resolutionOutcome()
	os.Unsetenv(enventry.HistoryDepthVariable)
	if depthMoved == baseline {
		t.Error("control: a real history depth did not move the outcome")
	}
}

func containsSub(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
