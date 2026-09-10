// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

// params_test.go — the collect id and the one parameter, refused loudly.

// TestAMalformedCollectIdIsRefusedNamingWhatIsWrong.
//
// THE ID IS THE WORKSPACE AND ALSO THE GRAPH, so a malformed one passed through
// would reach the caller as six per-enumeration failures, or worse as a walk
// that matched nothing and looked like an empty workspace.
func TestAMalformedCollectIdIsRefusedNamingWhatIsWrong(t *testing.T) {
	for _, arm := range []struct {
		id    string
		names []string
		why   string
	}{
		{"", []string{"collect id", "workspace"},
			"an empty id has nothing to quote, so the message names what the id IS instead"},
		{"   ", []string{"collect id", "workspace"},
			"whitespace trims to empty and takes the same arm"},
		{"acme/api", []string{"acme/api", "'/'"},
			"a repository path is the most likely wrong value and the message says so"},
		{"https://bitbucket.org/acme", []string{"https://bitbucket.org/acme"},
			"a URL quotes what was sent"},
		{"ACME", []string{"ACME"},
			"a workspace id is lower-case"},
		{"-acme", []string{"-acme", "hyphen"},
			"a leading hyphen is refused by its own arm"},
		{"acme-", []string{"acme-", "hyphen"},
			"and so is a trailing one"},
		{strings.Repeat("a", 63), []string{"63", "62"},
			"an over-length id names both the length and the bound"},
	} {
		_, err := offline(t).Walk(context.Background(), arm.id, walk.Params{},
			framework.ForeignContext{})
		if err == nil {
			t.Errorf("the id %q was accepted; %s", arm.id, arm.why)
			continue
		}
		for _, want := range arm.names {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of %q does not name %q: %v", arm.id, want, err)
			}
		}
	}
}

// TestTheEmptyIdSaysThereIsNothingToFallBackTo. It is the one arm that quotes
// nothing, because there is nothing to quote; what it names instead is that this
// collector reads no environment variable and takes no parameter naming a
// workspace, so a caller who expected one to be picked up is told otherwise.
func TestTheEmptyIdSaysThereIsNothingToFallBackTo(t *testing.T) {
	_, err := offline(t).Walk(context.Background(), "", walk.Params{},
		framework.ForeignContext{})
	if err == nil {
		t.Fatal("the empty id was accepted")
	}
	for _, want := range []string{"no environment variable", "no parameter", "fall back"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

// TestAValidWorkspaceIdIsAccepted is the known positive for every arm above: the
// validator is refusing malformed ids rather than every id.
//
// IT FAILS LATER, ON THE CREDENTIAL, which is the observable that the id passed:
// the credential is read after the id is validated and before any request.
func TestAValidWorkspaceIdIsAccepted(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	for _, id := range []string{"acme", "acme-team", "acme_team", "a", "acme1"} {
		_, err := offline(t).Walk(context.Background(), id, walk.Params{},
			framework.ForeignContext{})
		if err == nil {
			t.Errorf("%q: the walk ran with no credential", id)
			continue
		}
		if strings.Contains(err.Error(), "workspace id") {
			t.Errorf("the valid workspace %q was refused by the validator: %v", id, err)
		}
		if !strings.Contains(err.Error(), "BITBUCKET_USERNAME") {
			t.Errorf("%q: the walk failed for some reason other than the credential: %v", id, err)
		}
	}
}

// TestMaxConcurrencyIsRefusedOutsideItsBounds, naming the parameter, the value
// and the bound.
func TestMaxConcurrencyIsRefusedOutsideItsBounds(t *testing.T) {
	for _, arm := range []struct {
		value int
		names []string
	}{
		{0, []string{"max_concurrency", "0", "at least 1"}},
		{-1, []string{"max_concurrency", "-1", "at least 1"}},
		{33, []string{"max_concurrency", "33", "32"}},
		{500, []string{"max_concurrency", "500", "32"}},
	} {
		value := arm.value
		_, err := offline(t).Walk(context.Background(), "acme",
			walk.Params{MaxConcurrency: &value}, framework.ForeignContext{})
		if err == nil {
			t.Errorf("max_concurrency %d was accepted", arm.value)
			continue
		}
		for _, want := range arm.names {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of %d does not name %q: %v", arm.value, want, err)
			}
		}
	}
}

// TestZeroIsRefusedAndUnsetIsTheDefault is the pointer's whole reason for being.
//
// A PLAIN int CANNOT TELL THE TWO APART. A caller that omits the bound wants
// this collector's default; a caller that sends 0 has asked for no enumerations
// to run at once, which is not a thing to do — and with a plain int the second
// would silently become the first.
func TestZeroIsRefusedAndUnsetIsTheDefault(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	zero := 0
	_, err := offline(t).Walk(context.Background(), "acme",
		walk.Params{MaxConcurrency: &zero}, framework.ForeignContext{})
	if err == nil || !strings.Contains(err.Error(), "max_concurrency") {
		t.Errorf("an explicit zero was not refused by name: %v", err)
	}

	// UNSET reaches the credential check instead, which is the observable that it
	// was accepted and defaulted.
	_, err = offline(t).Walk(context.Background(), "acme", walk.Params{},
		framework.ForeignContext{})
	if err == nil || strings.Contains(err.Error(), "max_concurrency") {
		t.Errorf("an omitted bound was refused rather than defaulted: %v", err)
	}
}

// TestTheResolvedDefaultIsTheNumberTheDocumentationPublishes observes the VALUE
// an omitted bound resolves to, which the row above does not.
//
// "NOT REFUSED" IS NOT "TEN". Every assertion in this file until now was
// satisfied by any default at all, so the number in the README's parameter table
// was compared to nothing on this side either — and it is the number an operator
// plans a collect from. The refusal messages carry the resolved default, so they
// are where it is observable without exporting a second reader.
func TestTheResolvedDefaultIsTheNumberTheDocumentationPublishes(t *testing.T) {
	zero := 0
	_, err := offline(t).Walk(context.Background(), "acme",
		walk.Params{MaxConcurrency: &zero}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("an explicit zero was accepted")
	}
	// The message names the value an omitted bound would have taken.
	want := fmt.Sprintf("this collector's default of %d", walk.DefaultConcurrency)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal does not offer %q: %v", want, err)
	}
	if walk.DefaultConcurrency != 10 {
		t.Errorf("the resolved default is %d; the README's parameter table publishes 10, and a "+
			"reader plans a collect from it", walk.DefaultConcurrency)
	}

	// AND THE CEILING, on the same terms, from the arm that names it.
	tooLarge := walk.ConcurrencyCeiling + 1
	_, err = offline(t).Walk(context.Background(), "acme",
		walk.Params{MaxConcurrency: &tooLarge}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a bound above the ceiling was accepted")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("above the maximum of %d", walk.ConcurrencyCeiling)) {
		t.Errorf("the refusal does not name the ceiling it applied: %v", err)
	}
	if walk.ConcurrencyCeiling != 32 {
		t.Errorf("the ceiling is %d; the README's parameter table publishes 32",
			walk.ConcurrencyCeiling)
	}
}
