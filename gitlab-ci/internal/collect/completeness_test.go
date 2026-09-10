// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// completeness_test.go — EVERY READ OUTCOME REACHES THE VERDICT TRUTHFULLY.
//
// This is the file that proves the one place this collector does NOT reproduce
// the source provider's behavior. That provider logs a refused or failed
// per-project read and continues with its walk still asserting COMPLETE, at
// sixteen separate sites — two of them at DEBUG, which is invisible at default
// verbosity, so the work they dropped is reported nowhere at all. A complete walk
// is what lets the receiving server treat everything the walk did not carry as
// gone, so a token that lost a membership between two collects would silently
// delete what it could no longer read, and the collect would report success.
//
// Six outcomes, and each row asserts BOTH the verdict and the reason text.

// providerRefusalMessage is the message the recorded refusal carries. It is a
// distinctive string so an assertion that it reached the reason cannot pass on
// some other part of the text.
const providerRefusalMessage = "insufficient_scope"

// TestARefusedVariablesReadIsIncompleteAndNamesWhatItMissed is the row the
// completeness fix rests on: the exact input class where the source provider is
// wrong.
func TestARefusedVariablesReadIsIncompleteAndNamesWhatItMissed(t *testing.T) {
	api := fixtureAPI()
	api.projectVarErr = map[int64]error{
		apiID: refusal(http.StatusForbidden, providerRefusalMessage),
	}

	got, err := runOneAllowingError(t, api, "gitlab-variables")
	if err == nil {
		t.Fatal("a 403 on one project's variables produced a clean read. That is the source " +
			"provider's defect: a walk that could not look asserts it saw everything")
	}
	if !errors.Is(err, collect.ErrDenied) {
		t.Errorf("a 403 was not classified as a refusal: %v", err)
	}
	for _, want := range []string{"gitlab-variables", apiPath} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the reason does not name %q: %v", want, err)
		}
	}

	// THE PROVIDER'S OWN MESSAGE REACHES THE REASON, and this is the assertion
	// that keeps the whole suite honest about reason TEXT rather than only about
	// verdicts. The SDK's error renders the request and its own message; an error
	// fixture whose request is incomplete makes that rendering PANIC, fmt recovers,
	// and every reason a test reads carries a marker where the message belongs —
	// so a regression in how this module wraps the provider's error would be
	// invisible behind it.
	if !strings.Contains(err.Error(), providerRefusalMessage) {
		t.Errorf("the reason does not carry the provider's own message %q: %v",
			providerRefusalMessage, err)
	}
	if strings.Contains(err.Error(), "PANIC=") {
		t.Errorf("the reason carries a panic marker where the provider's message belongs, so no "+
			"test here has seen what an operator will read: %v", err)
	}

	// THE GROUP'S OWN VARIABLES ARE STILL EMITTED. A refusal on one scope may not
	// cost the operator the scopes that answered.
	if _, ok := resourceIDs(got)["gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN"]; !ok {
		t.Error("the refusal on one project dropped the group's own variables, which were readable")
	}
}

// TestTheSameFixtureWithNoRefusalIsComplete is the previous row's SAME-RUN KNOWN
// POSITIVE, and the baseline every other row here is read against. Without it, a
// collector that reported itself incomplete on every input would satisfy the
// assertions above while proving nothing.
func TestTheSameFixtureWithNoRefusalIsComplete(t *testing.T) {
	got, errs, listerPartial := runAll(t, fixtureAPI())

	for name, err := range errs {
		if err != nil {
			t.Errorf("the unmodified fixture made %s report %v; every refusal row is then "+
				"asserting something true of every input", name, err)
		}
	}
	if len(listerPartial) != 0 {
		t.Errorf("the unmodified fixture's project discovery reported %v", listerPartial)
	}
	if len(got.Resources) == 0 {
		t.Fatal("the clean read produced nothing, so its completeness says nothing")
	}
}

// TestAnEnvironmentsReadTheProviderDoesNotAnswerIsAPartialRead is the 404 arm.
func TestAnEnvironmentsReadTheProviderDoesNotAnswerIsAPartialRead(t *testing.T) {
	api := fixtureAPI()
	api.environmentsErr = map[int64]error{apiID: refusal(http.StatusNotFound, "404 Not Found")}

	got, err := runOneAllowingError(t, api, "gitlab-environments")
	if err == nil {
		t.Fatal("a 404 on the environments read produced a clean result")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("a 404 was not classified as a partial read: %v", err)
	}
	if !strings.Contains(err.Error(), apiPath) {
		t.Errorf("the reason does not name the project: %v", err)
	}
	// The protection rules of the same project were a SEPARATE call and still
	// answered, so they are still emitted.
	if _, ok := resourceIDs(got)["gitlab:acme/ProtectionRule/acme/api/production"]; !ok {
		t.Error("the environments failure took the protection rules with it; the two are " +
			"different calls and only one of them failed")
	}
}

// TestAPartialPaginationKeepsThePageItAlreadyRead is the partial arm: a read
// whose FIRST page succeeded and whose second failed keeps the first.
//
// DISCARDING IT WOULD BE THE LARGER LOSS. A group commonly answers one page and
// then rate-limits or drops a connection; throwing away what it did answer turns
// a small gap into a whole missing project.
func TestAPartialPaginationKeepsThePageItAlreadyRead(t *testing.T) {
	api := fixtureAPI()
	api.environments[apiID] = pages[gl.Environment]{
		{{ID: 10, Name: "production", State: "available"}},
		{{ID: 12, Name: "review", State: "available"}},
	}
	api.failAfterPage = map[string]int64{fmt.Sprintf("environments:%d", apiID): 1}

	got, err := runOneAllowingError(t, api, "gitlab-environments")
	if err == nil {
		t.Fatal("a failure mid-pagination produced a clean read")
	}
	ids := resourceIDs(got)
	if _, ok := ids["gitlab:acme/Environment/acme/api/production"]; !ok {
		t.Error("the page that was already read was discarded when the next one failed")
	}
	if _, ok := ids["gitlab:acme/Environment/acme/api/review"]; ok {
		t.Error("the page that failed was returned anyway; this row is not measuring a partial read")
	}
}

// TestAPipelineDefinitionThatDoesNotParseKeepsItsNodeAndLosesItsEdges is the
// decision this collector makes about the one read whose failure costs edges
// rather than nodes.
//
// THE SOURCE PROVIDER DROPS THE NODE TOO, so a project with a broken definition
// looks there like a project with none. Here the node is kept — the file was read
// and it exists — its two derived metadata keys are OMITTED rather than written
// as zero, and the walk says what it could not understand.
func TestAPipelineDefinitionThatDoesNotParseKeepsItsNodeAndLosesItsEdges(t *testing.T) {
	api := fixtureAPI()
	api.files["1/.gitlab-ci.yml"] = encodedFile("build:\n  script:\n   - echo\n\t- tab is not YAML\n")

	got, err := runOneAllowingError(t, api, "gitlab-pipelines")
	if err == nil {
		t.Fatal("a definition that does not parse produced a clean result; the edges its text " +
			"declares are then missing with nothing saying so")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("an unparseable definition was not classified as a partial read: %v", err)
	}
	if !strings.Contains(err.Error(), apiPath) {
		t.Errorf("the reason does not name the project whose definition was lost: %v", err)
	}

	const pipeline = "gitlab:acme/Pipeline/acme/api/main"
	res, ok := resourceByID(got, pipeline)
	if !ok {
		t.Fatalf("the pipeline node %q was dropped along with its parse", pipeline)
	}
	if res.Content == "" {
		t.Error("the pipeline node carries no content; the document was read and it is what the " +
			"node is for")
	}
	for _, key := range []string{"job_count", "has_stages"} {
		if _, present := res.Metadata[key]; present {
			t.Errorf("the pipeline node declares %q from a document that did not parse; this "+
				"collector did not learn it and a zero would read as a fact", key)
		}
	}
	for _, rel := range got.Relations {
		if rel.FromID == pipeline && rel.Type != glgraph.EdgeBelongsTo {
			t.Errorf("a %s edge survived a definition that did not parse: %s -> %s",
				rel.Type, rel.FromID, rel.ToID)
		}
	}
}

// TestAProjectWithNoPipelineDefinitionIsNotAnIncompleteness is the arm that must
// NOT count against the walk. The provider answered, and the answer is that the
// file is not there.
func TestAProjectWithNoPipelineDefinitionIsNotAnIncompleteness(t *testing.T) {
	got, err := runOneAllowingError(t, fixtureAPI(), "gitlab-pipelines")
	if err != nil {
		t.Fatalf("the fixture has three projects with no .gitlab-ci.yml and the enumeration "+
			"reported %v; a file that is not there is not a read that failed", err)
	}
	// The known positive: the two projects that DO have one produced their nodes,
	// so this is an enumeration that read something rather than one that read
	// nothing and reported no error.
	ids := resourceIDs(got)
	for _, want := range []string{
		"gitlab:acme/Pipeline/acme/api/main",
		"gitlab:acme/Pipeline/acme/legacy/master",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("the pipeline node %q is missing, so the clean verdict above says nothing", want)
		}
	}
}
