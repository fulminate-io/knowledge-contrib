// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// all_test.go — the COMPOSITION row and the shared fixture helpers.
//
// All is not a substitute for the per-resolver cases: it catches the one thing
// they cannot, a resolver that is written, tested and then never called.

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := gcpcontent.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling a fixture: %v", err)
	}
	return raw
}

type edgeKey struct{ from, to, edgeType string }

func edgeKeys(rels []gcpgraph.Relation) []edgeKey {
	out := make([]edgeKey, 0, len(rels))
	for _, r := range rels {
		out = append(out, edgeKey{r.From, r.To, r.Type})
	}
	return out
}

func hasEdge(rels []gcpgraph.Relation, from, to, edgeType string) bool {
	for _, r := range rels {
		if r.From == from && r.To == to && r.Type == edgeType {
			return true
		}
	}
	return false
}

func TestAllRunsEveryResolverOverOneWalk(t *testing.T) {
	in := gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			webInstance(t),
			firewallResource(t, "allow-https", gcpcontent.Firewall{
				Name: "allow-https", Network: vpc, Direction: "INGRESS",
				SourceRanges: []string{"0.0.0.0/0"}, Allowed: allowTCP443(),
			}),
			subnetResource(t, hostVPC),
			arRepo(t),
			runService(t, runSvcID, "api", "us-central1-docker.pkg.dev/proj-a/images/api:v1"),
			serviceAccount(t, foreignSA, "foreign@proj-b.iam.gserviceaccount.com"),
			{ID: sqlID, Name: "db", ResourceType: gcpgraph.ResourceTypeSQLInstance,
				Content: mustMarshal(t, gcpcontent.SQLInstance{IPAddresses: []string{"35.1.2.3"}})},
			recordSet(t, "A", []string{"35.1.2.3"}),
			groupResource(t),
		},
		Relations: []gcpgraph.Relation{
			grant(foreignSA, "roles/iam.serviceAccountTokenCreator"),
			{From: "roles/viewer", To: "group:team@example.com", Type: gcpgraph.EdgeGrants},
		},
	}
	got, err := resolve.All("proj-a", in)
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	for _, want := range []struct {
		what               string
		from, to, edgeType string
	}{
		{"firewall", vmWeb, gcpgraph.CIDRSentinelID("0.0.0.0/0"), gcpgraph.EdgeAllowsIngressFrom},
		{"shared vpc", hostVPC, localSubID, gcpgraph.EdgeSharedWith},
		{"image lineage", runSvcID, arRepoID, gcpgraph.EdgeUsesImage},
		{"cross-project trust", foreignSA, "projects/proj-a", gcpgraph.EdgeTrusts},
		{"dns targeting", recordID, sqlID, gcpgraph.EdgeRoutesTo},
		{"iam group resolution", "roles/viewer", groupID, gcpgraph.EdgeGrants},
	} {
		if !hasEdge(got.Relations, want.from, want.to, want.edgeType) {
			t.Errorf("All did not derive the %s edge (%s -> %s, %s)",
				want.what, want.from, want.to, want.edgeType)
		}
	}
	// The rewrite half survives composition: the placeholder is gone from the
	// final result, not merely absent from one resolver's return.
	if hasEdge(got.Relations, "roles/viewer", "group:team@example.com", gcpgraph.EdgeGrants) {
		t.Error("the raw group placeholder survived All")
	}
	// The CIDR sentinel node the firewall edges name is in the final result, so
	// no derived edge names an endpoint the walk does not carry.
	var sawSentinel bool
	for _, res := range got.Resources {
		if res.ID == gcpgraph.CIDRSentinelID("0.0.0.0/0") {
			sawSentinel = true
		}
	}
	if !sawSentinel {
		t.Error("All did not carry the CIDR sentinel node into the result")
	}
	if len(got.Resources) < len(in.Resources) {
		t.Errorf("All returned %d resources for a walk of %d; it must not drop enumerated ones",
			len(got.Resources), len(in.Resources))
	}
}

// TestAllDerivesTheFiveEdgeTypesNoEnumerationEmits is the requirement stated as
// its own row: the five types exist in the declared vocabulary only because
// these resolvers produce them, so a module that declares them and derives them
// never is exactly what this catches.
func TestAllDerivesTheFiveEdgeTypesNoEnumerationEmits(t *testing.T) {
	in := gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			webInstance(t),
			firewallResource(t, "in", gcpcontent.Firewall{
				Name: "in", Network: vpc, Direction: "INGRESS",
				SourceRanges: []string{"0.0.0.0/0"}, Allowed: allowTCP443(),
			}),
			firewallResource(t, "out", gcpcontent.Firewall{
				Name: "out", Network: vpc, Direction: "EGRESS",
				DestinationRanges: []string{"10.0.0.0/8"}, Allowed: allowTCP443(),
			}),
			subnetResource(t, hostVPC),
			arRepo(t),
			runService(t, runSvcID, "api", "us-central1-docker.pkg.dev/proj-a/images/api:v1"),
			serviceAccount(t, foreignSA, "foreign@proj-b.iam.gserviceaccount.com"),
		},
		Relations: []gcpgraph.Relation{grant(foreignSA, "roles/iam.serviceAccountUser")},
	}
	got, err := resolve.All("proj-a", in)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	emitted := map[string]bool{}
	for _, r := range got.Relations {
		emitted[r.Type] = true
	}
	for _, edgeType := range []string{
		gcpgraph.EdgeAllowsIngressFrom,
		gcpgraph.EdgeAllowsEgressTo,
		gcpgraph.EdgeSharedWith,
		gcpgraph.EdgeUsesImage,
		gcpgraph.EdgeTrusts,
	} {
		if !emitted[edgeType] {
			t.Errorf("the derived edge type %q was declared but never produced", edgeType)
		}
	}
}

// TestAllFailsLoudlyWhenAResolverCannotRead is the composition's own fail-loud
// arm: one unreadable resource fails the whole derivation rather than producing
// a quietly smaller graph.
func TestAllFailsLoudlyWhenAResolverCannotRead(t *testing.T) {
	_, err := resolve.All("proj-a", gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           "https://www.googleapis.com/compute/v1/projects/proj-a/global/firewalls/broken",
		Name:         "broken",
		ResourceType: gcpgraph.ResourceTypeFirewall,
		Content:      []byte("{not json"),
	}}})
	if err == nil {
		t.Fatal("All swallowed a resolver failure")
	}
}

// TestAllOnAnEmptyWalkIsEmpty is the same-run control for every zero the cases
// above assert: with nothing to read the composition derives nothing and errors
// on nothing, so a zero elsewhere is a measured zero.
func TestAllOnAnEmptyWalkIsEmpty(t *testing.T) {
	got, err := resolve.All("proj-a", gcpgraph.Result{})
	if err != nil {
		t.Fatalf("All(empty): %v", err)
	}
	if len(got.Relations) != 0 || len(got.Resources) != 0 {
		t.Errorf("an empty walk derived %d relations and %d resources",
			len(got.Relations), len(got.Resources))
	}
}
