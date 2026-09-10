// SPDX-License-Identifier: Apache-2.0

package gcpgraph_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// build_test.go — THE ONE PLACE a walk's intermediate resources become contract
// nodes, and the ORDERING that makes a re-collect comparable.

func TestBuildMapsAResourceOntoTheContractNode(t *testing.T) {
	nodes, edges, err := gcpgraph.Build(gcpgraph.Result{
		Resources: []gcpgraph.Resource{{
			ID:           "https://www.googleapis.com/compute/v1/projects/p/zones/z/instances/vm",
			Name:         "vm",
			ResourceType: gcpgraph.ResourceTypeInstance,
			Region:       "us-central1-a",
			Content:      []byte(`{"name":"vm"}`),
			Metadata:     map[string]string{"status": "RUNNING"},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(nodes) != 1 || len(edges) != 0 {
		t.Fatalf("Build: got %d nodes and %d edges, want 1 and 0", len(nodes), len(edges))
	}
	n := nodes[0]
	if n.ID != "https://www.googleapis.com/compute/v1/projects/p/zones/z/instances/vm" {
		t.Errorf("node id: got %q", n.ID)
	}
	if n.Type != gcpgraph.ResourceTypeInstance {
		t.Errorf("node type: got %q, want the resource type %q", n.Type, gcpgraph.ResourceTypeInstance)
	}
	if n.SymbolName != "vm" {
		t.Errorf("node symbol_name: got %q, want vm", n.SymbolName)
	}
	if n.Content != `{"name":"vm"}` {
		t.Errorf("node content: got %q", n.Content)
	}
	if n.Source != "gcp" {
		t.Errorf("node source: got %q, want gcp", n.Source)
	}
	if n.Metadata["resource_type"] != gcpgraph.ResourceTypeInstance {
		t.Errorf("metadata resource_type: got %q", n.Metadata["resource_type"])
	}
	if n.Metadata["region"] != "us-central1-a" {
		t.Errorf("metadata region: got %q", n.Metadata["region"])
	}
	if n.Metadata["status"] != "RUNNING" {
		t.Errorf("converter metadata was dropped: %v", n.Metadata)
	}
	if !strings.Contains(n.Summary, "vm") || !strings.Contains(n.Summary, gcpgraph.ResourceTypeInstance) {
		t.Errorf("default summary should name the type and the resource: got %q", n.Summary)
	}
}

func TestBuildKeepsAnExplicitSummary(t *testing.T) {
	nodes, _, err := gcpgraph.Build(gcpgraph.Result{
		Resources: []gcpgraph.Resource{{
			ID: "projects/p/topics/t", ResourceType: gcpgraph.ResourceTypePubSubTopic,
			Name: "t", Summary: "a hand-written summary",
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if nodes[0].Summary != "a hand-written summary" {
		t.Errorf("explicit summary was overwritten: got %q", nodes[0].Summary)
	}
}

func TestBuildSerializesRelationMetadataAsEvidence(t *testing.T) {
	_, edges, err := gcpgraph.Build(gcpgraph.Result{
		Relations: []gcpgraph.Relation{{
			From: "a", To: "b", Type: gcpgraph.EdgeGrants,
			Metadata: map[string]string{"role_name": "roles/viewer"},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	if edges[0].Evidence != `{"role_name":"roles/viewer"}` {
		t.Errorf("edge evidence: got %q", edges[0].Evidence)
	}
	if edges[0].Method != "gcp-collect" {
		t.Errorf("edge method: got %q, want gcp-collect", edges[0].Method)
	}
}

func TestBuildLeavesEvidenceEmptyWithoutRelationMetadata(t *testing.T) {
	_, edges, err := gcpgraph.Build(gcpgraph.Result{
		Relations: []gcpgraph.Relation{{From: "a", To: "b", Type: gcpgraph.EdgeBoundTo}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if edges[0].Evidence != "" || edges[0].Method != "" {
		t.Errorf("a relation with no metadata got evidence %q and method %q, want two empties",
			edges[0].Evidence, edges[0].Method)
	}
}

func TestBuildKeepsAnExplicitRelationMethod(t *testing.T) {
	_, edges, err := gcpgraph.Build(gcpgraph.Result{
		Relations: []gcpgraph.Relation{{
			From: "a", To: "b", Type: gcpgraph.EdgeTrusts, Method: "gcp-cross-project-trust",
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if edges[0].Method != "gcp-cross-project-trust" {
		t.Errorf("explicit method was overwritten: got %q", edges[0].Method)
	}
}

// TestBuildIsByteStableAcrossInputOrder is the CARRY-FORWARD SEAM's own row.
//
// The walk fans out over sub-collectors concurrently, so the order results are
// merged in is not stable between runs. A second collect of an unchanged project
// must produce the SAME output as the first, node for node and edge for edge, or
// the carry-forward diff sees a change where the source has none. Build is where
// that is guaranteed: it sorts.
func TestBuildIsByteStableAcrossInputOrder(t *testing.T) {
	forward := gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			{ID: "z-id", ResourceType: gcpgraph.ResourceTypeDisk, Name: "z"},
			{ID: "a-id", ResourceType: gcpgraph.ResourceTypeInstance, Name: "a"},
			{ID: "m-id", ResourceType: gcpgraph.ResourceTypeNetwork, Name: "m"},
		},
		Relations: []gcpgraph.Relation{
			{From: "z-id", To: "m-id", Type: gcpgraph.EdgeUsesNetwork},
			{From: "a-id", To: "m-id", Type: gcpgraph.EdgeUsesNetwork},
			{From: "a-id", To: "m-id", Type: gcpgraph.EdgeBoundTo},
		},
	}
	reversed := gcpgraph.Result{
		Resources: []gcpgraph.Resource{forward.Resources[2], forward.Resources[0], forward.Resources[1]},
		Relations: []gcpgraph.Relation{forward.Relations[2], forward.Relations[1], forward.Relations[0]},
	}

	nodesA, edgesA, err := gcpgraph.Build(forward)
	if err != nil {
		t.Fatalf("Build(forward): %v", err)
	}
	nodesB, edgesB, err := gcpgraph.Build(reversed)
	if err != nil {
		t.Fatalf("Build(reversed): %v", err)
	}

	if len(nodesA) != len(nodesB) || len(edgesA) != len(edgesB) {
		t.Fatalf("sizes differ: %d/%d nodes, %d/%d edges",
			len(nodesA), len(nodesB), len(edgesA), len(edgesB))
	}
	for i := range nodesA {
		if nodesA[i].ID != nodesB[i].ID {
			t.Fatalf("node %d differs between input orders: %q vs %q", i, nodesA[i].ID, nodesB[i].ID)
		}
	}
	for i := range edgesA {
		if edgesA[i] != edgesB[i] {
			t.Fatalf("edge %d differs between input orders: %+v vs %+v", i, edgesA[i], edgesB[i])
		}
	}
	// The known positive: the sort really did reorder, so an implementation that
	// simply returned its input in place could not pass this test by accident.
	if nodesA[0].ID != "a-id" {
		t.Errorf("nodes are not sorted by id: first is %q, want a-id", nodesA[0].ID)
	}
}

// TestBuildDedupesNodesByID pins the proxy case: two enumerations may both emit
// the same id, one having READ the resource and one having only REFERENCED it,
// and the id is the identity.
//
// THE READ ONE WINS, WHICHEVER ARRIVED FIRST. That is the whole point: the walk
// fans out concurrently, so "first wins" would make the surviving node depend on
// which goroutine finished, and two collects of an unchanged project would
// differ. Both orders are driven below.
func TestBuildDedupesNodesByIDKeepingTheResourceThatWasRead(t *testing.T) {
	read := gcpgraph.Resource{
		ID: "projects/p/notificationChannels/1", ResourceType: gcpgraph.ResourceTypeNotificationChannel,
		Name: "on call", Content: []byte(`{"name":"on call","fields":{"type":"email"}}`),
	}
	referenced := gcpgraph.Resource{
		ID: "projects/p/notificationChannels/1", ResourceType: gcpgraph.ResourceTypeNotificationChannel,
		Name:     "1",
		Metadata: map[string]string{"collected": "false", "collected_reason": "referenced by a policy"},
	}

	for _, tc := range []struct {
		name      string
		resources []gcpgraph.Resource
	}{
		{"read first", []gcpgraph.Resource{read, referenced}},
		{"referenced first", []gcpgraph.Resource{referenced, read}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, _, err := gcpgraph.Build(gcpgraph.Result{Resources: tc.resources})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if len(nodes) != 1 {
				t.Fatalf("got %d nodes for one id, want 1", len(nodes))
			}
			if nodes[0].Metadata["collected"] == "false" {
				t.Errorf("the placeholder won over the resource that was actually read; " +
					"which node survives must not depend on arrival order")
			}
			if nodes[0].SymbolName != "on call" {
				t.Errorf("the surviving node is %q, want the one that was read", nodes[0].SymbolName)
			}
		})
	}
}

// THE READ NODE WINS EVEN WHEN THE PLACEHOLDER IS BIGGER, and this is the only
// case that observes the first clause of the rule at all.
//
// In the fixtures above the two clauses agree: the node that was read is also
// the one carrying more content, so deleting the read-beats-referenced clause
// leaves them green and clause 2 decides everything. That is a rule with an
// unobserved half. Here they DISAGREE — the placeholder carries the longer body —
// so only the first clause can produce the right answer.
//
// IT IS NOT A CONTRIVED SHAPE. A placeholder is emitted by whichever converter
// happened to reference the resource, and what it carries is that converter's
// choice: a future one that stamped its whole referencing context into the
// placeholder would make it the longer of the two while still being the node
// that was never read. The rule must not depend on which of them is bigger.
func TestBuildPrefersTheResourceThatWasReadEvenWhenThePlaceholderIsLarger(t *testing.T) {
	read := gcpgraph.Resource{
		ID: "projects/p/notificationChannels/1", ResourceType: gcpgraph.ResourceTypeNotificationChannel,
		Name: "on call", Content: []byte(`{"name":"on call"}`),
	}
	fatPlaceholder := gcpgraph.Resource{
		ID: "projects/p/notificationChannels/1", ResourceType: gcpgraph.ResourceTypeNotificationChannel,
		Name: "1",
		// Deliberately LONGER than the read node's content.
		Content: []byte(`{"name":"1","fields":{"referenced_by":"projects/p/alertPolicies/1",` +
			`"reason":"named in a policy's notification list and not read directly"}}`),
		Metadata: map[string]string{"collected": "false", "collected_reason": "referenced by a policy"},
	}
	if len(fatPlaceholder.Content) <= len(read.Content) {
		t.Fatal("the fixture no longer inverts the two clauses; clause 2 would decide it and " +
			"clause 1 would go unobserved again")
	}

	for _, tc := range []struct {
		name      string
		resources []gcpgraph.Resource
	}{
		{"read first", []gcpgraph.Resource{read, fatPlaceholder}},
		{"placeholder first", []gcpgraph.Resource{fatPlaceholder, read}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, _, err := gcpgraph.Build(gcpgraph.Result{Resources: tc.resources})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if len(nodes) != 1 {
				t.Fatalf("got %d nodes for one id, want 1", len(nodes))
			}
			if nodes[0].Metadata[gcpgraph.MetadataCollected] == "false" {
				t.Error("the larger placeholder won over the resource that was actually read; " +
					"the rule is deciding on size rather than on whether the resource was read")
			}
			if nodes[0].SymbolName != "on call" {
				t.Errorf("the surviving node is %q, want the one that was read", nodes[0].SymbolName)
			}
		})
	}
}

// Two placeholders for one id collapse to one, deterministically.
func TestBuildDedupesTwoPlaceholdersForOneID(t *testing.T) {
	placeholder := func(reason string) gcpgraph.Resource {
		return gcpgraph.Resource{
			ID: "gcp:ar-remote:docker.io", ResourceType: gcpgraph.ResourceTypeARRemote,
			Name: "docker.io", Metadata: map[string]string{"collected": "false", "why": reason},
		}
	}
	forward, _, err := gcpgraph.Build(gcpgraph.Result{
		Resources: []gcpgraph.Resource{placeholder("a"), placeholder("b")},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	reverse, _, err := gcpgraph.Build(gcpgraph.Result{
		Resources: []gcpgraph.Resource{placeholder("b"), placeholder("a")},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(forward) != 1 || len(reverse) != 1 {
		t.Fatalf("got %d and %d nodes for one id, want 1 each", len(forward), len(reverse))
	}
	if forward[0].Metadata["why"] != reverse[0].Metadata["why"] {
		t.Errorf("which placeholder survives depends on arrival order: %q vs %q",
			forward[0].Metadata["why"], reverse[0].Metadata["why"])
	}
}

// TestBuildRefusesBadResources is the bad-input arm: a node with no id or no
// type is a defect in a converter, and the walk fails loudly naming it rather
// than emitting a node no edge can join and no consumer can classify.
func TestBuildRefusesBadResources(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resource gcpgraph.Resource
		wantIn   string
	}{
		{"no id", gcpgraph.Resource{ResourceType: gcpgraph.ResourceTypeDisk}, "empty id"},
		{"no type", gcpgraph.Resource{ID: "x"}, "empty resource type"},
		{
			"type outside the floor",
			gcpgraph.Resource{ID: "x", ResourceType: "gcp:invented:thing"},
			"not in the resource-type floor",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := gcpgraph.Build(gcpgraph.Result{Resources: []gcpgraph.Resource{tc.resource}})
			if err == nil {
				t.Fatalf("Build accepted %+v", tc.resource)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not name the condition %q", err, tc.wantIn)
			}
		})
	}
}

func TestBuildRefusesBadRelations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		relation gcpgraph.Relation
		wantIn   string
	}{
		{"no from", gcpgraph.Relation{To: "b", Type: gcpgraph.EdgeBoundTo}, "empty from_id"},
		{"no to", gcpgraph.Relation{From: "a", Type: gcpgraph.EdgeBoundTo}, "empty to_id"},
		{"no type", gcpgraph.Relation{From: "a", To: "b"}, "empty edge type"},
		{
			"type outside the floor",
			gcpgraph.Relation{From: "a", To: "b", Type: "INVENTED_EDGE"},
			"not in the edge-type floor",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := gcpgraph.Build(gcpgraph.Result{Relations: []gcpgraph.Relation{tc.relation}})
			if err == nil {
				t.Fatalf("Build accepted %+v", tc.relation)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not name the condition %q", err, tc.wantIn)
			}
		})
	}
}

// TestBuildEmitsEmptySlicesForAnEmptyWalk keeps the empty-walk cell honest at
// this layer too: an empty region is the first real run of a new collector.
func TestBuildEmitsEmptySlicesForAnEmptyWalk(t *testing.T) {
	nodes, edges, err := gcpgraph.Build(gcpgraph.Result{})
	if err != nil {
		t.Fatalf("Build(empty): %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("empty walk produced %d nodes and %d edges", len(nodes), len(edges))
	}
}

func TestResultAddMergesBothSlices(t *testing.T) {
	var got gcpgraph.Result
	got.Add(gcpgraph.Result{
		Resources: []gcpgraph.Resource{{ID: "a", ResourceType: gcpgraph.ResourceTypeDisk}},
		Relations: []gcpgraph.Relation{{From: "a", To: "b", Type: gcpgraph.EdgeBoundTo}},
	})
	got.Add(gcpgraph.Result{
		Resources: []gcpgraph.Resource{{ID: "b", ResourceType: gcpgraph.ResourceTypeInstance}},
	})
	if len(got.Resources) != 2 || len(got.Relations) != 1 {
		t.Errorf("Add: got %d resources and %d relations, want 2 and 1",
			len(got.Resources), len(got.Relations))
	}
}
