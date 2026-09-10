// SPDX-License-Identifier: Apache-2.0

package ghgraph_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// build_test.go — the refusals, the sort and the deduplication.

// TestAResourceTypeOutsideTheVocabularyIsRefused is what closes the coverage
// assertion into an equality: without it, that assertion is a one-way superset
// check a collector could satisfy while emitting anything else beside.
func TestAResourceTypeOutsideTheVocabularyIsRefused(t *testing.T) {
	_, _, err := ghgraph.Build(ghgraph.Result{Resources: []ghgraph.Resource{{
		ID:           "github:acme/Invented/x",
		Name:         "x",
		ResourceType: "invented",
	}}})
	if err == nil {
		t.Fatal("a resource type nothing declared was accepted")
	}
	if !strings.Contains(err.Error(), "invented") {
		t.Errorf("the refusal does not name the offending type: %v", err)
	}

	// The known positive: a DECLARED type on the same path is accepted, so this
	// is a refusal of the type rather than a builder that refuses everything.
	if _, _, err := ghgraph.Build(ghgraph.Result{Resources: []ghgraph.Resource{{
		ID:           "github:acme/Organization/acme",
		Name:         "acme",
		ResourceType: ghgraph.ResourceTypeOrganization,
	}}}); err != nil {
		t.Fatalf("a declared type was refused too: %v", err)
	}
}

// TestAnEdgeTypeOutsideTheVocabularyIsRefused, including RUNS_IN — which is
// declared as a constant so its absence can be asserted, and is deliberately not
// in the emitted vocabulary.
func TestAnEdgeTypeOutsideTheVocabularyIsRefused(t *testing.T) {
	for _, edgeType := range []string{"INVENTED", ghgraph.EdgeRunsIn} {
		_, _, err := ghgraph.Build(ghgraph.Result{
			Resources: []ghgraph.Resource{{
				ID: "a", Name: "a", ResourceType: ghgraph.ResourceTypeOrganization,
			}},
			Relations: []ghgraph.Relation{{FromID: "a", ToID: "b", Type: edgeType}},
		})
		if err == nil {
			t.Errorf("the edge type %q was accepted", edgeType)
			continue
		}
		if !strings.Contains(err.Error(), edgeType) {
			t.Errorf("the refusal does not name %q: %v", edgeType, err)
		}
	}

	// THE KNOWN POSITIVE, AND IT NAMES THE CLASSES THIS ROUND ADDED. A builder
	// that refused every edge type would satisfy the loop above; these three are
	// the ones whose admission the round depends on, and a near miss on each name
	// is refused beside it so the admission is of the declared spelling rather
	// than of anything shaped like it.
	for _, edgeType := range []string{
		ghgraph.EdgeAttributedTo, ghgraph.EdgeInitiatedBy, ghgraph.EdgeCreatedBy,
	} {
		if _, _, err := ghgraph.Build(ghgraph.Result{
			Resources: []ghgraph.Resource{{
				ID: "a", Name: "a", ResourceType: ghgraph.ResourceTypeOrganization,
			}},
			Relations: []ghgraph.Relation{{FromID: "a", ToID: "b", Type: edgeType}},
		}); err != nil {
			t.Errorf("the declared edge type %q was refused: %v", edgeType, err)
		}
		nearMiss := edgeType + "_USER"
		if _, _, err := ghgraph.Build(ghgraph.Result{
			Resources: []ghgraph.Resource{{
				ID: "a", Name: "a", ResourceType: ghgraph.ResourceTypeOrganization,
			}},
			Relations: []ghgraph.Relation{{FromID: "a", ToID: "b", Type: nearMiss}},
		}); err == nil {
			t.Errorf("the undeclared near miss %q was accepted beside %q", nearMiss, edgeType)
		}
	}
}

// TestAnEmptyIDOrEndpointIsRefused. A node with no id and an edge naming an
// empty endpoint are both bad input, and both would land silently.
func TestAnEmptyIDOrEndpointIsRefused(t *testing.T) {
	if _, _, err := ghgraph.Build(ghgraph.Result{Resources: []ghgraph.Resource{{
		Name: "x", ResourceType: ghgraph.ResourceTypeLabel,
	}}}); err == nil {
		t.Error("a resource with no id was accepted")
	}
	if _, _, err := ghgraph.Build(ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID: "a", Name: "a", ResourceType: ghgraph.ResourceTypeOrganization,
		}},
		Relations: []ghgraph.Relation{{FromID: "a", Type: ghgraph.EdgeBelongsTo}},
	}); err == nil {
		t.Error("an edge naming an empty endpoint was accepted")
	}
}

// TestTheOutputIsSortedAndDeduplicated is the row the carry-forward rests on and
// the row the three materialized classes rest on.
func TestTheOutputIsSortedAndDeduplicated(t *testing.T) {
	label := ghgraph.Resource{
		ID:           ghgraph.LabelID("acme", "self-hosted"),
		Name:         "self-hosted",
		ResourceType: ghgraph.ResourceTypeLabel,
		Metadata:     map[string]string{"org": "acme"},
	}
	nodes, edges, err := ghgraph.Build(ghgraph.Result{
		// Deliberately out of order, and the label minted three times, which is
		// what a walk over three runners carrying it produces.
		Resources: []ghgraph.Resource{
			label,
			{ID: "github:acme/Runner/2", Name: "b", ResourceType: ghgraph.ResourceTypeRunner},
			label,
			{ID: "github:acme/Organization/acme", Name: "acme", ResourceType: ghgraph.ResourceTypeOrganization},
			label,
		},
		Relations: []ghgraph.Relation{
			{FromID: "github:acme/Runner/2", ToID: label.ID, Type: ghgraph.EdgeHasLabel},
			{FromID: "github:acme/Runner/1", ToID: label.ID, Type: ghgraph.EdgeHasLabel},
			// The same edge twice, which says nothing a single one does not.
			{FromID: "github:acme/Runner/2", ToID: label.ID, Type: ghgraph.EdgeHasLabel},
		},
	})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	want := []string{
		"github:acme/Label/self-hosted",
		"github:acme/Organization/acme",
		"github:acme/Runner/2",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("the built nodes are %v, want %v — sorted by id with one node per id", ids, want)
	}
	if len(edges) != 2 {
		t.Errorf("the built edges are %d, want 2 — the repeated edge deduplicated and the two "+
			"distinct ones kept", len(edges))
	}
	if edges[0].FromID != "github:acme/Runner/1" {
		t.Errorf("the edges are not sorted: the first is from %q", edges[0].FromID)
	}
}

// TestTwoCopiesOfOneIdDeduplicateTheSameWayInEitherArrivalOrder is the row the
// carry-forward promise rests on once more than one enumeration mints the same
// node.
//
// WHY THE SORT KEY IS NOT THE ID ALONE, and this is the shape that had no test.
// A stable sort preserves INPUT order for equal keys, and this collector's input
// order is whatever its concurrent fan-out finished in — [Result.Add] says so.
// While the only multiply-minted classes produced byte-identical copies that was
// harmless. A user is minted by the environments enumeration from a protection
// rule AND by the runs and deployments enumerations from an actor or a creator,
// and those two provider objects do not carry the same fields, so with an
// id-only key which copy survived was decided by a goroutine race and two
// collects of an organization nobody changed differed.
//
// MEASURED ON THIS TREE: with the tiebreak removed, the two builds below produce
// different bytes.
//
// AND THE FULLER COPY WINS, which is a property of the encoding rather than a
// separate rule: a detail struct marshals its fields in declaration order and
// omits the empty ones, so the shorter copy is a prefix of the longer one, and
// the comma that continues the longer one sorts before the brace that ends the
// shorter.
func TestTwoCopiesOfOneIdDeduplicateTheSameWayInEitherArrivalOrder(t *testing.T) {
	base := ghgraph.Resource{
		ID:           ghgraph.UserID("acme", "ada"),
		Name:         "ada",
		ResourceType: ghgraph.ResourceTypeUser,
		Content:      `{"kind":"User","name":"ada"}`,
		Metadata:     map[string]string{"org": "acme"},
	}

	// ONE PAIR PER FIELD THAT CAN DISTINGUISH TWO COPIES, because the property
	// is that the ORDERING is total over them: a pair differing in a field the
	// sort does not read compares equal, and a stable sort leaves equal keys in
	// arrival order. A single pair proves the ordering reads ONE field.
	for _, row := range []struct {
		field string
		vary  func(ghgraph.Resource) ghgraph.Resource
	}{
		{"Content", func(res ghgraph.Resource) ghgraph.Resource {
			res.Content = `{"kind":"User","name":"ada","id":7,"html_url":"https://github.com/ada"}`
			return res
		}},
		{"Name", func(res ghgraph.Resource) ghgraph.Resource {
			res.Name = "ada-renamed"
			return res
		}},
		{"ResourceType", func(res ghgraph.Resource) ghgraph.Resource {
			res.ResourceType = ghgraph.ResourceTypeTeam
			return res
		}},
		{"Metadata", func(res ghgraph.Resource) ghgraph.Resource {
			res.Metadata = map[string]string{"org": "acme", "repo": "acme/api"}
			return res
		}},
	} {
		t.Run(row.field, func(t *testing.T) {
			other := row.vary(base)

			first, _, err := ghgraph.Build(ghgraph.Result{
				Resources: []ghgraph.Resource{base, other},
			})
			if err != nil {
				t.Fatalf("building the base-first order: %v", err)
			}
			second, _, err := ghgraph.Build(ghgraph.Result{
				Resources: []ghgraph.Resource{other, base},
			})
			if err != nil {
				t.Fatalf("building the other-first order: %v", err)
			}

			if len(first) != 1 || len(second) != 1 {
				t.Fatalf("the two builds carry %d and %d nodes, want one node per id",
					len(first), len(second))
			}
			if !sameNode(first[0], second[0]) {
				t.Errorf("two copies differing only in %s resolve by arrival order:\n"+
					"  base first:  %s\n  other first: %s\n"+
					"The sort key does not read %s, so a stable sort leaves the pair in whatever "+
					"order the concurrent fan-out finished in.",
					row.field, render(first[0]), render(second[0]), row.field)
			}
		})
	}

	// AND THE FULLER COPY WINS ON CONTENT, which is the one arm where WHICH copy
	// survives is a property worth having rather than merely a stable one.
	full := base
	full.Content = `{"kind":"User","name":"ada","id":7,"html_url":"https://github.com/ada"}`
	got, _, err := ghgraph.Build(ghgraph.Result{Resources: []ghgraph.Resource{base, full}})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if got[0].Content != full.Content {
		t.Errorf("the surviving copy is %s, want the one carrying the provider's fields %s",
			got[0].Content, full.Content)
	}
}

// TestTheResourceSortKeyReadsEveryFieldTheResourceCarries is the census half of
// the row above, and it exists because that row can only cover the fields its
// author thought of.
//
// A FIELD ADDED TO Resource AND NOT TO THE SORT reopens the arrival-order gap
// silently: two copies agreeing on everything the sort reads compare equal, and
// nothing in the suite would notice. This asserts the count the ordering was
// written against, so adding a field is a red that sends the author to the sort
// rather than a change that passes.
//
// THE BEHAVIORAL PAIRING IS THE ROW ABOVE. A count agreeing with itself proves
// nothing about what the comparator does; those subtests are what prove each
// counted field actually orders.
func TestTheResourceSortKeyReadsEveryFieldTheResourceCarries(t *testing.T) {
	const ordered = 5 // ID, Content, Name, ResourceType, Metadata
	if got := reflect.TypeOf(ghgraph.Resource{}).NumField(); got != ordered {
		t.Errorf("Resource carries %d fields and the node ordering is written against %d.\n"+
			"Two copies of one id that agree on every ORDERED field compare equal, and the stable "+
			"sort then leaves them in the concurrent fan-out's arrival order — which is the race "+
			"the ordering exists to remove. Add the new field to buildNodes' comparison and give "+
			"it a subtest above.", got, ordered)
	}
}

// sameNode compares two built nodes on everything a consumer reads, so a
// difference in any of it counts as a different surviving copy.
func sameNode(a, b framework.Node) bool {
	return render(a) == render(b)
}

// render is a node as the bytes that go on the wire, which is what "changed"
// means to the receiving server.
func render(node framework.Node) string {
	raw, err := json.Marshal(node)
	if err != nil {
		return "unmarshalable node: " + node.ID
	}
	return string(raw)
}

// TestEveryNodeCarriesTheTypeTheKindAndTheProvider pins the one-node-type shape
// a consumer's queries are written against.
func TestEveryNodeCarriesTheTypeTheKindAndTheProvider(t *testing.T) {
	nodes, _, err := ghgraph.Build(ghgraph.Result{Resources: []ghgraph.Resource{{
		ID:           "github:acme/Repository/acme/api",
		Name:         "acme/api",
		ResourceType: ghgraph.ResourceTypeRepository,
		Metadata:     map[string]string{"org": "acme"},
	}}})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	node := nodes[0]
	if node.Type != "cicd-resource" {
		t.Errorf("the node type is %q, want %q — one type for every kind, with the kind in "+
			"metadata, which is the shape a consumer already queries", node.Type, "cicd-resource")
	}
	if node.Metadata["resource_type"] != ghgraph.ResourceTypeRepository {
		t.Errorf("the node's resource_type is %q", node.Metadata["resource_type"])
	}
	if node.Metadata["provider"] != "github" {
		t.Errorf("the node's provider is %q, want %q", node.Metadata["provider"], "github")
	}
	if node.Source != "cicd" {
		t.Errorf("the node's source is %q, want %q", node.Source, "cicd")
	}
	if node.Metadata["org"] != "acme" {
		t.Error("the resource's own metadata was dropped")
	}
	if node.Summary == "" {
		t.Error("the node carries no summary")
	}
}

// TestNoEdgeNamesAForeignGraph. Every relationship this collector emits joins two
// nodes of its own graph; naming a family would send the edge into the linkage
// graph instead, which is a different assertion entirely.
func TestNoEdgeNamesAForeignGraph(t *testing.T) {
	_, edges, err := ghgraph.Build(ghgraph.Result{
		Resources: []ghgraph.Resource{
			{ID: "a", Name: "a", ResourceType: ghgraph.ResourceTypeOrganization},
		},
		Relations: []ghgraph.Relation{{FromID: "a", ToID: "b", Type: ghgraph.EdgeBelongsTo}},
	})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if edges[0].SourceGraph != "" || edges[0].TargetGraph != "" {
		t.Errorf("an edge names a foreign graph family (source=%q target=%q)",
			edges[0].SourceGraph, edges[0].TargetGraph)
	}
}

// TestOnlyAnEdgeWithEvidenceCarriesAMethod. The evidence and the method travel
// together: a method with no evidence says how an edge was derived while
// carrying none of it.
func TestOnlyAnEdgeWithEvidenceCarriesAMethod(t *testing.T) {
	_, edges, err := ghgraph.Build(ghgraph.Result{
		Resources: []ghgraph.Resource{
			{ID: "a", Name: "a", ResourceType: ghgraph.ResourceTypeOrganization},
		},
		Relations: []ghgraph.Relation{
			{FromID: "a", ToID: "b", Type: ghgraph.EdgeBelongsTo},
			{FromID: "a", ToID: "c", Type: ghgraph.EdgeTriggeredBy, Evidence: `{"event":"push"}`},
		},
	})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	for _, edge := range edges {
		if edge.Evidence == "" && edge.Method != "" {
			t.Errorf("the edge %s -> %s carries the method %q with no evidence",
				edge.FromID, edge.ToID, edge.Method)
		}
		if edge.Evidence != "" && edge.Method != "cicd-collect" {
			t.Errorf("the edge %s -> %s carries evidence with the method %q",
				edge.FromID, edge.ToID, edge.Method)
		}
	}
}
