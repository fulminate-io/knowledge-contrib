// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/walk"
)

// params_test.go — the collect id and the two caps, on every value a correctly
// typed call can carry.
//
// A VALUE OF THE WRONG KIND NEVER REACHES THIS CODE. The framework advertises
// this params type's inferred schema and the SDK validates a call against it
// before the walk runs, so `"max_runs": "ten"` is refused by the transport. That
// arm is asserted over the real transport in serve_test.go; what is asserted here
// is every value that IS an integer and is still wrong.

// TestAnOmittedCapTakesTheDefault, which is what a collect with no params does.
func TestAnOmittedCapTakesTheDefault(t *testing.T) {
	recorder := &recordingAPI{}
	collector := walk.Collector{API: recorder.build}

	if _, err := collector.Walk(context.Background(), "acme", walk.Params{},
		framework.ForeignContext{}); err != nil {
		t.Fatalf("a collect with no params was refused: %v", err)
	}
	if recorder.runsPerPage != collect.DefaultMaxRuns {
		t.Errorf("an omitted max_runs asked the provider for %d per page, want the default of %d",
			recorder.runsPerPage, collect.DefaultMaxRuns)
	}
	if recorder.deploymentsPerPage != collect.DefaultMaxDeployments {
		t.Errorf("an omitted max_deployments asked for %d per page, want the default of %d",
			recorder.deploymentsPerPage, collect.DefaultMaxDeployments)
	}
}

// TestEveryAdmittedCapValueIsHonoredExactly, including the boundary.
//
// EXACTLY 100 IS THE CELL THAT MUST NOT BE OFF BY ONE. It is the provider's own
// page maximum, so it is the largest value that can be honored — and a refusal
// written as `>= ceiling` rather than `> ceiling` would refuse the one value at
// the boundary while every other test passed.
func TestEveryAdmittedCapValueIsHonoredExactly(t *testing.T) {
	for _, value := range []int{1, 7, 99, 100} {
		recorder := &recordingAPI{}
		collector := walk.Collector{API: recorder.build}
		if _, err := collector.Walk(context.Background(), "acme",
			walk.Params{MaxRuns: &value, MaxDeployments: &value},
			framework.ForeignContext{}); err != nil {
			t.Errorf("max_runs=%d was refused: %v", value, err)
			continue
		}
		if recorder.runsPerPage != value {
			t.Errorf("max_runs=%d asked the provider for %d per page", value, recorder.runsPerPage)
		}
		if recorder.deploymentsPerPage != value {
			t.Errorf("max_deployments=%d asked for %d per page", value, recorder.deploymentsPerPage)
		}
	}
}

// TestACapAboveTheCeilingIsRefusedByNameAndNothingIsWalked is the bad-input row.
//
// REFUSED RATHER THAN CLAMPED. These two enumerations read a single page, so a
// request for 500 would be answered with 100 and reported as a successful collect
// of 500 — the caller would be told it got five times what it did.
func TestACapAboveTheCeilingIsRefusedByNameAndNothingIsWalked(t *testing.T) {
	for name, params := range map[string]walk.Params{
		"max_runs":        {MaxRuns: new(101)},
		"max_deployments": {MaxDeployments: new(500)},
	} {
		recorder := &recordingAPI{}
		collector := walk.Collector{API: recorder.build}

		_, err := collector.Walk(context.Background(), "acme", params, framework.ForeignContext{})
		if err == nil {
			t.Errorf("%s above the ceiling was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name the parameter: %v", err)
		}
		if !strings.Contains(err.Error(), "100") {
			t.Errorf("the refusal does not name the ceiling: %v", err)
		}
		if recorder.built {
			t.Errorf("%s: the provider was dialed before the parameters were validated", name)
		}
	}
}

// TestACapBelowOneIsRefusedByName. Zero items to read is not a thing to collect,
// and a plain int would have made it indistinguishable from an omitted value.
func TestACapBelowOneIsRefusedByName(t *testing.T) {
	for _, value := range []int{0, -1} {
		recorder := &recordingAPI{}
		collector := walk.Collector{API: recorder.build}
		_, err := collector.Walk(context.Background(), "acme", walk.Params{MaxRuns: &value},
			framework.ForeignContext{})
		if err == nil {
			t.Errorf("max_runs=%d was accepted", value)
			continue
		}
		if !strings.Contains(err.Error(), "max_runs") {
			t.Errorf("the refusal does not name the parameter: %v", err)
		}
		if recorder.built {
			t.Error("the provider was dialed before the parameters were validated")
		}
	}
}

// TestTheCollectIDIsValidatedOnceAndNamesWhatIsWrong. The id is interpolated into
// every request path AND into every node id, so a malformed one reaches the
// caller as seven per-enumeration errors or as a walk that looked empty.
func TestTheCollectIDIsValidatedOnceAndNamesWhatIsWrong(t *testing.T) {
	for _, row := range []struct{ id, tell string }{
		{"", "no organization"},
		{"   ", "no organization"},
		{"acme/api", "letters, digits and hyphens"},
		{"https://github.com/acme", "letters, digits and hyphens"},
		{"acme_corp", "letters, digits and hyphens"},
		{"-acme", "hyphen"},
		{"acme-", "hyphen"},
		{strings.Repeat("a", 40), "at most 39"},
	} {
		recorder := &recordingAPI{}
		collector := walk.Collector{API: recorder.build}

		_, err := collector.Walk(context.Background(), row.id, walk.Params{},
			framework.ForeignContext{})
		if err == nil {
			t.Errorf("the collect id %q was accepted", row.id)
			continue
		}
		if !strings.Contains(err.Error(), row.tell) {
			t.Errorf("the refusal of %q does not say %q: %v", row.id, row.tell, err)
		}
		if recorder.built {
			t.Errorf("the provider was dialed for the malformed id %q", row.id)
		}
	}
}

// TestAWellFormedIDIsAccepted is the previous row's known positive: a validator
// that refused everything would satisfy it otherwise.
func TestAWellFormedIDIsAccepted(t *testing.T) {
	for _, id := range []string{"acme", "acme-corp", "Acme2", " acme ", strings.Repeat("a", 39)} {
		recorder := &recordingAPI{}
		collector := walk.Collector{API: recorder.build}
		if _, err := collector.Walk(context.Background(), id, walk.Params{},
			framework.ForeignContext{}); err != nil {
			t.Errorf("the well-formed id %q was refused: %v", id, err)
		}
	}
}

// TestTheOrganizationIsTrimmedBeforeItIsUsed. A trailing space in a config file
// or a shell substitution is the ordinary way this arrives.
func TestTheOrganizationIsTrimmedBeforeItIsUsed(t *testing.T) {
	recorder := &recordingAPI{}
	collector := walk.Collector{API: recorder.build}
	got, err := collector.Walk(context.Background(), "  acme  ", walk.Params{},
		framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	for _, node := range got.Nodes {
		if strings.Contains(node.ID, " ") {
			t.Errorf("a node id carries the untrimmed organization: %q", node.ID)
		}
	}
}
