// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/walk"
)

// walk_test.go — the walk's own decisions: the completeness verdict it reports,
// the one case that is an error rather than an incomplete success, and the
// foreign-context block it declares nothing of.

// TestACleanWalkAssertsComplete is the baseline every other row is read against.
func TestACleanWalkAssertsComplete(t *testing.T) {
	got := mustWalk(t, &recordingAPI{}, "acme", walk.Params{})

	if !got.Complete.IsAsserted() {
		t.Fatal("the walk returned a zero completeness value, which asserts nothing")
	}
	if !got.Complete.IsComplete() {
		t.Errorf("a clean walk reported itself incomplete: %s", got.Complete.Reason())
	}
	// The known positive: it found the organization, so this is a walk that
	// completed rather than one that did nothing.
	if len(got.Nodes) == 0 {
		t.Fatal("the walk produced no nodes at all")
	}
}

// TestARefusedReadMakesTheWalkIncompleteAndNamesIt is the completeness fix seen
// from the walk, which is where the assertion reaches the wire.
func TestARefusedReadMakesTheWalkIncompleteAndNamesIt(t *testing.T) {
	api := &recordingAPI{secretsFailure: refusal(403, "Resource not accessible")}
	got := mustWalk(t, api, "acme", walk.Params{})

	if got.Complete.IsComplete() {
		t.Fatal("a walk that was refused one repository's secrets asserted it saw the whole " +
			"organization. That assertion is what lets the server delete what the walk could " +
			"not read")
	}
	reason := got.Complete.Reason()
	for _, want := range []string{"acme", "github-secrets", "acme/api"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the incompleteness reason does not name %q: %s", want, reason)
		}
	}
	// Everything the other enumerations found is still returned.
	if len(got.Nodes) == 0 {
		t.Error("a refusal on one read cost the walk everything it did read")
	}
}

// TestEveryEnumerationFailingIsAnErrorRatherThanAnIncompleteSuccess. A walk that
// learned nothing is not a partial answer: reporting it as an incomplete success
// would land an empty generation the server then reconciles against.
func TestEveryEnumerationFailingIsAnErrorRatherThanAnIncompleteSuccess(t *testing.T) {
	api := &recordingAPI{failure: refusal(500, "Server Error")}
	collector := walk.Collector{API: api.build}

	_, err := collector.Walk(context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a walk whose every enumeration failed returned a successful result")
	}
	if !strings.Contains(err.Error(), "acme") {
		t.Errorf("the error does not name the organization: %v", err)
	}
}

// TestANonEmptyForeignContextChangesNoByteOfTheOutput is the pin on SEAM 6.
//
// This collector's registration declares no foreign-graph context, so the block
// is always the zero value. Wiring it in without also declaring it in the entry
// would be a graph that silently depended on data the entry never asked for, so
// the consequence is pinned rather than described: handed a block, the walk
// produces the same bytes.
func TestANonEmptyForeignContextChangesNoByteOfTheOutput(t *testing.T) {
	without := mustWalk(t, &recordingAPI{}, "acme", walk.Params{})

	collector := walk.Collector{API: (&recordingAPI{}).build}
	with, err := collector.Walk(context.Background(), "acme", walk.Params{}, framework.ForeignContext{
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
	// The known positive: the block really was non-empty, so this is a walk that
	// ignored data rather than one handed nothing.
	if len(without.Nodes) == 0 {
		t.Fatal("both walks produced nothing, so their equality says nothing")
	}
}

// TestTheToolIsNamedAndDescribed. An operator installing this collector reads the
// description, and the entry's `tool` field must match the name it serves.
func TestTheToolIsNamedAndDescribed(t *testing.T) {
	spec := walk.Collector{}.Tool()
	if spec.Name != "" {
		t.Errorf("the collector overrides the tool name with %q; it serves the framework's default, "+
			"which is what its own config entry names", spec.Name)
	}
	if spec.Description == "" {
		t.Error("the served tool carries no description")
	}
	for _, want := range []string{"GitHub", "organization"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("the description does not mention %q: %q", want, spec.Description)
		}
	}
}

// TestAWalkWithNoAPIConfiguredIsRefused. A collector value built without its one
// required field would otherwise dereference nothing and look like an empty
// organization.
func TestAWalkWithNoAPIConfiguredIsRefused(t *testing.T) {
	_, err := walk.Collector{}.Walk(context.Background(), "acme", walk.Params{},
		framework.ForeignContext{})
	if err == nil {
		t.Fatal("a collector with no API was walked")
	}
}

// mustWalk runs one walk and fails on error.
func mustWalk(t *testing.T, api *recordingAPI, id string, params walk.Params) framework.Result {
	t.Helper()
	got, err := walk.Collector{API: api.build}.Walk(
		context.Background(), id, params, framework.ForeignContext{})
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
