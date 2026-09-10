// SPDX-License-Identifier: Apache-2.0

package glgraph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// build_test.go — the one place resources and relations become contract nodes
// and edges, and the four properties that live there: the vocabulary refusal, the
// sort, the deduplication, and the fields every node carries.

func resource(id, name, resourceType string) glgraph.Resource {
	return glgraph.Resource{ID: id, Name: name, ResourceType: resourceType}
}

// TestAnUndeclaredResourceTypeIsRefused is what closes the coverage assertion
// into an equality: without it, the emitted set could carry anything and the
// parity row would only ever check one direction.
func TestAnUndeclaredResourceTypeIsRefused(t *testing.T) {
	_, _, err := glgraph.Build(glgraph.Result{Resources: []glgraph.Resource{
		resource("gitlab:acme/Widget/1", "widget", "widget"),
	}})
	if err == nil {
		t.Fatal("a resource type outside the declared vocabulary was built into a node")
	}
	for _, want := range []string{"widget", "gitlab:acme/Widget/1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	// The known positive: a DECLARED type is built, so the refusal is about the
	// vocabulary rather than about every resource.
	if _, _, err := glgraph.Build(glgraph.Result{Resources: []glgraph.Resource{
		resource("gitlab:acme/Group/acme", "acme", glgraph.ResourceTypeGroup),
	}}); err != nil {
		t.Fatalf("a declared type was refused: %v", err)
	}
}

func TestAnUndeclaredEdgeTypeIsRefused(t *testing.T) {
	_, _, err := glgraph.Build(glgraph.Result{Relations: []glgraph.Relation{
		{FromID: "a", ToID: "b", Type: "INVENTED"},
	}})
	if err == nil {
		t.Fatal("an edge type outside the declared vocabulary was built into an edge")
	}
	if !strings.Contains(err.Error(), "INVENTED") {
		t.Errorf("the refusal does not name the type: %v", err)
	}
}

// TestTheNotEmittedEdgeTypeIsRefusedToo. The seventh relationship is declared as
// a constant so its absence can be asserted, and declaring it must not make it
// buildable — or a converter could start emitting it and every floor row would
// still pass.
func TestTheNotEmittedEdgeTypeIsRefusedToo(t *testing.T) {
	_, _, err := glgraph.Build(glgraph.Result{Relations: []glgraph.Relation{
		{FromID: "a", ToID: "b", Type: glgraph.EdgeTriggeredBy},
	}})
	if err == nil {
		t.Fatalf("%s was built into an edge; it is declared so its ABSENCE can be asserted, and it "+
			"is not in the emitted vocabulary", glgraph.EdgeTriggeredBy)
	}
}

func TestAnEmptyIDOrEndpointIsRefused(t *testing.T) {
	if _, _, err := glgraph.Build(glgraph.Result{Resources: []glgraph.Resource{
		resource("", "nameless", glgraph.ResourceTypeGroup),
	}}); err == nil {
		t.Error("a resource with no id was built into a node")
	}
	for _, rel := range []glgraph.Relation{
		{FromID: "", ToID: "b", Type: glgraph.EdgeBelongsTo},
		{FromID: "a", ToID: "", Type: glgraph.EdgeBelongsTo},
	} {
		if _, _, err := glgraph.Build(glgraph.Result{Relations: []glgraph.Relation{rel}}); err == nil {
			t.Errorf("an edge with an empty endpoint was built: %+v", rel)
		}
	}
}

// TestNodesAndEdgesAreSortedAndDeduplicated is the property two collects of an
// unchanged group rest on, and the mechanism behind one node per distinct tag.
func TestNodesAndEdgesAreSortedAndDeduplicated(t *testing.T) {
	in := glgraph.Result{
		Resources: []glgraph.Resource{
			resource("gitlab:acme/RunnerTag/docker", "docker", glgraph.ResourceTypeRunnerTag),
			resource("gitlab:acme/Group/acme", "acme", glgraph.ResourceTypeGroup),
			// The same tag, minted a second time by another runner.
			resource("gitlab:acme/RunnerTag/docker", "docker", glgraph.ResourceTypeRunnerTag),
			resource("gitlab:acme/Runner/2", "runner-2", glgraph.ResourceTypeRunner),
		},
		Relations: []glgraph.Relation{
			{FromID: "gitlab:acme/Runner/2", ToID: "gitlab:acme/RunnerTag/docker",
				Type: glgraph.EdgeHasLabel},
			{FromID: "gitlab:acme/Runner/1", ToID: "gitlab:acme/RunnerTag/docker",
				Type: glgraph.EdgeHasLabel},
			// The identical edge twice, which says nothing a single one does not.
			{FromID: "gitlab:acme/Runner/2", ToID: "gitlab:acme/RunnerTag/docker",
				Type: glgraph.EdgeHasLabel},
		},
	}

	nodes, edges, err := glgraph.Build(in)
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	gotNodes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		gotNodes = append(gotNodes, node.ID)
	}
	wantNodes := []string{
		"gitlab:acme/Group/acme",
		"gitlab:acme/Runner/2",
		"gitlab:acme/RunnerTag/docker",
	}
	if !slices.Equal(gotNodes, wantNodes) {
		t.Errorf("the built nodes are %v, want %v — sorted by id with one node per distinct id",
			gotNodes, wantNodes)
	}

	if len(edges) != 2 {
		t.Errorf("the built graph carries %d edges, want 2: two runners into one tag, with the "+
			"repeated edge collapsed", len(edges))
	}
	if len(edges) == 2 && edges[0].FromID > edges[1].FromID {
		t.Errorf("the edges are not sorted: %q then %q", edges[0].FromID, edges[1].FromID)
	}
}

// TestEveryNodeCarriesTheContractFields pins the shape a consumer queries: one
// node type, the kind in metadata, the provider tag, and the deterministic
// summary.
func TestEveryNodeCarriesTheContractFields(t *testing.T) {
	nodes, _, err := glgraph.Build(glgraph.Result{Resources: []glgraph.Resource{{
		ID:           "gitlab:acme/Project/acme/api",
		Name:         "api",
		ResourceType: glgraph.ResourceTypeProject,
		Content:      `{"default_branch":"main"}`,
		Metadata:     map[string]string{"visibility": "private"},
	}}})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	node := nodes[0]

	if node.Type != "cicd-resource" {
		t.Errorf("the node type is %q, want the one type every resource in this graph carries",
			node.Type)
	}
	if node.Source != "cicd" {
		t.Errorf("the node source is %q", node.Source)
	}
	if node.SymbolName != "api" {
		t.Errorf("the node's symbol name is %q, want the resource's own name", node.SymbolName)
	}
	if node.Summary == "" {
		t.Error("the node carries no summary")
	}
	for key, want := range map[string]string{
		"resource_type": glgraph.ResourceTypeProject,
		"provider":      "gitlab",
		"visibility":    "private",
	} {
		if node.Metadata[key] != want {
			t.Errorf("the node's %q metadata is %q, want %q", key, node.Metadata[key], want)
		}
	}
}

// TestNoEdgeNamesAnotherGraph. A cross-graph edge is a new assertion about the
// operator's other graphs, and this collector makes none.
func TestNoEdgeNamesAnotherGraph(t *testing.T) {
	_, edges, err := glgraph.Build(glgraph.Result{Relations: []glgraph.Relation{
		{FromID: "a", ToID: "b", Type: glgraph.EdgeBelongsTo},
	}})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if edges[0].SourceGraph != "" || edges[0].TargetGraph != "" {
		t.Errorf("an edge names a foreign graph: source=%q target=%q",
			edges[0].SourceGraph, edges[0].TargetGraph)
	}
	if edges[0].Evidence != "" || edges[0].Method != "" {
		t.Errorf("an edge carries evidence %q by method %q; no edge in this graph carries either",
			edges[0].Evidence, edges[0].Method)
	}
}
