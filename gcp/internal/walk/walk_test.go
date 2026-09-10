// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/walk"
)

// walk_test.go — the whole walk, driven end to end over canned enumerations with
// no credential and no network.

func fixedEnumerations(subs ...collect.Subcollector) walk.Enumerations {
	return func(context.Context, string) ([]collect.Subcollector, func(), error) {
		return subs, func() {}, nil
	}
}

// staticSub is an enumeration that returns what it was given.
func staticSub(name string, out gcpgraph.Result) collect.Subcollector {
	return collect.Subcollector{
		Name: name,
		Run:  func(context.Context, string) (gcpgraph.Result, error) { return out, nil },
	}
}

func failingSub(name string, err error) collect.Subcollector {
	return collect.Subcollector{
		Name: name,
		Run:  func(context.Context, string) (gcpgraph.Result, error) { return gcpgraph.Result{}, err },
	}
}

func TestWalkRefusesACollectWithNoProject(t *testing.T) {
	for _, project := range []string{"", "   "} {
		_, err := walk.Collector{Enumerations: fixedEnumerations()}.
			Walk(t.Context(), "gcp-instance", walk.Params{Project: project}, framework.ForeignContext{})
		if err == nil {
			t.Fatalf("a collect with project %q was walked anyway", project)
		}
		if !strings.Contains(err.Error(), "project") {
			t.Errorf("error %q does not name what is missing", err)
		}
		// The refusal says there is nothing to fall back to, because an operator
		// whose tooling resolves a project from the environment will otherwise
		// assume this one does too.
		if !strings.Contains(err.Error(), "environment variable") {
			t.Errorf("error %q does not say that no environment variable is read", err)
		}
	}
}

func TestWalkReturnsWhatTheEnumerationsFound(t *testing.T) {
	got, err := walk.Collector{Enumerations: fixedEnumerations(
		staticSub("a", gcpgraph.Result{Resources: []gcpgraph.Resource{
			{ID: "id-a", Name: "a", ResourceType: gcpgraph.ResourceTypeInstance},
		}}),
		staticSub("b", gcpgraph.Result{
			Resources: []gcpgraph.Resource{
				{ID: "id-b", Name: "b", ResourceType: gcpgraph.ResourceTypeNetwork},
			},
			Relations: []gcpgraph.Relation{
				{From: "id-a", To: "id-b", Type: gcpgraph.EdgeUsesNetwork},
			},
		}),
	)}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("got %d nodes and %d edges, want 2 and 1", len(got.Nodes), len(got.Edges))
	}
	if !got.Complete.IsAsserted() || !got.Complete.IsComplete() {
		t.Errorf("a clean walk did not assert completeness: %+v", got.Complete)
	}
	// The nodes come back SORTED, which is what makes a re-collect comparable.
	if got.Nodes[0].ID != "id-a" || got.Nodes[1].ID != "id-b" {
		t.Errorf("nodes are not in a stable order: %q, %q", got.Nodes[0].ID, got.Nodes[1].ID)
	}
}

// The completeness matrix. The middle row is the one that matters: a partial
// read reported as complete is what lets a server treat what this walk could not
// see as deleted.
func TestWalkCompletenessMatrix(t *testing.T) {
	good := staticSub("good", gcpgraph.Result{Resources: []gcpgraph.Resource{
		{ID: "id-a", Name: "a", ResourceType: gcpgraph.ResourceTypeInstance},
	}})

	t.Run("a clean walk is complete", func(t *testing.T) {
		got, err := walk.Collector{Enumerations: fixedEnumerations(good)}.
			Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if !got.Complete.IsComplete() {
			t.Error("a clean walk asserted an incomplete result")
		}
		if len(got.Nodes) != 1 {
			t.Errorf("got %d nodes, want 1", len(got.Nodes))
		}
	})

	t.Run("one failure is incomplete AND still returns what was read", func(t *testing.T) {
		got, err := walk.Collector{Enumerations: fixedEnumerations(
			good, failingSub("broken", errors.New("the backend went away")),
		)}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("one failed enumeration failed the whole walk: %v", err)
		}
		if got.Complete.IsComplete() {
			t.Fatal("a walk with a failed enumeration asserted COMPLETE; " +
				"the server would treat everything it could not read as deleted")
		}
		if len(got.Nodes) != 1 {
			t.Errorf("the successful enumeration's nodes were dropped: got %d", len(got.Nodes))
		}
		// The reason names the enumeration, because an operator reading a
		// partial collect needs to know which part is missing.
		if !strings.Contains(got.Complete.Reason(), "broken") {
			t.Errorf("the incomplete reason does not name the failure: %q", got.Complete.Reason())
		}
	})

	t.Run("every failure is an error, not an empty success", func(t *testing.T) {
		_, err := walk.Collector{Enumerations: fixedEnumerations(
			failingSub("one", errors.New("down")),
			failingSub("two", errors.New("down")),
		)}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
		if err == nil {
			t.Fatal("a walk that read nothing at all reported success")
		}
		if !strings.Contains(err.Error(), "every enumeration") {
			t.Errorf("error %q does not say the whole walk failed", err)
		}
	})
}

// An empty project is a COMPLETE walk that found nothing, and it is the first
// real run of any new collector.
func TestWalkOnAnEmptyProjectIsCompleteAndEmpty(t *testing.T) {
	got, err := walk.Collector{Enumerations: fixedEnumerations(
		staticSub("empty", gcpgraph.Result{}),
	)}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !got.Complete.IsComplete() {
		t.Error("an empty project was reported as an incomplete walk")
	}
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Errorf("an empty project produced %d nodes and %d edges", len(got.Nodes), len(got.Edges))
	}
}

// A resolver failure fails the WALK. The derivations produce edges nothing else
// produces, so a walk that swallowed one would assert a complete graph missing
// them.
func TestWalkFailsWhenADerivationCannotRead(t *testing.T) {
	_, err := walk.Collector{Enumerations: fixedEnumerations(
		staticSub("firewalls", gcpgraph.Result{Resources: []gcpgraph.Resource{{
			ID: "fw-1", Name: "broken", ResourceType: gcpgraph.ResourceTypeFirewall,
			Content: []byte("{not json"),
		}}}),
	)}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a derivation that could not read its input was swallowed")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q does not name what could not be read", err)
	}
}

// A converter that emitted a type outside the declared vocabulary fails the
// walk, which is what keeps the emitted set a subset of the declared one.
func TestWalkRefusesAnUndeclaredResourceType(t *testing.T) {
	_, err := walk.Collector{Enumerations: fixedEnumerations(
		staticSub("invented", gcpgraph.Result{Resources: []gcpgraph.Resource{
			{ID: "x", Name: "x", ResourceType: "gcp:invented:thing"},
		}}),
	)}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a resource type outside the declared vocabulary was emitted")
	}
	if !strings.Contains(err.Error(), "gcp:invented:thing") {
		t.Errorf("error %q does not name the offending type", err)
	}
}

func TestWalkFailsWhenTheEnumerationsCannotBeBuilt(t *testing.T) {
	_, err := walk.Collector{Enumerations: func(context.Context, string) ([]collect.Subcollector, func(), error) {
		return nil, nil, errors.New("could not find Application Default Credentials")
	}}.Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a walk with no usable client reported success")
	}
	if !strings.Contains(err.Error(), "Application Default Credentials") {
		t.Errorf("the credential failure was not surfaced: %q", err)
	}
}

// The fan-out is bounded and it releases what it held. The cancellation arm is
// where a leak would live: a walk cancelled mid-fan-out must not leave workers
// blocked on a channel nobody closes.
func TestWalkOnACancelledContextReturnsAndReleases(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	released := false
	_, err := walk.Collector{
		Concurrency: 2,
		Enumerations: func(context.Context, string) ([]collect.Subcollector, func(), error) {
			subs := make([]collect.Subcollector, 0, 50)
			for i := range 50 {
				subs = append(subs, staticSub(string(rune('a'+i%26)), gcpgraph.Result{
					Resources: []gcpgraph.Resource{{
						ID: string(rune('a' + i%26)), ResourceType: gcpgraph.ResourceTypeDisk,
					}},
				}))
			}
			return subs, func() { released = true }, nil
		},
	}.Walk(ctx, "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	// Either arm is legitimate: the walk may complete before the cancellation is
	// observed. What must NOT happen is a hang, and what must always happen is
	// the release.
	if err != nil && !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "context") {
		t.Errorf("unexpected error on a cancelled walk: %v", err)
	}
	if !released {
		t.Error("the walk did not release what the enumerations held")
	}
}

func TestToolNamesTheCollectTool(t *testing.T) {
	if got := (walk.Collector{}).Tool().Name; got != "" {
		t.Errorf("an unset tool name should defer to the framework default: got %q", got)
	}
	if got := (walk.Collector{ToolName: "collect_gcp"}).Tool().Name; got != "collect_gcp" {
		t.Errorf("Tool name override: got %q", got)
	}
	if (walk.Collector{}).Tool().Description == "" {
		t.Error("the served tool carries no description; an operator installing it reads that")
	}
}

// THE FOREIGN-CONTEXT BLOCK CHANGES NOTHING, and that is the claim this
// collector's registration makes by declaring no foreign slice.
//
// WHY PIN IT. The block is a new input on this seam and the sibling collectors
// are wiring it in. If someone later reads it here without also declaring the
// slice in this collector's config entry, the walk would depend on data the
// entry never asked for — which arrives as the zero value in production and as
// something useful only in whatever test they wrote. That is a graph whose
// contents differ between a developer's machine and an operator's, and it fails
// silently. This test makes it a red instead.
//
// THE BLOCK BELOW IS DELIBERATELY NOT EMPTY. A zero-value block would make the
// two runs identical by construction and prove nothing; this one carries a
// resource in each family, so a walk that read either would produce a different
// result.
func TestTheForeignContextBlockChangesNothing(t *testing.T) {
	enumerations := fixedEnumerations(staticSub("a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			{ID: "id-a", Name: "a", ResourceType: gcpgraph.ResourceTypeInstance},
		},
	}))

	populated := framework.ForeignContext{
		"aws": []framework.ForeignGraph{{
			GraphName: "cloud/other-project",
			Nodes: []framework.ForeignNode{{
				ID:   "https://www.googleapis.com/compute/v1/projects/other/zones/z/instances/vm",
				Type: "gcp:compute:instance", SymbolName: "vm",
				Metadata: map[string]string{"label/app.kubernetes.io/name": "api"},
			}},
			Edges: []framework.ForeignEdge{{FromID: "a", ToID: "b"}},
		}},
		framework.FamilyCode: []framework.ForeignGraph{{
			GraphName: "code/some-repo",
			Nodes:     []framework.ForeignNode{{ID: "chart/Chart.yaml", SymbolName: "api"}},
		}},
	}
	// The control on the fixture itself: a block that was empty would make the
	// comparison below vacuous.
	if populated.IsEmpty() {
		t.Fatal("the fixture block is empty; the comparison below would prove nothing")
	}

	empty, err := walk.Collector{Enumerations: enumerations}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk with an empty block: %v", err)
	}
	full, err := walk.Collector{Enumerations: enumerations}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, populated)
	if err != nil {
		t.Fatalf("Walk with a populated block: %v", err)
	}

	if len(empty.Nodes) != len(full.Nodes) || len(empty.Edges) != len(full.Edges) {
		t.Fatalf("the foreign block changed the output size: %d/%d nodes, %d/%d edges",
			len(empty.Nodes), len(full.Nodes), len(empty.Edges), len(full.Edges))
	}
	// Nodes carry a metadata map, so they are compared by their encoded form:
	// that reads every field including the map, and a map encodes with its keys
	// sorted, so the comparison is over contents rather than over iteration
	// order.
	for i := range empty.Nodes {
		if encoded(t, empty.Nodes[i]) != encoded(t, full.Nodes[i]) {
			t.Errorf("node %d differs when a foreign block is supplied: %s vs %s",
				i, encoded(t, empty.Nodes[i]), encoded(t, full.Nodes[i]))
		}
	}
	for i := range empty.Edges {
		if empty.Edges[i] != full.Edges[i] {
			t.Errorf("edge %d differs when a foreign block is supplied: %+v vs %+v",
				i, empty.Edges[i], full.Edges[i])
		}
	}
	if empty.Complete.IsComplete() != full.Complete.IsComplete() {
		t.Error("the foreign block changed the completeness assertion")
	}
}

// encoded renders a node for comparison; see the note at its call site.
func encoded(t *testing.T, node framework.Node) string {
	t.Helper()
	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("encoding a node for comparison: %v", err)
	}
	return string(raw)
}

// The collector satisfies the framework's interface. It is a compile-time
// assertion because the framework's entry points are generic over it, so a
// signature drift would otherwise surface only where main.go wires it up.
var _ framework.Collector[walk.Params] = walk.Collector{}
