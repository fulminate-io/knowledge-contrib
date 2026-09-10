// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/walk"
)

// walk_test.go — the walk's own decisions: the completeness verdict it reports,
// the one case that is an error rather than an incomplete success, the shared
// discovery's gaps reaching the verdict once, and the foreign-context block it
// declares nothing of.

// TestACleanWalkAssertsComplete is the baseline every other row is read against.
func TestACleanWalkAssertsComplete(t *testing.T) {
	got := mustWalk(t, &recordingAPI{}, fixtureGroup)

	if !got.Complete.IsAsserted() {
		t.Fatal("the walk returned a zero completeness value, which asserts nothing")
	}
	if !got.Complete.IsComplete() {
		t.Errorf("a clean walk reported itself incomplete: %s", got.Complete.Reason())
	}
	// The known positive: it found the group, so this is a walk that completed
	// rather than one that did nothing.
	if len(got.Nodes) == 0 {
		t.Fatal("the walk produced no nodes at all")
	}
}

// TestARefusedReadMakesTheWalkIncompleteAndNamesIt is the completeness fix seen
// from the walk, which is where the assertion reaches the wire.
func TestARefusedReadMakesTheWalkIncompleteAndNamesIt(t *testing.T) {
	api := &recordingAPI{variablesFailure: refusal(http.StatusForbidden, "403 Forbidden")}
	got := mustWalk(t, api, fixtureGroup)

	if got.Complete.IsComplete() {
		t.Fatal("a walk that was refused one project's variables asserted it saw the whole group. " +
			"That assertion is what lets the server delete what the walk could not read")
	}
	reason := got.Complete.Reason()
	for _, want := range []string{fixtureGroup, "gitlab-variables", fixtureProject} {
		if !strings.Contains(reason, want) {
			t.Errorf("the incompleteness reason does not name %q: %s", want, reason)
		}
	}
	// Everything the other enumerations found is still returned.
	if len(got.Nodes) == 0 {
		t.Error("a refusal on one read cost the walk everything it did read")
	}
}

// TestTheSharedDiscoverysGapsReachTheVerdictExactlyOnce.
//
// Six of the seven enumerations read one project discovery, so a subgroup it
// could not list belongs to none of them in particular. The walk folds it in
// once; six copies of the same sentence would read like six failures.
func TestTheSharedDiscoverysGapsReachTheVerdictExactlyOnce(t *testing.T) {
	api := &recordingAPI{subgroupProjectsFailure: refusal(http.StatusForbidden, "403 Forbidden")}
	got := mustWalk(t, api, fixtureGroup)

	if got.Complete.IsComplete() {
		t.Fatal("a subgroup this collector could not list left the walk asserting it saw the " +
			"whole group")
	}
	reason := got.Complete.Reason()
	if n := strings.Count(reason, fixtureSubgroup); n != 1 {
		t.Errorf("the subgroup is named %d times in the reason, want exactly 1:\n%s", n, reason)
	}
	// The known positive: the group's own project was still walked, so this is a
	// partial discovery rather than a failed one.
	if len(got.Nodes) == 0 {
		t.Error("one unreadable subgroup cost the walk the group's own projects")
	}
}

// TestEveryEnumerationFailingIsAnErrorRatherThanAnIncompleteSuccess. A walk that
// learned nothing is not a partial answer: reporting it as an incomplete success
// would land an empty generation the server then reconciles against.
func TestEveryEnumerationFailingIsAnErrorRatherThanAnIncompleteSuccess(t *testing.T) {
	api := &recordingAPI{failure: refusal(http.StatusInternalServerError, "500 Server Error")}
	collector := walk.Collector{API: api.build}

	_, err := collector.Walk(context.Background(), fixtureGroup, walk.Params{},
		framework.ForeignContext{})
	if err == nil {
		t.Fatal("a walk whose every enumeration failed returned a successful result")
	}
	if !strings.Contains(err.Error(), fixtureGroup) {
		t.Errorf("the error does not name the group: %v", err)
	}
}

// TestANonEmptyForeignContextChangesNoByteOfTheOutput is the pin on the declared
// foreign-graph context.
//
// This collector's registration declares none, so the block is always the zero
// value. Wiring it in without also declaring it in the entry would be a graph that
// silently depended on data the entry never asked for, so the consequence is
// pinned rather than described: handed a block, the walk produces the same bytes.
func TestANonEmptyForeignContextChangesNoByteOfTheOutput(t *testing.T) {
	without := mustWalk(t, &recordingAPI{}, fixtureGroup)

	collector := walk.Collector{API: (&recordingAPI{}).build}
	with, err := collector.Walk(context.Background(), fixtureGroup, walk.Params{},
		framework.ForeignContext{
			"code": {{
				GraphName: "api",
				Nodes: []framework.ForeignNode{
					{ID: "deploy/Chart.yaml", FilePath: "deploy/Chart.yaml", Content: "name: api"},
				},
			}},
			"acme-aws": {{GraphName: "prod-aws"}},
		})
	if err != nil {
		t.Fatalf("walking with a context block: %v", err)
	}

	if a, b := render(t, without), render(t, with); a != b {
		t.Errorf("a non-empty context block changed the output:\n%s\nvs\n%s", a, b)
	}
	// The known positive: the walk really produced something, so this is a walk
	// that ignored data rather than one handed nothing.
	if len(without.Nodes) == 0 {
		t.Fatal("both walks produced nothing, so their equality says nothing")
	}
}

// TestTheWalkCarriesNoGraphIdentity. The graph a registered family writes into is
// the collect id itself; a name derived here would be a second definition of it,
// and the source provider's own graph-name function warns against exactly that.
func TestTheWalkCarriesNoGraphIdentity(t *testing.T) {
	got := mustWalk(t, &recordingAPI{}, fixtureGroup)

	rendered := render(t, got)
	if strings.Contains(rendered, "gitlab-"+fixtureGroup) {
		t.Errorf("the result carries the source provider's derived graph name; this module "+
			"neither prefixes nor rewrites the collect id:\n%s", rendered)
	}
	// And the id reaches the node ids verbatim, which is the known positive: the
	// group really was the one walked.
	if !strings.Contains(rendered, "gitlab:acme/Group/acme") {
		t.Errorf("the result does not carry the group node built from the collect id:\n%s", rendered)
	}
}

// TestTheToolIsNamedAndDescribed. An operator installing this collector reads the
// description, and the entry's `tool` field must match the name it serves.
func TestTheToolIsNamedAndDescribed(t *testing.T) {
	spec := walk.Collector{}.Tool()
	if spec.Name != "" {
		t.Errorf("the collector overrides the tool name with %q; it serves the framework's "+
			"default, which is what its own config entry names", spec.Name)
	}
	if spec.Description == "" {
		t.Error("the served tool carries no description")
	}
	for _, want := range []string{"GitLab", "group"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("the description does not mention %q: %q", want, spec.Description)
		}
	}
}

// TestAProviderThatCannotBeBuiltFailsTheCollectWithItsCause.
//
// THE ONE PACKAGE THAT HOLDS A CREDENTIAL IS ALSO THE ONE THAT CAN REFUSE BEFORE
// A WALK STARTS: no token, a token present and empty, an instance selector
// carrying userinfo, a selector that is not a URL. Each of those is a refusal an
// operator has to act on, and each reaches this walk as one error from one call.
//
// TURNING IT INTO A SUCCESSFUL EMPTY WALK IS THE FAILURE THIS PINS. The framework
// would still refuse the envelope, because a zero completeness asserts nothing —
// but that refusal names neither the credential nor the cause, so an operator
// whose token expired would be told the collector returned something malformed.
// The CAUSE has to survive, which is why this asserts on the sentinel rather than
// only on the presence of an error.
func TestAProviderThatCannotBeBuiltFailsTheCollectWithItsCause(t *testing.T) {
	cause := errors.New("no GitLab token: this collector reads two names and neither is set")
	api := &recordingAPI{buildFailure: cause}

	got, err := walk.Collector{API: api.build}.Walk(
		context.Background(), fixtureGroup, walk.Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatalf("a provider that could not be built produced a successful walk of %d node(s)",
			len(got.Nodes))
	}
	if !errors.Is(err, cause) {
		t.Errorf("the walk failed with %v, which does not carry the cause the provider reported; "+
			"an operator whose credential is the problem would be told something else", err)
	}
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Errorf("the failed walk still produced %d node(s) and %d edge(s)",
			len(got.Nodes), len(got.Edges))
	}
	// AND IT ASSERTS NOTHING ABOUT COMPLETENESS, which is what makes it an error
	// rather than an incomplete success: a walk that never dialed did not see PART
	// of the group.
	if got.Complete.IsAsserted() {
		t.Errorf("a failed walk carried a completeness assertion (%v): it enumerated nothing, so "+
			"there is nothing to assert about", got.Complete.IsComplete())
	}
	// The known positive: the builder really was reached, so this is a walk that
	// asked for a provider and was refused rather than one refused earlier.
	if !api.wasBuilt() {
		t.Error("the provider was never asked for; this row is measuring an earlier refusal")
	}
}

// TestAWalkWithNoAPIConfiguredIsRefused. A collector value built without its one
// required field would otherwise dereference nothing and look like an empty group.
func TestAWalkWithNoAPIConfiguredIsRefused(t *testing.T) {
	_, err := walk.Collector{}.Walk(context.Background(), fixtureGroup, walk.Params{},
		framework.ForeignContext{})
	if err == nil {
		t.Fatal("a collector with no API was walked")
	}
}

// TestTheDefaultCapsReachTheRequest. A cap that never left the parameter object
// would be one round trip per item with the graph looking identical.
func TestTheDefaultCapsReachTheRequest(t *testing.T) {
	api := &recordingAPI{}
	mustWalk(t, api, fixtureGroup)

	api.mu.Lock()
	runs, deployments := api.runsPerPage, api.deploymentsPerPage
	api.mu.Unlock()
	if runs != 20 {
		t.Errorf("the runs listing asked for %d per page, want the default of 20", runs)
	}
	if deployments != 20 {
		t.Errorf("the deployments listing asked for %d per page, want the default of 20", deployments)
	}
}

// mustWalk runs one walk with this collector's default parameters and fails on
// error. The rows that drive the two caps set them explicitly and go through
// Walk directly, because what they assert is the REQUEST the walk made rather
// than the result it returned.
func mustWalk(t *testing.T, api *recordingAPI, id string) framework.Result {
	t.Helper()
	got, err := walk.Collector{API: api.build}.Walk(
		context.Background(), id, walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walking %q: %v", id, err)
	}
	return got
}

// render is a walk's nodes and edges as the bytes the envelope carries.
func render(t *testing.T, got framework.Result) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"nodes": got.Nodes, "edges": got.Edges})
	if err != nil {
		t.Fatalf("rendering a walk result: %v", err)
	}
	return string(raw)
}
