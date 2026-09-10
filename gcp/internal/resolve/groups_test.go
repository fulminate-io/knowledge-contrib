// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// groups_test.go — the ONE resolver that REWRITES the walk instead of adding to
// it: an IAM binding naming a group placeholder is re-targeted onto the group's
// node AND the placeholder edge is dropped. Skipping the drop half leaves a
// permanently dangling endpoint that no membership assertion ever notices, so
// both halves are asserted in the same case.

const groupID = "groups/01abcdef"

const groupEmail = "team@example.com"

func groupResource(t *testing.T) gcpgraph.Resource {
	t.Helper()
	email := groupEmail
	return gcpgraph.Resource{
		ID: groupID, Name: email, ResourceType: gcpgraph.ResourceTypeCloudIdentityGroup,
		Metadata: map[string]string{"email": email},
		Content:  mustMarshal(t, gcpcontent.Generic{Name: email}),
	}
}

func TestIAMBindingGroupsRetargetsAndDropsThePlaceholder(t *testing.T) {
	got, err := resolve.IAMBindingGroups(gcpgraph.Result{
		Resources: []gcpgraph.Resource{groupResource(t)},
		Relations: []gcpgraph.Relation{{
			From: "roles/viewer", To: "group:team@example.com", Type: gcpgraph.EdgeGrants,
		}},
	})
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if !hasEdge(got.Relations, "roles/viewer", groupID, gcpgraph.EdgeGrants) {
		t.Fatalf("the grant was not re-targeted onto the group node: %v", edgeKeys(got.Relations))
	}
	if hasEdge(got.Relations, "roles/viewer", "group:team@example.com", gcpgraph.EdgeGrants) {
		t.Error("the raw group placeholder edge survived; it is a permanently dangling endpoint")
	}
	if len(got.Relations) != 1 {
		t.Errorf("got %d relations, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
	if got.Relations[0].Method != resolve.MethodIAMGroupResolve {
		t.Errorf("edge method: got %q, want %q", got.Relations[0].Method, resolve.MethodIAMGroupResolve)
	}
}

// The cell that closes the axis: a placeholder whose email is NOT in the
// collected group index is LEFT ALONE rather than dropped. Dropping it would
// delete the only record that the binding exists.
func TestIAMBindingGroupsLeavesAnUncollectedGroupAlone(t *testing.T) {
	got, err := resolve.IAMBindingGroups(gcpgraph.Result{
		Resources: []gcpgraph.Resource{groupResource(t)},
		Relations: []gcpgraph.Relation{{
			From: "roles/viewer", To: "group:other@example.com", Type: gcpgraph.EdgeGrants,
		}},
	})
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if !hasEdge(got.Relations, "roles/viewer", "group:other@example.com", gcpgraph.EdgeGrants) {
		t.Errorf("a placeholder for an uncollected group was dropped: %v", edgeKeys(got.Relations))
	}
	if len(got.Relations) != 1 {
		t.Errorf("got %d relations, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
}

// Every other relation passes through untouched, which is what makes this a
// rewrite of one shape rather than a filter over the whole walk.
func TestIAMBindingGroupsPassesEveryOtherRelationThrough(t *testing.T) {
	got, err := resolve.IAMBindingGroups(gcpgraph.Result{
		Resources: []gcpgraph.Resource{groupResource(t)},
		Relations: []gcpgraph.Relation{
			{From: "a", To: "b", Type: gcpgraph.EdgeBoundTo},
			{From: "roles/viewer", To: "group:team@example.com", Type: gcpgraph.EdgeGrants},
			{From: "c", To: "user:someone@example.com", Type: gcpgraph.EdgeGrants},
		},
	})
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if !hasEdge(got.Relations, "a", "b", gcpgraph.EdgeBoundTo) {
		t.Error("an unrelated edge was dropped")
	}
	if !hasEdge(got.Relations, "c", "user:someone@example.com", gcpgraph.EdgeGrants) {
		t.Error("a grant to a non-group principal was dropped")
	}
	if len(got.Relations) != 3 {
		t.Errorf("got %d relations, want 3: %v", len(got.Relations), edgeKeys(got.Relations))
	}
}

// A membership edge that already names the group node is not a placeholder and
// is not rewritten, so the resolver is keyed on the placeholder shape and not on
// the edge type alone.
func TestIAMBindingGroupsLeavesAnAlreadyResolvedGrantAlone(t *testing.T) {
	got, err := resolve.IAMBindingGroups(gcpgraph.Result{
		Resources: []gcpgraph.Resource{groupResource(t)},
		Relations: []gcpgraph.Relation{{
			From: "roles/viewer", To: groupID, Type: gcpgraph.EdgeGrants,
			Metadata: map[string]string{"role_name": "roles/viewer"},
		}},
	})
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if len(got.Relations) != 1 {
		t.Fatalf("got %d relations, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
	if got.Relations[0].Metadata["role_name"] != "roles/viewer" {
		t.Errorf("an already-resolved grant lost its metadata: %+v", got.Relations[0])
	}
	if got.Relations[0].Method == resolve.MethodIAMGroupResolve {
		t.Error("an already-resolved grant was restamped as a resolution")
	}
}

func TestIAMBindingGroupsDedupesRepeatedBindings(t *testing.T) {
	got, err := resolve.IAMBindingGroups(gcpgraph.Result{
		Resources: []gcpgraph.Resource{groupResource(t)},
		Relations: []gcpgraph.Relation{
			{From: "roles/viewer", To: "group:team@example.com", Type: gcpgraph.EdgeGrants},
			{From: "roles/viewer", To: "group:team@example.com", Type: gcpgraph.EdgeGrants},
		},
	})
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if len(got.Relations) != 1 {
		t.Errorf("two identical bindings produced %d relations, want 1: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

// With no groups collected there is nothing to resolve against, and the walk
// passes through unchanged rather than losing its placeholders.
func TestIAMBindingGroupsWithNoGroupsIsAPassThrough(t *testing.T) {
	got, err := resolve.IAMBindingGroups(gcpgraph.Result{
		Relations: []gcpgraph.Relation{{
			From: "roles/viewer", To: "group:team@example.com", Type: gcpgraph.EdgeGrants,
		}},
	})
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if len(got.Relations) != 1 ||
		!hasEdge(got.Relations, "roles/viewer", "group:team@example.com", gcpgraph.EdgeGrants) {
		t.Errorf("the walk did not pass through unchanged: %v", edgeKeys(got.Relations))
	}
}

// The resources the walk carries survive the rewrite: this resolver rewrites
// edges and touches no node.
func TestIAMBindingGroupsKeepsEveryResource(t *testing.T) {
	in := gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			groupResource(t),
			{ID: "projects/proj-a/topics/t", ResourceType: gcpgraph.ResourceTypePubSubTopic},
		},
	}
	got, err := resolve.IAMBindingGroups(in)
	if err != nil {
		t.Fatalf("IAMBindingGroups: %v", err)
	}
	if len(got.Resources) != len(in.Resources) {
		t.Errorf("got %d resources, want %d", len(got.Resources), len(in.Resources))
	}
}
