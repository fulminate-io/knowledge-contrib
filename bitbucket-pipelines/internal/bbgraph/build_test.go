// SPDX-License-Identifier: Apache-2.0

package bbgraph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// build_test.go — the vocabulary refusal, the sort, the dedup and the fields
// [bbgraph.Build] stamps.

// oneOfEach is a result carrying one resource of a declared type and one edge of
// a declared type, used as the base every arm below mutates.
func oneOfEach() bbgraph.Result {
	return bbgraph.Result{
		Resources: []bbgraph.Resource{{
			ID:           bbgraph.RepositoryID("acme", "api"),
			Name:         "acme/api",
			ResourceType: bbgraph.ResourceTypeRepository,
			Metadata:     map[string]string{"workspace": "acme", "slug": "api"},
		}, {
			ID:           bbgraph.WorkspaceID("acme"),
			Name:         "acme",
			ResourceType: bbgraph.ResourceTypeWorkspace,
			Metadata:     map[string]string{"workspace": "acme"},
		}},
		Relations: []bbgraph.Relation{{
			FromID: bbgraph.RepositoryID("acme", "api"),
			ToID:   bbgraph.WorkspaceID("acme"),
			Type:   bbgraph.EdgeBelongsTo,
		}},
	}
}

// TestBuildRefusesAnUndeclaredResourceType. This is what closes the coverage
// assertion from a one-way superset check into an equality: a type nothing
// declared is a node no consumer queries for.
func TestBuildRefusesAnUndeclaredResourceType(t *testing.T) {
	result := oneOfEach()
	result.Resources[0].ResourceType = "deployment"

	_, _, err := bbgraph.Build(result)
	if err == nil {
		t.Fatal("a resource type outside the declared vocabulary was passed through")
	}
	if !strings.Contains(err.Error(), "deployment") {
		t.Errorf("the refusal does not name the offending type: %v", err)
	}

	// The known positive: the unmutated result builds, so the refusal is about
	// the type rather than about the shape of the input.
	if _, _, err := bbgraph.Build(oneOfEach()); err != nil {
		t.Fatalf("the unmutated result did not build: %v", err)
	}
}

// TestBuildRefusesAnUndeclaredEdgeType, on the same terms.
func TestBuildRefusesAnUndeclaredEdgeType(t *testing.T) {
	result := oneOfEach()
	result.Relations[0].Type = bbgraph.EdgeTriggeredBy

	_, _, err := bbgraph.Build(result)
	if err == nil {
		t.Fatal("an edge type outside the declared vocabulary was passed through. TRIGGERED_BY is " +
			"declared so its ABSENCE can be asserted; it is not in the emitted set")
	}
	if !strings.Contains(err.Error(), bbgraph.EdgeTriggeredBy) {
		t.Errorf("the refusal does not name the offending type: %v", err)
	}
}

// TestBuildRefusesAnEmptyIdOrEndpoint. A node with no id is a node nothing can
// reference, and an edge with an empty endpoint is a relationship naming nothing
// at all.
func TestBuildRefusesAnEmptyIdOrEndpoint(t *testing.T) {
	missingID := oneOfEach()
	missingID.Resources[0].ID = ""
	if _, _, err := bbgraph.Build(missingID); err == nil {
		t.Error("a resource with no id was built into a node")
	}

	for _, mutate := range []func(*bbgraph.Relation){
		func(rel *bbgraph.Relation) { rel.FromID = "" },
		func(rel *bbgraph.Relation) { rel.ToID = "" },
	} {
		result := oneOfEach()
		mutate(&result.Relations[0])
		if _, _, err := bbgraph.Build(result); err == nil {
			t.Error("a relation with an empty endpoint was built into an edge")
		}
	}
}

// TestBuildSortsAndDeduplicates. Both are load-bearing rather than tidy: the
// sort is what makes two collects of an unchanged workspace identical, and the
// dedup is what makes the label node class work at all.
func TestBuildSortsAndDeduplicates(t *testing.T) {
	result := bbgraph.Result{
		Resources: []bbgraph.Resource{
			{ID: bbgraph.LabelID("acme", "linux"), Name: "linux",
				ResourceType: bbgraph.ResourceTypeLabel},
			{ID: bbgraph.RunnerID("acme", "{r2}"), Name: "two",
				ResourceType: bbgraph.ResourceTypeRunner},
			{ID: bbgraph.LabelID("acme", "linux"), Name: "linux",
				ResourceType: bbgraph.ResourceTypeLabel},
			{ID: bbgraph.RunnerID("acme", "{r1}"), Name: "one",
				ResourceType: bbgraph.ResourceTypeRunner},
		},
		Relations: []bbgraph.Relation{
			{FromID: bbgraph.RunnerID("acme", "{r2}"), ToID: bbgraph.LabelID("acme", "linux"),
				Type: bbgraph.EdgeHasLabel},
			{FromID: bbgraph.RunnerID("acme", "{r1}"), ToID: bbgraph.LabelID("acme", "linux"),
				Type: bbgraph.EdgeHasLabel},
			{FromID: bbgraph.RunnerID("acme", "{r1}"), ToID: bbgraph.LabelID("acme", "linux"),
				Type: bbgraph.EdgeHasLabel},
		},
	}

	nodes, edges, err := bbgraph.Build(result)
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	var ids []string
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	want := []string{
		"bitbucket:acme/Label/linux",
		"bitbucket:acme/Runner/{r1}",
		"bitbucket:acme/Runner/{r2}",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("the built nodes are %v, want %v — sorted by id and one per distinct id",
			ids, want)
	}
	if len(edges) != 2 {
		t.Errorf("the built graph carries %d edges, want 2 — the repeated one is deduplicated",
			len(edges))
	}
	if edges[0].FromID != "bitbucket:acme/Runner/{r1}" {
		t.Errorf("the edges are not sorted: the first is from %q, and {r1} sorts before {r2}",
			edges[0].FromID)
	}
}

// TestBuildStampsTheTypeSourceAndKindOnEveryNode. The resource type rides
// metadata rather than the node type, which is the shape a consumer already
// queries, and the provider and source tags are what identify the graph.
func TestBuildStampsTheTypeSourceAndKindOnEveryNode(t *testing.T) {
	nodes, _, err := bbgraph.Build(oneOfEach())
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	for _, node := range nodes {
		if node.Type != bbgraph.NodeType {
			t.Errorf("%s: the node type is %q, want the one type this graph carries, %q",
				node.ID, node.Type, bbgraph.NodeType)
		}
		if node.Source != bbgraph.SourceTag {
			t.Errorf("%s: the source is %q, want %q — it names the DOMAIN rather than the family",
				node.ID, node.Source, bbgraph.SourceTag)
		}
		if node.Metadata["provider"] != bbgraph.Provider {
			t.Errorf("%s: the provider metadata is %q, want %q",
				node.ID, node.Metadata["provider"], bbgraph.Provider)
		}
		if node.Metadata["resource_type"] == "" {
			t.Errorf("%s: the node carries no resource_type", node.ID)
		}
		if node.Summary == "" {
			t.Errorf("%s: the node carries no summary", node.ID)
		}
	}
	if bbgraph.Provider == "bitbucket-pipelines" {
		t.Error("the provider tag is the collector's family name; it names the SYSTEM the " +
			"resource lives in, which is what a consumer filters on")
	}
}

// TestNoEdgeNamesAForeignGraph. Every relationship this collector emits joins
// two nodes of its own graph; a cross-graph edge would be a new assertion about
// the operator's other graphs rather than a reproduction of what the provider
// states.
func TestNoEdgeNamesAForeignGraph(t *testing.T) {
	_, edges, err := bbgraph.Build(oneOfEach())
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("no edges were built; the assertion below means nothing")
	}
	for _, edge := range edges {
		if edge.SourceGraph != "" || edge.TargetGraph != "" {
			t.Errorf("the edge %s -> %s names a foreign graph (source=%q target=%q)",
				edge.FromID, edge.ToID, edge.SourceGraph, edge.TargetGraph)
		}
	}
}

// TestTheDeclaredVocabularyIsTheParityFloor. Nine resource types and six edge
// types, with the seventh CI/CD relationship declared and excluded.
func TestTheDeclaredVocabularyIsTheParityFloor(t *testing.T) {
	if got := len(bbgraph.ResourceTypes()); got != 9 {
		t.Errorf("the declared vocabulary carries %d resource types, want 9", got)
	}
	if got := len(bbgraph.EdgeTypes()); got != 6 {
		t.Errorf("the declared vocabulary carries %d edge types, want 6", got)
	}
	if bbgraph.IsDeclaredEdgeType(bbgraph.EdgeTriggeredBy) {
		t.Error("TRIGGERED_BY is in the emitted vocabulary; this provider emits it nowhere and " +
			"it is declared only so its absence can be asserted")
	}
	// The known positive: the constant exists and is not in the list, rather than
	// the list simply being short.
	if bbgraph.EdgeTriggeredBy != "TRIGGERED_BY" {
		t.Errorf("the declared-but-unemitted edge constant is %q", bbgraph.EdgeTriggeredBy)
	}
}
