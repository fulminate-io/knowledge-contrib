// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"errors"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// reviewer_test.go — the reviewer reader, and the failure the source provider's
// version of it could not report.
//
// THAT VERSION RETURNED ONE STRING. A reviewer it could not encode, one it could
// not decode, one of a kind it did not model and one that was simply absent all
// produced the empty string, and its caller skipped on empty — so a lost reviewer
// and an absent one were the same event. The rows below are the four inputs, and
// they are four different outcomes here.

// TestAReviewerThatCannotBeDecodedFailsTheEnumeration is the row the split
// exists for.
//
// IT FAILS THE ENUMERATION RATHER THAN SKIPPING THE REVIEWER, deliberately. A
// skipped reviewer is an environment that appears to require no approval, which
// is a materially wrong statement about a deployment gate; the walk says it could
// not read the organization instead.
func TestAReviewerThatCannotBeDecodedFailsTheEnumeration(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.environments["acme/api"] = pages[gogithub.Environment]{{{
		ID:   new(int64(fixtureEnvironmentID)),
		Name: new(fixtureEnvironment),
		ProtectionRules: []*gogithub.ProtectionRule{{
			Type: new("required_reviewers"),
			// A value that encodes to a JSON string rather than to an object.
			// The decode into the identity shape then fails, which is the arm
			// that used to become an empty id.
			Reviewers: []*gogithub.RequiredReviewer{
				{Type: new("User"), Reviewer: "not-an-object"},
			},
		}},
	}}}

	_, err := runOneAllowingError(t, repos, actions, "github-environments")
	if err == nil {
		t.Fatal("a reviewer that could not be decoded produced a clean read, so the environment " +
			"reads as requiring no approval")
	}
	if errors.Is(err, collect.ErrDenied) || errors.Is(err, collect.ErrPartial) {
		t.Errorf("a malformed answer was reported as a scope this collector could not read: %v", err)
	}
	if !strings.Contains(err.Error(), "reviewer") {
		t.Errorf("the failure does not say what it was reading: %v", err)
	}
}

// TestAReviewerOfAnUnmodeledKindIsSkippedWithoutFailing is the other side of the
// split: a kind this collector does not model is not an error, and nothing is
// invented for it.
func TestAReviewerOfAnUnmodeledKindIsSkippedWithoutFailing(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.environments["acme/api"] = pages[gogithub.Environment]{{{
		ID:   new(int64(fixtureEnvironmentID)),
		Name: new(fixtureEnvironment),
		ProtectionRules: []*gogithub.ProtectionRule{{
			Type: new("required_reviewers"),
			Reviewers: []*gogithub.RequiredReviewer{
				{Type: new("App"), Reviewer: &gogithub.User{Login: new("bot")}},
				{Type: new("User"), Reviewer: &gogithub.User{Login: new("ada")}},
			},
		}},
	}}}

	got := runOne(t, repos, actions, "github-environments")
	ids := resourceIDs(got)
	if _, minted := ids["github:acme/User/bot"]; minted {
		t.Error("a reviewer of an unmodeled kind was minted as a user node")
	}
	// The known positive: the reviewer beside it, of a kind this collector DOES
	// model, was still read — so this is a walk that skipped one rather than one
	// that read none.
	if ids["github:acme/User/ada"] != ghgraph.ResourceTypeUser {
		t.Error("the modeled reviewer beside it was dropped too")
	}
}

// TestAReviewerWithNoIdentityIsSkippedWithoutFailing. A user with no login and a
// team with no slug name nothing, so there is nothing to point at.
func TestAReviewerWithNoIdentityIsSkippedWithoutFailing(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.environments["acme/api"] = pages[gogithub.Environment]{{{
		ID:   new(int64(fixtureEnvironmentID)),
		Name: new(fixtureEnvironment),
		ProtectionRules: []*gogithub.ProtectionRule{{
			Type: new("required_reviewers"),
			Reviewers: []*gogithub.RequiredReviewer{
				{Type: new("User"), Reviewer: &gogithub.User{}},
				{Type: new("Team"), Reviewer: &gogithub.Team{}},
				{Type: new("Team"), Reviewer: &gogithub.Team{Slug: new("platform")}},
			},
		}},
	}}}

	got := runOne(t, repos, actions, "github-environments")
	for _, res := range got.Resources {
		if res.ID == "github:acme/User/" || res.ID == "github:acme/Team/" {
			t.Errorf("a reviewer with no identity minted %q", res.ID)
		}
	}
	if resourceIDs(got)["github:acme/Team/platform"] != ghgraph.ResourceTypeTeam {
		t.Error("the identified reviewer beside them was dropped too")
	}
}

// TestAReviewerUserCarriesTheSameFieldsAnActorDoes is the row that keeps ONE
// person one node.
//
// THE SAME LOGIN ARRIVES FROM TWO ENUMERATIONS. A person who approves a
// deployment is a person who runs workflows, so their node is minted by the
// environments read from a protection rule and by the runs read from an actor.
// While the protection-rule reader took only the login, those two copies differed
// and the graph carried whichever the fan-out finished with. This row reads the
// reviewer path ALONE and asserts it produces the whole node, so a reader
// narrowed back to the login is a red here rather than a race in production.
func TestAReviewerUserCarriesTheSameFieldsAnActorDoes(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.environments["acme/api"] = pages[gogithub.Environment]{{{
		ID:   new(int64(fixtureEnvironmentID)),
		Name: new(fixtureEnvironment),
		ProtectionRules: []*gogithub.ProtectionRule{{
			Type: new("required_reviewers"),
			// The provider's protection rule carries a whole user object, which
			// is where the numeric id and the URL come from — and an address,
			// which is where they do not.
			Reviewers: []*gogithub.RequiredReviewer{{
				Type: new("User"),
				Reviewer: &gogithub.User{
					Login:   new(fixtureReviewer),
					ID:      new(int64(7)),
					Type:    new("User"),
					HTMLURL: new("https://github.com/" + fixtureReviewer),
					Email:   new(fixtureActorEmail),
				},
			}},
		}},
	}}}

	got := runOne(t, repos, actions, "github-environments")

	found := false
	for _, res := range got.Resources {
		if res.ID != "github:acme/User/"+fixtureReviewer {
			continue
		}
		found = true
		want := `{"kind":"User","name":"ada","id":7,"html_url":"https://github.com/ada"}`
		if res.Content != want {
			t.Errorf("the reviewer node carries\n  %s\nwant\n  %s", res.Content, want)
		}
	}
	if !found {
		t.Fatalf("the reviewer %q minted no node at all", fixtureReviewer)
	}
}

// TestAProtectionRuleThatNamesNobodyProducesNoApprovalEdge. A wait timer gates a
// deployment without naming an approver.
//
// THE FIXTURE'S WAIT TIMER CARRIES A REVIEWER, AND THAT IS THE WHOLE ROW. A wait
// timer with an empty reviewer list produces no edge whether the rule-type filter
// is there or not, so a fixture shaped that way leaves the filter unobserved —
// measured: deleting the filter left this test green until the reviewer below was
// added to the timer. What the provider actually sends is not the question; what
// is, is whether the filter is the thing deciding, and only a rule that WOULD
// produce an edge without it can answer that.
func TestAProtectionRuleThatNamesNobodyProducesNoApprovalEdge(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.environments["acme/api"] = pages[gogithub.Environment]{{{
		ID:   new(int64(fixtureEnvironmentID)),
		Name: new(fixtureEnvironment),
		ProtectionRules: []*gogithub.ProtectionRule{{
			Type:      new("wait_timer"),
			WaitTimer: new(30),
			Reviewers: []*gogithub.RequiredReviewer{
				{Type: new("User"), Reviewer: &gogithub.User{Login: new("ada")}},
			},
		}},
	}}}

	got := runOne(t, repos, actions, "github-environments")
	for _, rel := range got.Relations {
		if rel.Type == ghgraph.EdgeRequiresApproval {
			t.Errorf("a %q rule produced the approval edge %s -> %s. Only a required-reviewers "+
				"rule names an approver; every other kind gates a deployment without naming one",
				"wait_timer", rel.FromID, rel.ToID)
		}
	}
	// And no reviewer node was minted for it either: a node standing for an
	// approver the provider never named is the same wrong statement one level
	// down.
	if _, minted := resourceIDs(got)["github:acme/User/ada"]; minted {
		t.Error("a wait timer's reviewer list minted a reviewer node")
	}
	// The known positive: the environment itself was still emitted with its rule
	// recorded in its detail, so the absence above is about the edge.
	if _, ok := resourceIDs(got)["github:acme/Environment/acme/api/production"]; !ok {
		t.Fatal("the environment was not emitted at all")
	}
	for _, res := range got.Resources {
		if res.ID == "github:acme/Environment/acme/api/production" &&
			!strings.Contains(res.Content, "wait_timer") {
			t.Errorf("the environment's detail does not record the rule: %s", res.Content)
		}
	}
}
