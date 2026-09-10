// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// environments_test.go — THE DEPLOYMENT ENVIRONMENTS AND THE APPROVAL GATE A
// RESTRICTED ONE IMPLIES. Split out of enumerations_test.go, whose recorded
// workspace these rows walk.

// TestAnAdminOnlyEnvironmentMintsItsGateFromTheLiveRestrictionsShape is the
// admin-only arm of the gate condition, driven by the restrictions object the
// provider really sends.
//
// IT IS THE ARM WITH NO OTHER WAY IN. The `review` environment's lock is OPEN —
// `deployment_environment_lock_open`, which is what a live unlocked environment
// reports — so nothing but `restrictions.admin_only` can have minted its gate.
// That makes this row the one observation of a boolean the module used to
// declare as a slice: with the field read as anything but a bool, the whole
// environments page fails to decode, and with the arm removed, the gate is gone
// while the lock-armed one beside it still stands.
func TestAnAdminOnlyEnvironmentMintsItsGateFromTheLiveRestrictionsShape(t *testing.T) {
	got := walkTheFixture(t)

	review, ok := resourceByID(got, "bitbucket:acme/Environment/api/review")
	if !ok {
		t.Fatal("no node for the admin-only environment")
	}
	gate, ok := resourceByID(got, "bitbucket:acme/ApprovalGate/api/review")
	if !ok {
		t.Fatal("the admin-only environment minted no approval gate. Its lock is open, so the " +
			"restriction is the only thing that could have")
	}
	if !hasRelation(got, review.ID, gate.ID, bbgraph.EdgeRequiresApproval) {
		t.Error("no REQUIRES_APPROVAL edge from the admin-only environment to its gate")
	}
	// THE CONTROL, in the same walk: an environment whose lock is open the same
	// way and whose restriction is false mints nothing. Without it this row would
	// pass against a converter that gated every environment it saw.
	if _, minted := resourceIDs(got)["bitbucket:acme/ApprovalGate/api/staging"]; minted {
		t.Error("the environment with an open lock and no restriction minted a gate anyway")
	}
}

// TestTheEnvironmentAndItsGate is the environments converter, both arms of the
// gate condition.
func TestTheEnvironmentAndItsGate(t *testing.T) {
	got := walkTheFixture(t)

	production, ok := resourceByID(got, "bitbucket:acme/Environment/api/production")
	if !ok {
		t.Fatal("no node for the locked environment")
	}
	for key, want := range map[string]string{
		"workspace":        "acme",
		"repo":             "api",
		"environment_type": "Production",
		"rank":             "1",
	} {
		if production.Metadata[key] != want {
			t.Errorf("the production environment's %q is %q, want %q",
				key, production.Metadata[key], want)
		}
	}
	gate, ok := resourceByID(got, "bitbucket:acme/ApprovalGate/api/production")
	if !ok {
		t.Fatal("the locked environment minted no approval gate")
	}
	if gate.Content != "" {
		t.Errorf("the approval gate carries content %q; the source provider stores none", gate.Content)
	}
	if !hasRelation(got, production.ID, gate.ID, bbgraph.EdgeRequiresApproval) {
		t.Error("no REQUIRES_APPROVAL edge from the environment to its gate")
	}

	// THE OTHER ARM. The unlocked environment mints NO gate, and its two
	// conditional keys are absent rather than empty.
	staging, ok := resourceByID(got, "bitbucket:acme/Environment/api/staging")
	if !ok {
		t.Fatal("no node for the unlocked environment")
	}
	for _, absent := range []string{"environment_type", "rank"} {
		if value, present := staging.Metadata[absent]; present {
			t.Errorf("the unlocked environment carries %q anyway: %q", absent, value)
		}
	}
	if _, minted := resourceIDs(got)["bitbucket:acme/ApprovalGate/api/staging"]; minted {
		t.Error("an environment with no lock and no restriction minted an approval gate")
	}
}
