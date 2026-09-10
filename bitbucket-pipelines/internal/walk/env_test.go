// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

// env_test.go — the credential arms and the history-depth arms, read through the
// REAL process environment rather than through an injected reader.
//
// t.Setenv IS THE SEAM. A reader field would be a double on the far side of the
// thing under test — the point of these rows is that the collector reads its
// environment, and a fake environment would prove only that it reads a field.

// TestBothCredentialHalvesAreRequiredAndAMissingOneNamesBoth.
func TestBothCredentialHalvesAreRequiredAndAMissingOneNamesBoth(t *testing.T) {
	for _, arm := range []struct {
		username, appPassword string
		why                   string
	}{
		{"", "", "neither is set"},
		{"user", "", "the app password is missing"},
		{"", "password", "the username is missing"},
		{"", "password", "present-and-empty is the state an entry writing ${NAME:-} produces"},
	} {
		t.Setenv(enventry.UsernameVariable, arm.username)
		t.Setenv(enventry.AppPasswordVariable, arm.appPassword)

		_, err := offline(t).Walk(context.Background(), "acme", walk.Params{},
			framework.ForeignContext{})
		if err == nil {
			t.Errorf("%s: the walk ran anyway", arm.why)
			continue
		}
		// THE MESSAGE NAMES BOTH, always. An operator who set one and not the
		// other is one keystroke from working, and a message naming only the
		// missing half leaves them guessing which pair is expected.
		for _, want := range []string{enventry.UsernameVariable, enventry.AppPasswordVariable} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the refusal does not name %q: %v", arm.why, want, err)
			}
			// AND IT SHOWS THE SHAPE THE ENTRY CARRIES. The installed entry
			// names each variable as a reference to the operator's own
			// environment; a message that told them to write the app password
			// into the entry as a value would instruct exactly what this
			// collector's documents forbid and what the installer will not
			// write.
			if !strings.Contains(err.Error(), "${"+want+"}") {
				t.Errorf("%s: the refusal does not show the reference form for %q: %v",
					arm.why, want, err)
			}
		}
		for _, forbidden := range []string{"as the value", "with your own"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Errorf("%s: the refusal instructs a literal credential value (%q): %v",
					arm.why, forbidden, err)
			}
		}
	}
}

// TestAWhitespaceAppPasswordIsNotTreatedAsMissing is the boundary the arm above
// deliberately does not claim: a password of spaces is a value the operator set,
// so it is sent and refused by the provider rather than second-guessed here.
//
// THE OBSERVABLE IS THAT THE WALK GETS PAST THE CREDENTIAL CHECK, which it shows
// by failing on the network instead.
func TestAWhitespaceAppPasswordIsNotTreatedAsMissing(t *testing.T) {
	t.Setenv(enventry.UsernameVariable, "user")
	t.Setenv(enventry.AppPasswordVariable, "   ")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := offline(t).Walk(ctx, "acme", walk.Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("the walk succeeded against a cancelled context")
	}
	if strings.Contains(err.Error(), enventry.AppPasswordVariable) {
		t.Errorf("a non-empty app password was refused as missing: %v", err)
	}
}

// TestTheHistoryDepthIsRefusedWhenItIsPresentAndUnusable.
//
// WHOSE REQUIREMENT THIS IS. The ticket pins only the default and the fact of
// the environment override; the fail-loud parse is this module's own call, and
// what mandates it is the repository's rule that bad input always errors, never
// silently coerces or defaults. The source provider parses this value with a
// condition that SILENTLY IGNORES a non-integer, a zero and a negative and walks
// at the default, so an operator who typed a letter into the number gets fifty
// runs and no indication that what they typed was never read.
func TestTheHistoryDepthIsRefusedWhenItIsPresentAndUnusable(t *testing.T) {
	t.Setenv(enventry.UsernameVariable, "user")
	t.Setenv(enventry.AppPasswordVariable, "password")

	for _, arm := range []struct {
		value string
		names []string
	}{
		{"abc", []string{enventry.HistoryDepthVariable, `"abc"`, "whole number"}},
		{"10O", []string{enventry.HistoryDepthVariable, `"10O"`, "whole number"}},
		{"0", []string{enventry.HistoryDepthVariable, "0", "at least 1"}},
		{"-5", []string{enventry.HistoryDepthVariable, "-5", "at least 1"}},
		{"", []string{enventry.HistoryDepthVariable, "empty string"}},
		{"12.5", []string{enventry.HistoryDepthVariable, `"12.5"`}},
	} {
		t.Setenv(enventry.HistoryDepthVariable, arm.value)

		_, err := offline(t).Walk(context.Background(), "acme", walk.Params{},
			framework.ForeignContext{})
		if err == nil {
			t.Errorf("%q was accepted", arm.value)
			continue
		}
		for _, want := range arm.names {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of %q does not name %s: %v", arm.value, want, err)
			}
		}
		// AND IT NAMES THE DEFAULT, so an operator who wanted it knows how to ask
		// for it: by unsetting the variable rather than by finding the number.
		if !strings.Contains(err.Error(), "50") {
			t.Errorf("the refusal of %q does not name the default: %v", arm.value, err)
		}
	}
}

// TestAnAbsentHistoryDepthIsTheDefaultAndAPositiveOneIsAccepted is the known
// positive for the arms above: the parse refuses unusable values rather than
// every value.
//
// THE OBSERVABLE IS THAT THE WALK GETS PAST THE PARSE, which it shows by failing
// on the cancelled context instead of on the variable.
func TestAnAbsentHistoryDepthIsTheDefaultAndAPositiveOneIsAccepted(t *testing.T) {
	t.Setenv(enventry.UsernameVariable, "user")
	t.Setenv(enventry.AppPasswordVariable, "password")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// ABSENT.
	_, err := offline(t).Walk(ctx, "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil && strings.Contains(err.Error(), enventry.HistoryDepthVariable) {
		t.Errorf("an unset history depth was refused: %v", err)
	}

	// SET, to values on both sides of the provider's own page maximum. This
	// enumeration PAGINATES to the depth, so a value above 100 is honored rather
	// than refused — which is where this collector differs from the sibling that
	// reads a single page and refuses a cap above it.
	for _, value := range []string{"1", "50", "100", "500"} {
		t.Setenv(enventry.HistoryDepthVariable, value)
		_, err = offline(t).Walk(ctx, "acme", walk.Params{}, framework.ForeignContext{})
		if err != nil && strings.Contains(err.Error(), enventry.HistoryDepthVariable) {
			t.Errorf("the history depth %q was refused: %v", value, err)
		}
	}
}

// TestTheEnvironmentClosureIsExactlyTheDeclaredNames pins the DECLARATION:
// three names, in the spellings the install table is built from. The census that
// compares them against the names the module actually READS is
// TestTheNamesReadAndTheNamesDeclaredAreTheSameSet in internal/enventry, which
// resolves the argument of every environment lookup in the module.
//
// WHY THE DECLARATION IS WORTH PINNING ON ITS OWN. A stdio child receives
// exactly what its config entry declares, and the entry is built from this list,
// so a name whose spelling drifts here is a variable an operator sets and the
// collector never sees — a failure the census upstream of it cannot show,
// because that one compares two lists which would have drifted together.
func TestTheEnvironmentClosureIsExactlyTheDeclaredNames(t *testing.T) {
	declared := map[string]bool{}
	for _, name := range enventry.Names() {
		declared[name] = true
	}
	if len(declared) != 3 {
		t.Errorf("the declared closure is %v, want exactly three names", enventry.Names())
	}
	for _, want := range []string{
		"BITBUCKET_USERNAME", "BITBUCKET_APP_PASSWORD", "BITBUCKET_PIPELINE_HISTORY_DEPTH",
	} {
		if !declared[want] {
			t.Errorf("the declared closure does not name %q", want)
		}
	}
}
