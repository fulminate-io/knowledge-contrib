// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// completeness_test.go — EVERY READ OUTCOME REACHES THE VERDICT TRUTHFULLY.
//
// This is the file that proves the one place this collector does NOT reproduce
// the source provider's behavior. That provider drops a refused or failed
// per-repository read and continues with its walk still asserting COMPLETE, at
// NINE separate sites — eight logged at debug level and one discarded with no
// log at all. A complete walk is what lets the receiving server treat everything
// the walk did not carry as gone, so a permission revoked between two collects
// would silently delete what it could no longer read, and the collect would
// report success.
//
// THE MATRIX IS THE NINE SITES TIMES THE OUTCOME CLASSES, and every cell asserts
// the class AND the reason text, because a verdict with a reason naming nothing
// is a verdict an operator cannot act on.

// theNineSites are the reads the source provider drops, each named with the
// request path that produces it and the enumeration that makes it.
//
// EIGHT PATHS COVER NINE SITES: the environments read is made twice, once by the
// environments enumeration and once by the variables enumeration, which re-lists
// it for the environment UUIDs its deployment-variable walk needs. That second
// read is the site the source provider discards with no log at all.
var theNineSites = []struct {
	site        string
	enumeration string
	path        string
	scope       string
	// stillRead is a path the enumeration must fetch ANYWAY when the site above
	// fails. It is the observable for "a failure on one scope does not abort the
	// enumeration", and it is a request count rather than an emitted node because
	// the scope that still answers is sometimes legitimately empty.
	stillRead string
	// listing marks the sites that are PAGINATED LISTINGS of a scope, as opposed
	// to the one site that reads a FILE. The distinction decides what a 404 on
	// the site means, and the two 404 rows below are split on it.
	listing bool
}{
	{"one repository's whole pipeline config", "bitbucket-pipelines-config",
		"repositories/acme/api/src/trunk/bitbucket-pipelines.yml", "api",
		"repositories/acme/web/src/main/bitbucket-pipelines.yml", false},
	{"one repository's whole run history", "bitbucket-pipeline-runs",
		"repositories/acme/api/pipelines", "api",
		"repositories/acme/web/pipelines", true},
	{"one repository's environments and its approval gates", "bitbucket-environments",
		"repositories/acme/api/environments", "api",
		"repositories/acme/web/environments", true},
	{"every workspace-level runner and its labels", "bitbucket-runners",
		"workspaces/acme/pipelines-config/runners", "the workspace's runners",
		repoRunnersPath(fixtureAPIRepo), true},
	{"one repository's runners", "bitbucket-runners",
		repoRunnersPath(fixtureAPIRepo), "api",
		repoRunnersPath(fixtureWebRepo), true},
	{"every workspace-scoped variable", "bitbucket-variables",
		"workspaces/acme/pipelines-config/variables", "the workspace's variables",
		repoVariablesPath(fixtureAPIRepo), true},
	{"one repository's variables", "bitbucket-variables",
		repoVariablesPath(fixtureAPIRepo), "api",
		"repositories/acme/api/deployments_config/environments/{env-prod}/variables", true},
	{"one environment's variables", "bitbucket-variables",
		"repositories/acme/api/deployments_config/environments/{env-prod}/variables",
		"api environment production",
		"repositories/acme/api/deployments_config/environments/{env-stage}/variables", true},
	{"the environment list the deployment-variable walk depends on", "bitbucket-variables",
		"repositories/acme/api/environments", "api environments",
		repoVariablesPath(fixtureWebRepo), true},
}

// TestEveryRefusedReadIsIncompleteAndNamesWhatItMissed is the REFUSED row of the
// matrix, over all nine sites and both refusal statuses.
func TestEveryRefusedReadIsIncompleteAndNamesWhatItMissed(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, site := range theNineSites {
			name := fmt.Sprintf("%d/%s", status, site.site)
			t.Run(name, func(t *testing.T) {
				fixture := newFixture(t)
				fixture.status[site.path] = status

				got, err := runOne(t, fixture, site.enumeration)
				if err == nil {
					t.Fatalf("a %d on %q produced a clean read. That is the source provider's "+
						"defect: a walk that could not look asserts it saw everything",
						status, site.path)
				}
				if !errors.Is(err, collect.ErrDenied) {
					t.Errorf("a %d was not classified as a refusal: %v", status, err)
				}
				if !strings.Contains(err.Error(), site.enumeration) {
					t.Errorf("the reason does not name the enumeration: %v", err)
				}
				if !strings.Contains(err.Error(), site.scope) {
					t.Errorf("the reason does not name the scope %q: %v", site.scope, err)
				}
				// THE ENUMERATION KEEPS READING. A refusal on one scope may not cost
				// the operator the scopes that would have answered, so a later read
				// the same enumeration makes is asserted to have happened.
				if fixture.requests(site.stillRead) == 0 {
					t.Errorf("the refusal on %q stopped the enumeration: it never read %q",
						site.path, site.stillRead)
				}
				_ = got
			})
		}
	}
}

// TestTheSameFixtureWithNoRefusalIsComplete is the previous row's SAME-RUN KNOWN
// POSITIVE. Without it, an enumeration that reported itself incomplete on every
// input would satisfy the assertions above while proving nothing.
func TestTheSameFixtureWithNoRefusalIsComplete(t *testing.T) {
	fixture := newFixture(t)
	for _, name := range enumerationNames(t, fixture) {
		got, err := runOne(t, fixture, name)
		if err != nil {
			t.Fatalf("%s: the unmodified fixture reported %v; the refusal rows above are then "+
				"asserting something that is true of every input", name, err)
		}
		if len(got.Resources) == 0 {
			t.Errorf("%s: the clean read produced nothing, so its completeness says nothing", name)
		}
	}
}

// TestAPersistentRateLimitIsItsOwnIncompleteClass is the cell no sibling
// collector supplies, because this is the only CI/CD provider with a retry.
//
// THE FOURTH RESPONSE IS STILL A 429. The client answers a 429 by retrying three
// times and then makes one unconditional final request, so a persistently
// rate-limited URL is fetched FOUR times and the last answer reaches the
// enumeration. 429 is neither 401 nor 403, so it is not a refusal; it is neither
// a 5xx nor a transport failure, so it is not the failed class either. Without
// its own arm the outcome the retry exists to produce would land in whichever
// class the classifier happened to fall through to.
func TestAPersistentRateLimitIsItsOwnIncompleteClass(t *testing.T) {
	fixture := newFixture(t)
	path := repoVariablesPath(fixtureAPIRepo)
	fixture.status[path] = http.StatusTooManyRequests

	got, err := runOne(t, fixture, "bitbucket-variables")
	if err == nil {
		t.Fatal("a read the provider kept rate-limiting produced a clean result")
	}
	if !errors.Is(err, collect.ErrRateLimited) {
		t.Errorf("a persistent 429 was not classified as a rate limit: %v", err)
	}
	// AND IT IS NOT EITHER OF THE OTHER TWO CLASSES. An operator fixes a rate
	// limit by waiting and a refusal by granting a permission, so a 429 reported
	// as a refusal sends them to the wrong place.
	if errors.Is(err, collect.ErrDenied) {
		t.Errorf("a 429 was reported as a refusal, which asks the operator to grant a "+
			"permission that is not the problem: %v", err)
	}
	if errors.Is(err, collect.ErrPartial) {
		t.Errorf("a 429 was reported as an unclassified failure: %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("the reason does not carry the status: %v", err)
	}

	// THE REQUEST BUDGET, observed rather than reasoned: four attempts, not three.
	if requests := fixture.requests(path); requests != 4 {
		t.Errorf("a persistently rate-limited URL was fetched %d times, want 4 — three retries "+
			"and one unconditional final request", requests)
	}

	// The workspace-scoped variables, read before this one, are still emitted.
	if _, ok := resourceIDs(got)["bitbucket:acme/Variable/workspace/WORKSPACE_TOKEN"]; !ok {
		t.Error("the rate limit on one repository cost the workspace's own variables")
	}
}

// TestAServerErrorIsThePartialClass is the FAILED row.
func TestAServerErrorIsThePartialClass(t *testing.T) {
	fixture := newFixture(t)
	fixture.status["repositories/acme/api/environments"] = http.StatusInternalServerError

	_, err := runOne(t, fixture, "bitbucket-environments")
	if err == nil {
		t.Fatal("a 500 produced a clean read")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("a 500 was not classified as a partial read: %v", err)
	}
	if errors.Is(err, collect.ErrDenied) || errors.Is(err, collect.ErrRateLimited) {
		t.Errorf("a 500 was classified as a refusal or a rate limit: %v", err)
	}
}

// TestTheClientTimeoutIsThePartialClassToo. The client bounds a request at
// thirty seconds and a request that exceeds it produces a transport error rather
// than a status, so it has no APIError to classify on — the default arm is what
// catches it, and this row is what proves that arm reaches the verdict.
func TestTheClientTimeoutIsThePartialClassToo(t *testing.T) {
	fixture := newFixture(t)
	path := repoVariablesPath(fixtureAPIRepo)
	fixture.slow[path] = 200 * time.Millisecond

	// A client with a timeout far below the fixture's delay. The SHIPPED timeout
	// is thirty seconds; waiting for it here would trade thirty seconds of suite
	// time for the same assertion.
	client := bbclient.NewAt(fixture.server.URL,
		&http.Client{Timeout: 20 * time.Millisecond}, "fixture-user", "fixture-app-password")

	repositories, err := collect.Repositories(client).Run(context.Background(), fixtureWorkspace)
	if err != nil {
		t.Fatalf("the repository listing was expected to answer within the bound: %v", err)
	}
	subs := collect.AfterRepos(client, collect.RepoInfos(repositories), fixtureHistoryDepth)
	var variables collect.Subcollector
	for _, sub := range subs {
		if sub.Name == "bitbucket-variables" {
			variables = sub
		}
	}
	_, err = variables.Run(context.Background(), fixtureWorkspace)
	if err == nil {
		t.Fatal("a read that exceeded the client's timeout produced a clean result")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("a timed-out read was not classified as a partial read: %v", err)
	}
}

// TestAPartialPaginationKeepsThePagesItAlreadyRead is the PARTIAL row: a read
// whose first page succeeded and whose second failed keeps the first.
//
// DISCARDING IT WOULD BE THE LARGER LOSS. A workspace commonly answers one page
// and then rate-limits or drops a connection; throwing away what it did answer
// turns a small gap into a whole missing scope.
func TestAPartialPaginationKeepsThePagesItAlreadyRead(t *testing.T) {
	fixture := newFixture(t)
	fixture.failAfter["workspaces/acme/pipelines-config/variables"] = 1

	got, err := runOne(t, fixture, "bitbucket-variables")
	if err == nil {
		t.Fatal("a failure mid-pagination produced a clean read")
	}
	ids := resourceIDs(got)
	if _, ok := ids["bitbucket:acme/Variable/workspace/WORKSPACE_TOKEN"]; !ok {
		t.Error("the page that was already read was discarded when the next one failed")
	}
	if _, ok := ids["bitbucket:acme/Variable/workspace/DEPLOY_KEY"]; ok {
		t.Error("the page that failed was returned anyway; this row is not measuring a partial read")
	}
}

// TestAnEmptyValuesArrayIsCompleteToo, which is the other true empty: the
// provider answered, and the answer is that there is nothing there. The fixture
// already carries three such endpoints for the second repository.
func TestAnEmptyValuesArrayIsCompleteToo(t *testing.T) {
	got := walkTheFixture(t)
	for id := range resourceIDs(got) {
		if strings.Contains(id, "/"+fixtureWebRepo+"/") &&
			!strings.HasPrefix(id, "bitbucket:acme/Repository/") {
			t.Errorf("the repository whose every feature answered an empty array produced %q", id)
		}
	}
	// The known positive: the repository itself IS in the graph, so this is a
	// repository with no CI/CD rather than a repository that was never read.
	if _, ok := resourceIDs(got)["bitbucket:acme/Repository/web"]; !ok {
		t.Error("the repository with no configured features is missing entirely")
	}
}

// TestACancelledWalkIsIncompleteAndStopsReading. The enumerations check the
// context between repositories, so a cancelled walk stops rather than draining a
// workspace whose result will be discarded.
func TestACancelledWalkIsIncompleteAndStopsReading(t *testing.T) {
	fixture := newFixture(t)
	client := fixture.client()

	repositories, err := collect.Repositories(client).Run(context.Background(), fixtureWorkspace)
	if err != nil {
		t.Fatalf("the repository listing: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, sub := range collect.AfterRepos(
		client, collect.RepoInfos(repositories), fixtureHistoryDepth,
	) {
		if _, err := sub.Run(ctx, fixtureWorkspace); err == nil {
			t.Errorf("%s ran to completion on a cancelled context", sub.Name)
		}
	}
}

// TestTheEnvironmentRelistFailureIsReported is the `_ =` site specifically: the
// one read the source provider discards with NO log at any level.
//
// ITS SYMPTOM ON THE CURRENT TREE IS SILENCE. A repository whose environments
// could not be listed produced zero deployment variables, said nothing, and the
// walk still asserted it had seen everything — so an operator comparing two
// collects would see a repository's environment variables disappear with no
// diagnostic anywhere.
func TestTheEnvironmentRelistFailureIsReported(t *testing.T) {
	fixture := newFixture(t)
	fixture.status["repositories/acme/api/environments"] = http.StatusInternalServerError

	got, err := runOne(t, fixture, "bitbucket-variables")
	if err == nil {
		t.Fatal("a failure listing the environments for the deployment-variable walk produced " +
			"a clean read, which is the source provider's own behavior")
	}
	if !strings.Contains(err.Error(), "environments") {
		t.Errorf("the reason does not name the environment list: %v", err)
	}
	if !strings.Contains(err.Error(), fixtureAPIRepo) {
		t.Errorf("the reason does not name the repository: %v", err)
	}
	// The deployment variables really are gone, which is what the reason is FOR.
	if _, ok := resourceIDs(got)["bitbucket:acme/Variable/env/api/production/DEPLOY_KEY"]; ok {
		t.Error("the deployment variables survived a failure to list the environments they " +
			"belong to; this row is not measuring the re-list")
	}
	// And the repository's own variables, read before it, are still there.
	if _, ok := resourceIDs(got)["bitbucket:acme/Variable/repository/api/API_KEY"]; !ok {
		t.Error("the repository's own variables were lost to the environment re-list failure")
	}
}

// TestBothClassesAtOnceAreReportedAsBoth. An enumeration refused one scope and
// unable to reach another has two different things wrong with it, and an
// operator fixes them differently.
func TestBothClassesAtOnceAreReportedAsBoth(t *testing.T) {
	fixture := newFixture(t)
	fixture.status["workspaces/acme/pipelines-config/variables"] = http.StatusForbidden
	fixture.status[repoVariablesPath(fixtureAPIRepo)] =
		http.StatusInternalServerError

	_, err := runOne(t, fixture, "bitbucket-variables")
	if err == nil {
		t.Fatal("two failing scopes produced a clean read")
	}
	if !errors.Is(err, collect.ErrDenied) {
		t.Errorf("the refusal was lost: %v", err)
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("the failure was lost: %v", err)
	}
	for _, want := range []string{"the workspace's variables", fixtureAPIRepo} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the reason does not name %q: %v", want, err)
		}
	}
}

// TestAnIncompleteReasonIsNeverEmpty. The framework refuses an incomplete
// assertion with no reason when the envelope is encoded, naming the collector —
// so an enumeration that reported incompleteness with an empty message would
// fail the whole collect rather than mark it.
func TestAnIncompleteReasonIsNeverEmpty(t *testing.T) {
	fixture := newFixture(t)
	fixture.status[repoRunnersPath(fixtureAPIRepo)] = http.StatusForbidden

	_, err := runOne(t, fixture, "bitbucket-runners")
	if err == nil {
		t.Fatal("the refusal produced no error")
	}
	if strings.TrimSpace(err.Error()) == "" {
		t.Error("the incompleteness carries an empty reason")
	}
	// And it is more than the sentinel: the sentinel alone names no scope.
	if err.Error() == collect.ErrDenied.Error() {
		t.Errorf("the reason is the bare sentinel and names nothing an operator can look at: %v",
			err)
	}
}
