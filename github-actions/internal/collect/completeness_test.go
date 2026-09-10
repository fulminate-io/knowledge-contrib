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

// completeness_test.go — EVERY READ OUTCOME REACHES THE VERDICT TRUTHFULLY.
//
// This is the file that proves the one place this collector does NOT reproduce
// the source provider's behavior. That provider logs a refused or failed
// per-repository read and continues with its walk still asserting COMPLETE, at
// nine separate sites. A complete walk is what lets the receiving server treat
// everything the walk did not carry as gone, so a token that lost a scope between
// two collects would silently delete what it could no longer read, and the
// collect would report success.
//
// Five outcomes, and each row asserts BOTH the verdict and the reason text.

// TestARefusedSecretsReadIsIncompleteAndNamesWhatItMissed is the row the
// completeness fix rests on: the exact input class where the source provider is
// wrong.
func TestARefusedSecretsReadIsIncompleteAndNamesWhatItMissed(t *testing.T) {
	repos, actions := fixtureAPI()
	actions.repoSecretsErr = map[string]error{
		"acme/web": refusal(403, "Resource not accessible by personal access token"),
	}

	got, err := runOneAllowingError(t, repos, actions, "github-secrets")
	if err == nil {
		t.Fatal("a 403 on one repository's secrets produced a clean read. That is the source " +
			"provider's defect: a walk that could not look asserts it saw everything")
	}
	if !errors.Is(err, collect.ErrDenied) {
		t.Errorf("a 403 was not classified as a refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "github-secrets") || !strings.Contains(err.Error(), "acme/web") {
		t.Errorf("the reason names neither the enumeration nor the repository: %v", err)
	}

	// THE READABLE REPOSITORY'S SECRETS ARE STILL EMITTED. A refusal on one
	// scope may not cost the operator the scopes that answered.
	ids := resourceIDs(got)
	for _, want := range []string{
		"github:acme/Secret/acme/api/repo/API_KEY",
		"github:acme/Secret/org/ORG_TOKEN",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("the refusal on one repository dropped %q, which was readable", want)
		}
	}
}

// TestTheSameFixtureWithNoRefusalIsComplete is the previous row's SAME-RUN KNOWN
// POSITIVE. Without it, an enumeration that reported itself incomplete on every
// input would satisfy the assertion above while proving nothing.
func TestTheSameFixtureWithNoRefusalIsComplete(t *testing.T) {
	repos, actions := fixtureAPI()

	got, err := runOneAllowingError(t, repos, actions, "github-secrets")
	if err != nil {
		t.Fatalf("the unmodified fixture reported %v; the refusal row above is then asserting "+
			"something that is true of every input", err)
	}
	if len(got.Resources) == 0 {
		t.Fatal("the clean read produced nothing, so its completeness says nothing")
	}
}

// TestAnEnvironmentTheProviderDoesNotReturnIsAPartialRead is the 404 arm.
//
// THE SOURCE PROVIDER SWALLOWS THIS ONE WITH NO LOG AT ALL on its environment-
// secrets path, so there is no pre-change behavior to mirror and no operator
// signal to preserve — only the wrong verdict to fix.
func TestAnEnvironmentTheProviderDoesNotReturnIsAPartialRead(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.environmentsErr = map[string]error{"acme/api": refusal(404, "Not Found")}

	for _, enumeration := range []string{"github-environments", "github-secrets"} {
		got, err := runOneAllowingError(t, repos, actions, enumeration)
		if err == nil {
			t.Errorf("%s: a 404 on the environments read produced a clean result", enumeration)
			continue
		}
		if !errors.Is(err, collect.ErrPartial) {
			t.Errorf("%s: a 404 was not classified as a partial read: %v", enumeration, err)
		}
		if !strings.Contains(err.Error(), "acme/api") {
			t.Errorf("%s: the reason does not name the repository: %v", enumeration, err)
		}
		// The other repository is still walked.
		if enumeration == "github-secrets" {
			if _, ok := resourceIDs(got)["github:acme/Secret/org/ORG_TOKEN"]; !ok {
				t.Error("github-secrets: the organization's secrets were lost to one repository's " +
					"environment failure")
			}
		}
	}
}

// TestAPartialPaginationKeepsThePageItAlreadyRead is the partial arm: a read
// whose FIRST page succeeded and whose second failed keeps the first.
//
// DISCARDING IT WOULD BE THE LARGER LOSS. An organization commonly answers one
// page and then rate-limits or drops a connection; throwing away what it did
// answer turns a small gap into a whole missing repository.
func TestAPartialPaginationKeepsThePageItAlreadyRead(t *testing.T) {
	repos, actions := fixtureAPI()
	// The first page of workflows answers; the second is refused.
	actions.workflows["acme/api"] = pages[gogithub.Workflow]{
		{{
			Name:  new("CI"),
			Path:  new(fixtureWorkflow),
			State: new("active"),
		}},
		{{
			Name:  new("Release"),
			Path:  new(".github/workflows/release.yml"),
			State: new("active"),
		}},
	}
	actions.workflowsFailAfter = map[string]int{"acme/api": 1}

	got, err := runOneAllowingError(t, repos, actions, "github-workflows")
	if err == nil {
		t.Fatal("a failure mid-pagination produced a clean read")
	}
	ids := resourceIDs(got)
	if _, ok := ids["github:acme/Workflow/acme/api/.github/workflows/ci.yml"]; !ok {
		t.Error("the page that was already read was discarded when the next one failed")
	}
	if _, ok := ids["github:acme/Workflow/acme/api/.github/workflows/release.yml"]; ok {
		t.Error("the page that failed was returned anyway; this row is not measuring a partial read")
	}
}

// TestAWorkflowDefinitionThatCannotBeReadIsAPartialRead is the decision this
// collector makes about the one read whose failure costs edges rather than nodes.
func TestAWorkflowDefinitionThatCannotBeReadIsAPartialRead(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.contentsErr = map[string]error{
		"acme/api/" + fixtureWorkflow: refusal(404, "Not Found"),
	}

	got, err := runOneAllowingError(t, repos, actions, "github-workflows")
	if err == nil {
		t.Fatal("a workflow definition that could not be read produced a clean result; the edges " +
			"its text declares are then missing with nothing saying so")
	}
	if !strings.Contains(err.Error(), fixtureWorkflow) {
		t.Errorf("the reason does not name the workflow whose definition was lost: %v", err)
	}

	// The workflow NODE is still emitted: it was enumerated, and only its text
	// was unreadable.
	if _, ok := resourceIDs(got)["github:acme/Workflow/acme/api/.github/workflows/ci.yml"]; !ok {
		t.Error("the workflow node was dropped along with its definition")
	}
	// And the edges its text declares are the thing that is gone.
	for _, rel := range got.Relations {
		if rel.Type == ghgraph.EdgeUsesSecret {
			t.Errorf("a USES_SECRET edge survived a definition that could not be read: %s -> %s",
				rel.FromID, rel.ToID)
		}
	}
}

// TestAFailureListingTheRepositoriesFailsTheWholeEnumeration is the arm that is
// NOT a partial read. Six enumerations start from that listing, so a walk that
// could not read it never found out what the organization contains.
func TestAFailureListingTheRepositoriesFailsTheWholeEnumeration(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.reposErr = refusal(500, "Server Error")

	for _, name := range enumerationNames(collect.API{Repos: repos, Actions: actions}) {
		got, err := runOneAllowingError(t, repos, actions, name)
		if err == nil {
			t.Errorf("%s survived a failure to list the organization's repositories", name)
			continue
		}
		if errors.Is(err, collect.ErrDenied) || errors.Is(err, collect.ErrPartial) {
			t.Errorf("%s reported a failure to list the repositories as a partial read: %v", name, err)
		}
		// NOTHING REPOSITORY-SCOPED SURVIVES, which is the observable half. The
		// ORGANIZATION-scoped reads are a different matter: two enumerations read
		// the organization's own runners and secrets before they ever ask for the
		// repository list, so what they had already read is still returned — and
		// the walk discards it anyway, because a hard error keeps none of an
		// enumeration's output.
		for _, res := range got.Resources {
			if res.ResourceType == ghgraph.ResourceTypeRepository || res.Metadata["repo"] != "" {
				t.Errorf("%s emitted the repository-scoped node %q from a walk that could not list "+
					"repositories", name, res.ID)
			}
		}
	}
}

// TestBothClassesAtOnceAreReportedAsBoth. An enumeration refused one repository
// and unable to reach another has two different things wrong with it, and an
// operator fixes them differently.
func TestBothClassesAtOnceAreReportedAsBoth(t *testing.T) {
	repos, actions := fixtureAPI()
	actions.repoSecretsErr = map[string]error{
		"acme/api": refusal(403, "Resource not accessible"),
		"acme/web": refusal(500, "Server Error"),
	}

	_, err := runOneAllowingError(t, repos, actions, "github-secrets")
	if err == nil {
		t.Fatal("two failing repositories produced a clean read")
	}
	if !errors.Is(err, collect.ErrDenied) {
		t.Errorf("the refusal was lost: %v", err)
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("the failure was lost: %v", err)
	}
	for _, want := range []string{"acme/api", "acme/web"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the reason does not name %q: %v", want, err)
		}
	}
}

// TestEveryPerScopeReadFailureReachesTheVerdict is the per-SITE row, and it is
// the one the rows above do not imply.
//
// THE AXIS IS THE CARRIERS, NOT THE OUTCOME KINDS. The rows above cover five
// OUTCOMES — refused, not-returned, partial, failed, empty — and each one happens
// to be driven through whichever enumeration was convenient. But the requirement
// constrains a STATE: the walk's completeness assertion must reflect EVERY read
// that failed. So the axis is every place a failure is recorded, and there are
// ten of them across seven enumerations. Measured on this tree before this row
// existed: five of the ten could have their record call replaced with `_ = err`
// and the whole module stayed green — including the deployments and runners
// reads, which are exactly the inventory the fix exists to stop the server
// deleting.
//
// EACH ARM ASSERTS THE VERDICT AND THE REASON. A walk that reported itself
// incomplete without naming what it missed sends an operator looking through
// seven enumerations for a repository it will not name.
func TestEveryPerScopeReadFailureReachesTheVerdict(t *testing.T) {
	for _, row := range []struct {
		name        string
		enumeration string
		// arrange installs one scope's failure on the recorded provider.
		arrange func(*fakeRepos, *fakeActions)
		// sentinel is the class this failure must be reported as.
		sentinel error
		// names is what the reason must carry: the enumeration and the scope.
		names []string
	}{
		{
			name:        "a repository's deployments are refused",
			enumeration: "github-deployments",
			arrange: func(repos *fakeRepos, _ *fakeActions) {
				repos.deploymentsErr = map[string]error{
					"acme/web": refusal(403, "Resource not accessible by personal access token"),
				}
			},
			sentinel: collect.ErrDenied,
			names:    []string{"github-deployments", "acme/web"},
		},
		{
			name:        "the organization's runners are refused",
			enumeration: "github-runners",
			arrange: func(_ *fakeRepos, actions *fakeActions) {
				actions.orgRunnersErr = refusal(403, "Must have admin rights to Repository")
			},
			sentinel: collect.ErrDenied,
			names:    []string{"github-runners", "the organization's runners"},
		},
		{
			name:        "a repository's runners are refused",
			enumeration: "github-runners",
			arrange: func(_ *fakeRepos, actions *fakeActions) {
				actions.repoRunnersErr = map[string]error{
					"acme/api": refusal(403, "Resource not accessible"),
				}
			},
			sentinel: collect.ErrDenied,
			names:    []string{"github-runners", "acme/api"},
		},
		{
			name:        "the organization's secrets are refused",
			enumeration: "github-secrets",
			arrange: func(_ *fakeRepos, actions *fakeActions) {
				actions.orgSecretsErr = refusal(403, "Resource not accessible")
			},
			sentinel: collect.ErrDenied,
			names:    []string{"github-secrets", "the organization's secrets"},
		},
		{
			name:        "a repository's workflow runs fail",
			enumeration: "github-workflow-runs",
			arrange: func(_ *fakeRepos, actions *fakeActions) {
				actions.runsErr = map[string]error{"acme/api": refusal(500, "Server Error")}
			},
			sentinel: collect.ErrPartial,
			names:    []string{"github-workflow-runs", "acme/api"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			repos, actions := fixtureAPI()
			row.arrange(repos, actions)

			got, err := runOneAllowingError(t, repos, actions, row.enumeration)
			if err == nil {
				t.Fatalf("%s reported a clean read. The walk then asserts it enumerated the whole "+
					"organization, and the receiving server treats what this read could not see "+
					"as deleted", row.enumeration)
			}
			if !errors.Is(err, row.sentinel) {
				t.Errorf("the failure was not classified as %v: %v", row.sentinel, err)
			}
			for _, want := range row.names {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the reason does not name %q: %v", want, err)
				}
			}
			// EVERYTHING ELSE THE ENUMERATION READ IS STILL RETURNED. A failure on
			// one scope may not cost the operator the scopes that answered, and an
			// enumeration that returned nothing would satisfy the assertions above
			// while being a far worse outcome.
			if len(got.Resources) == 0 && len(got.Relations) == 0 {
				t.Errorf("%s returned nothing at all; one scope's failure discarded every scope "+
					"that answered", row.enumeration)
			}
		})
	}
}

// TestTheSameFiveScopesAreCleanWithoutTheFailure is the same-run known positive
// for every arm above. Without it, an enumeration that reported itself incomplete
// on every input would satisfy all five.
func TestTheSameFiveScopesAreCleanWithoutTheFailure(t *testing.T) {
	repos, actions := fixtureAPI()
	for _, enumeration := range []string{
		"github-deployments", "github-runners", "github-secrets", "github-workflow-runs",
	} {
		got, err := runOneAllowingError(t, repos, actions, enumeration)
		if err != nil {
			t.Errorf("%s over the unmodified fixture reported %v; the arms above are then "+
				"asserting something true of every input", enumeration, err)
		}
		if len(got.Resources) == 0 {
			t.Errorf("%s read nothing over the unmodified fixture", enumeration)
		}
	}
}
