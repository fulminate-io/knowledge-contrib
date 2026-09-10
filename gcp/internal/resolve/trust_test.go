// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// trust_test.go — cross-project trust, which fires behind a TWO-ROLE PREDICATE.
//
// A module that emitted trust for every cross-project inbound grant would pass
// the positive case and over-emit on every real project, so the role axis and
// the project axis each get their own closing cell.

const (
	localSA   = "projects/proj-a/serviceAccounts/local@proj-a.iam.gserviceaccount.com"
	foreignSA = "projects/proj-a/serviceAccounts/foreign@proj-b.iam.gserviceaccount.com"
)

func serviceAccount(t *testing.T, id, email string) gcpgraph.Resource {
	t.Helper()
	return gcpgraph.Resource{
		ID: id, Name: email, ResourceType: gcpgraph.ResourceTypeServiceAccount,
		Metadata: map[string]string{"email": email},
		Content:  mustMarshal(t, gcpcontent.Generic{Name: email}),
	}
}

func grant(to, role string) gcpgraph.Relation {
	return gcpgraph.Relation{
		From: "roles-node", To: to, Type: gcpgraph.EdgeGrants,
		Metadata: map[string]string{"role_name": role},
	}
}

func TestCrossProjectTrustFiresOnAnImpersonationRole(t *testing.T) {
	for _, role := range []string{
		"roles/iam.serviceAccountTokenCreator",
		"roles/iam.serviceAccountUser",
	} {
		t.Run(role, func(t *testing.T) {
			got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
				Resources: []gcpgraph.Resource{
					serviceAccount(t, foreignSA, "foreign@proj-b.iam.gserviceaccount.com"),
				},
				Relations: []gcpgraph.Relation{grant(foreignSA, role)},
			})
			if err != nil {
				t.Fatalf("CrossProjectTrust: %v", err)
			}
			if !hasEdge(got.Relations, foreignSA, "projects/proj-a", gcpgraph.EdgeTrusts) {
				t.Fatalf("no TRUSTS from the foreign account to the local project: %v",
					edgeKeys(got.Relations))
			}
			if got.Relations[0].Method != resolve.MethodCrossProjectTrust {
				t.Errorf("edge method: got %q, want %q",
					got.Relations[0].Method, resolve.MethodCrossProjectTrust)
			}
		})
	}
}

// The cell that closes the ROLE axis.
func TestCrossProjectTrustIgnoresAnyOtherRole(t *testing.T) {
	got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			serviceAccount(t, foreignSA, "foreign@proj-b.iam.gserviceaccount.com"),
		},
		Relations: []gcpgraph.Relation{grant(foreignSA, "roles/viewer")},
	})
	if err != nil {
		t.Fatalf("CrossProjectTrust: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a non-impersonation role produced %d TRUSTS edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

// The cell that closes the PROJECT axis.
func TestCrossProjectTrustIgnoresASameProjectAccount(t *testing.T) {
	got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			serviceAccount(t, localSA, "local@proj-a.iam.gserviceaccount.com"),
		},
		Relations: []gcpgraph.Relation{grant(localSA, "roles/iam.serviceAccountTokenCreator")},
	})
	if err != nil {
		t.Fatalf("CrossProjectTrust: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a same-project account produced %d TRUSTS edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

// A principal that is not a service account at all — a human, a group — has no
// project in its email and never yields a trust edge.
func TestCrossProjectTrustIgnoresANonServiceAccountPrincipal(t *testing.T) {
	const humanID = "projects/proj-a/serviceAccounts/someone@example.com"
	got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{serviceAccount(t, humanID, "someone@example.com")},
		Relations: []gcpgraph.Relation{grant(humanID, "roles/iam.serviceAccountTokenCreator")},
	})
	if err != nil {
		t.Fatalf("CrossProjectTrust: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a non-service-account principal produced %d TRUSTS edges, want 0", len(got.Relations))
	}
}

// The role is read from the edge's own metadata, and falls back to the edge
// SOURCE when the metadata carries none — because the source id of an IAM
// binding edge IS the role string.
func TestCrossProjectTrustReadsTheRoleFromTheEdgeSourceWhenMetadataCarriesNone(t *testing.T) {
	got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			serviceAccount(t, foreignSA, "foreign@proj-b.iam.gserviceaccount.com"),
		},
		Relations: []gcpgraph.Relation{{
			From: "roles/iam.serviceAccountTokenCreator", To: foreignSA, Type: gcpgraph.EdgeGrants,
		}},
	})
	if err != nil {
		t.Fatalf("CrossProjectTrust: %v", err)
	}
	if !hasEdge(got.Relations, foreignSA, "projects/proj-a", gcpgraph.EdgeTrusts) {
		t.Fatalf("the role-as-edge-source fallback did not fire: %v", edgeKeys(got.Relations))
	}
}

// An account with no email metadata cannot be placed in a project, so it is not
// guessed at from the node id.
func TestCrossProjectTrustIgnoresAnAccountWithNoEmail(t *testing.T) {
	got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{{
			ID: foreignSA, Name: "foreign", ResourceType: gcpgraph.ResourceTypeServiceAccount,
		}},
		Relations: []gcpgraph.Relation{grant(foreignSA, "roles/iam.serviceAccountTokenCreator")},
	})
	if err != nil {
		t.Fatalf("CrossProjectTrust: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an account with no email produced %d TRUSTS edges, want 0", len(got.Relations))
	}
}

func TestCrossProjectTrustDedupesRepeatedGrants(t *testing.T) {
	got, err := resolve.CrossProjectTrust("proj-a", gcpgraph.Result{
		Resources: []gcpgraph.Resource{
			serviceAccount(t, foreignSA, "foreign@proj-b.iam.gserviceaccount.com"),
		},
		Relations: []gcpgraph.Relation{
			grant(foreignSA, "roles/iam.serviceAccountTokenCreator"),
			grant(foreignSA, "roles/iam.serviceAccountUser"),
		},
	})
	if err != nil {
		t.Fatalf("CrossProjectTrust: %v", err)
	}
	if len(got.Relations) != 1 {
		t.Errorf("two impersonation grants on one account produced %d TRUSTS edges, want 1: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}
