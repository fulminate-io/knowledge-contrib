// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// params_test.go — this module's own half of the tool's input, asserted where
// it is actually enforced: on a call over the wire, before the walk runs.

// TestParams_AKeyTheSchemaDoesNotAdmitIsRefusedBeforeTheWalk.
//
// THE REFUSAL IS THE POINT rather than the tolerance. An operator who
// misspells a params key has configured something this collector will not do,
// and a collector that accepted the call and ignored the key would run a walk
// the operator did not ask for and report it as the one they did.
func TestParams_AKeyTheSchemaDoesNotAdmitIsRefusedBeforeTheWalk(t *testing.T) {
	session := dialChild(t)

	refused, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": "sub-1", "params": map[string]any{"subscriptionid": "typo"}},
	})
	if err != nil {
		// A transport-level refusal is also a refusal.
		return
	}
	if !refused.IsError {
		t.Fatal("a params object carrying a key the schema does not admit was accepted")
	}

	// THE SAME-RUN KNOWN POSITIVE, through the identical path: the correctly
	// spelled key IS admitted, so the refusal above is the schema
	// discriminating rather than every params object failing.
	accepted, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": "sub-1", "params": map[string]any{"subscription_id": "sub-2"}},
	})
	if err != nil {
		t.Fatalf("calling collect with an admitted key: %v", err)
	}
	if accepted.IsError {
		t.Fatalf("the correctly spelled key was refused too, so the refusal above proves nothing: %v", accepted.Content)
	}
}

// TestParams_AWrongTypedValueIsRefused. The schema declares the concurrency
// bound as a number, and a string there is a configuration error rather than
// something to coerce — this repository's invariant is that bad input errors.
func TestParams_AWrongTypedValueIsRefused(t *testing.T) {
	session := dialChild(t)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": "sub-1", "params": map[string]any{"max_concurrency": "many"}},
	})
	if err != nil {
		return
	}
	if !res.IsError {
		t.Fatal("a params value of the wrong type was coerced rather than refused")
	}
}

// TestParams_SubscriptionOverrideReachesTheWalk. The key is declared because
// the walk reads it, and this is the row that shows it does.
func TestParams_SubscriptionOverrideReachesTheWalk(t *testing.T) {
	var walked string
	c := &Collector{
		newCredential: func(context.Context) (azureCredential, error) { return fakeCredential{}, nil },
		buildSubs: func(_ azureCredential, subscriptionID string) []subCollector {
			walked = subscriptionID
			return []subCollector{staticSub{name: "azure-vms"}}
		},
		lookupEnv: func(string) (string, bool) { return "", false },
	}
	if _, err := c.Walk(context.Background(), "from-the-id", Params{SubscriptionID: "from-the-params"}, framework.ForeignContext{}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if walked != "from-the-params" {
		t.Errorf("the walk ran against %q; the params key the schema declares was ignored", walked)
	}

	// The control: with no override, the collect id is what is walked.
	if _, err := c.Walk(context.Background(), "from-the-id", Params{}, framework.ForeignContext{}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if walked != "from-the-id" {
		t.Errorf("with no override the walk ran against %q rather than the collect id", walked)
	}
}

// TestParams_ConcurrencyOverrideReachesTheRunner, observed as the bound the
// runner actually applies rather than as a field that was read.
func TestParams_ConcurrencyOverrideReachesTheRunner(t *testing.T) {
	c := &Collector{}
	if got := c.concurrency(Params{MaxConcurrency: 1}); got != 1 {
		t.Fatalf("the caller's bound resolved to %d", got)
	}
	// And the runner honors a bound of one by running the subcollectors one at
	// a time, which is the observable behind the number.
	var live, peak atomic.Int32
	subs := []subCollector{
		watchingSub{live: &live, peak: &peak},
		watchingSub{live: &live, peak: &peak},
		watchingSub{live: &live, peak: &peak},
	}
	runSubCollectors(context.Background(), subs, 1)
	if got := peak.Load(); got != 1 {
		t.Errorf("with a bound of one, %d subcollectors ran at once", got)
	}
}

// TestToolSpec_NamesTheDefaultToolAndDescribesItself. The name is what a config
// entry's `tool` field defaults to, so a collector serving anything else would
// need every operator to override it.
func TestToolSpec_NamesTheDefaultToolAndDescribesItself(t *testing.T) {
	spec := New().Tool()
	if spec.Name != framework.DefaultToolName {
		t.Errorf("the served tool is named %q rather than %q", spec.Name, framework.DefaultToolName)
	}
	if !strings.Contains(strings.ToLower(spec.Description), "azure") {
		t.Errorf("the tool's description does not say what it walks: %q", spec.Description)
	}
}
