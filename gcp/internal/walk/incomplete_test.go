// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/walk"
)

// incomplete_test.go — WHAT THE WALK DOES WHEN AN ENUMERATION DID NOT SEE ITS
// WHOLE SCOPE, which is the one family of behavior in this package where getting
// it wrong destroys data rather than merely reporting it badly.
//
// Three outcomes reach the completeness verdict and they count equally, because
// in every one of them this run did not see all of the project: an enumeration
// that FAILED, one the provider REFUSED, and one the provider answered only IN
// PART. What separates them is the REASON, because an operator fixes them
// differently, and what they share is that a COMPLETE assertion would let the
// receiving server treat what the walk did not carry as gone.
//
// The cases live in their own file because they are one subject read together;
// the file they were split from carries the walk's ordinary behavior.

// A REFUSED enumeration makes the walk incomplete, and this is the row that
// separates data loss from an honest partial read.
//
// A refusal is not a failure: the API answered, and it answered "not for you".
// Reporting it as a clean empty enumeration would let the walk assert COMPLETE,
// and a complete walk is what lets the receiving server treat everything the
// walk did not carry as gone. A role revoked between two collects would then
// delete a whole service's resources with the second collect reporting success.
func TestARefusedEnumerationMakesTheWalkIncomplete(t *testing.T) {
	good := staticSub("good", gcpgraph.Result{Resources: []gcpgraph.Resource{
		{ID: "id-a", Name: "a", ResourceType: gcpgraph.ResourceTypeInstance},
	}})
	refused := collect.Subcollector{
		Name: "caches",
		Run: func(context.Context, string) (gcpgraph.Result, error) {
			return gcpgraph.Result{}, fmt.Errorf("caches: %w: forbidden", collect.ErrDenied)
		},
	}

	got, err := walk.Collector{Enumerations: fixedEnumerations(good, refused)}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a refused enumeration failed the whole walk: %v", err)
	}
	if got.Complete.IsComplete() {
		t.Fatal("a walk containing a REFUSED enumeration asserted COMPLETE; the server " +
			"would treat everything that enumeration would have carried as deleted")
	}
	if !strings.Contains(got.Complete.Reason(), "caches") {
		t.Errorf("the incomplete reason does not name the refused enumeration: %q",
			got.Complete.Reason())
	}
	// The reason distinguishes the two causes, because an operator fixes a
	// refusal by granting a role and a failure by looking at a service.
	if !strings.Contains(got.Complete.Reason(), "refused") {
		t.Errorf("the incomplete reason does not say the enumeration was refused rather than "+
			"broken: %q", got.Complete.Reason())
	}
	// Everything else still lands.
	if len(got.Nodes) != 1 {
		t.Errorf("the successful enumeration's nodes were dropped: got %d", len(got.Nodes))
	}
}

// A PARTIALLY READ enumeration makes the walk incomplete on the same terms as a
// refusal, and this is the second half of the same data-loss story.
//
// A refusal is about the credential. A partial read is about the provider: it
// answered, and said part of the answer is missing. The walk cares about the one
// thing they share — this run did not see part of the project — so it may not
// claim to have seen all of it. Treating a partial read as clean is what lets
// the next full-replace collect delete a zone that was merely unreachable.
func TestAPartiallyReadEnumerationMakesTheWalkIncomplete(t *testing.T) {
	good := staticSub("good", gcpgraph.Result{Resources: []gcpgraph.Resource{
		{ID: "id-a", Name: "a", ResourceType: gcpgraph.ResourceTypeInstance},
	}})
	partial := collect.Subcollector{
		Name: "instances",
		Run: func(context.Context, string) (gcpgraph.Result, error) {
			return gcpgraph.Result{Resources: []gcpgraph.Resource{
					{ID: "id-b", Name: "b", ResourceType: gcpgraph.ResourceTypeDisk},
				}},
				fmt.Errorf("instances: %w: the provider did not read zones/europe-west1-b",
					collect.ErrPartial)
		},
	}

	got, err := walk.Collector{Enumerations: fixedEnumerations(good, partial)}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a partially read enumeration failed the whole walk: %v", err)
	}
	if got.Complete.IsComplete() {
		t.Fatal("a walk containing a PARTIALLY READ enumeration asserted COMPLETE; the next " +
			"full-replace collect would delete the scope the provider merely could not reach")
	}
	if !strings.Contains(got.Complete.Reason(), "zones/europe-west1-b") {
		t.Errorf("the incomplete reason does not name the unread scope: %q", got.Complete.Reason())
	}
	// The three causes are kept apart in the reason, because the operator's next
	// action differs: a partial read is usually the provider's and transient.
	if !strings.Contains(got.Complete.Reason(), "answered only in part") {
		t.Errorf("the reason does not distinguish a partial read from a refusal or a failure: %q",
			got.Complete.Reason())
	}
	// Both enumerations' resources land: a partial answer is still an answer.
	if len(got.Nodes) != 2 {
		t.Errorf("got %d nodes, want 2 — the partial enumeration's own page was dropped", len(got.Nodes))
	}
}

// Every enumeration partially read is an incomplete walk, not an error: the
// provider answered every time.
func TestEveryEnumerationPartiallyReadIsAnIncompleteWalkNotAnError(t *testing.T) {
	partial := func(name string) collect.Subcollector {
		return collect.Subcollector{
			Name: name,
			Run: func(context.Context, string) (gcpgraph.Result, error) {
				return gcpgraph.Result{}, fmt.Errorf("%s: %w: unreachable", name, collect.ErrPartial)
			},
		}
	}
	got, err := walk.Collector{Enumerations: fixedEnumerations(partial("a"), partial("b"))}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a wholly partial project failed the collect: %v", err)
	}
	if got.Complete.IsComplete() {
		t.Fatal("a project the provider answered only in part was reported as a complete walk")
	}
}

// A refusal that still returned a partial page keeps the page. An aggregated
// enumeration commonly refuses one scope out of dozens.
func TestARefusedEnumerationKeepsWhatItAlreadyRead(t *testing.T) {
	partial := collect.Subcollector{
		Name: "disks",
		Run: func(context.Context, string) (gcpgraph.Result, error) {
			return gcpgraph.Result{Resources: []gcpgraph.Resource{
					{ID: "id-d", Name: "d", ResourceType: gcpgraph.ResourceTypeDisk},
				}},
				fmt.Errorf("disks: %w: one zone refused", collect.ErrDenied)
		},
	}
	got, err := walk.Collector{Enumerations: fixedEnumerations(partial)}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Errorf("the partially refused enumeration's page was discarded: got %d nodes",
			len(got.Nodes))
	}
	if got.Complete.IsComplete() {
		t.Error("a partial refusal asserted a complete walk")
	}
}

// Every enumeration being REFUSED is not the same as every one FAILING. The
// collect succeeded and the answer is "you may read nothing here", which is a
// real and reportable state rather than a broken run.
func TestEveryEnumerationRefusedIsAnIncompleteWalkNotAnError(t *testing.T) {
	refused := func(name string) collect.Subcollector {
		return collect.Subcollector{
			Name: name,
			Run: func(context.Context, string) (gcpgraph.Result, error) {
				return gcpgraph.Result{}, fmt.Errorf("%s: %w: forbidden", name, collect.ErrDenied)
			},
		}
	}
	got, err := walk.Collector{Enumerations: fixedEnumerations(refused("a"), refused("b"))}.
		Walk(t.Context(), "gcp-instance", walk.Params{Project: "proj-a"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a fully refused project failed the collect: %v", err)
	}
	if got.Complete.IsComplete() {
		t.Fatal("a project this credential may read nothing of was reported as a complete walk")
	}
	if len(got.Nodes) != 0 {
		t.Errorf("got %d nodes from a fully refused project", len(got.Nodes))
	}
}
