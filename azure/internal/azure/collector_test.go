// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector_test.go — the walk as the framework calls it: which subscription,
// which credential, and what completeness it asserts.

// fakeCredential stands in for the Azure default chain in every test that needs
// a credential but no Azure. It returns a token that would be refused by Azure
// and is never sent anywhere.
type fakeCredential struct{}

func (fakeCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test-token"}, nil
}

// testCollector builds a collector with both seams filled: a credential that
// needs no Azure, and a fixed set of subcollectors.
func testCollector(subs ...subCollector) *Collector {
	return &Collector{
		newCredential: func(context.Context) (azureCredential, error) { return fakeCredential{}, nil },
		buildSubs:     func(azureCredential, string) []subCollector { return subs },
		lookupEnv:     func(string) (string, bool) { return "", false },
	}
}

// TestWalk_AssertsCompleteWhenEverySubcollectorSucceeded.
func TestWalk_AssertsCompleteWhenEverySubcollectorSucceeded(t *testing.T) {
	c := testCollector(staticSub{name: "azure-vms", result: childWalkResult()})
	result, err := c.Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !result.Complete.IsAsserted() {
		t.Fatal("the walk asserted nothing about its completeness")
	}
	if !result.Complete.IsComplete() {
		t.Errorf("a walk with no failure asserted itself incomplete: %s", result.Complete.Reason())
	}
	if len(result.Nodes) != 1 || result.Nodes[0].ID != vmID {
		t.Errorf("the walk's node did not reach the result: %v", result.Nodes)
	}
	if len(result.Edges) != 1 {
		t.Errorf("the walk's edge did not reach the result: %v", result.Edges)
	}
}

// TestWalk_AssertsIncompleteAndKeepsWhatItGathered. This is the assertion the
// server's deletion phase reads: a partial walk claiming completeness would
// name every resource this run failed to read as deleted.
func TestWalk_AssertsIncompleteAndKeepsWhatItGathered(t *testing.T) {
	c := testCollector(
		staticSub{name: "azure-vms", result: childWalkResult()},
		staticSub{name: "azure-storage", err: errors.New("listing storage accounts: 429 Too Many Requests")},
	)
	result, err := c.Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a failed subcollector became a refused collect: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a walk with a failed subcollector asserted itself COMPLETE, which would arm the deletion of what it could not read")
	}
	if len(result.Nodes) != 1 {
		t.Errorf("the healthy subcollector's nodes were discarded: %v", result.Nodes)
	}
	reason := result.Complete.Reason()
	if !strings.Contains(reason, "azure-storage") || !strings.Contains(reason, "429") {
		t.Errorf("the incomplete reason does not name the failure an operator must act on: %s", reason)
	}
}

// TestWalk_RefusesWithNoSubscriptionAndWithNoCredential. Both are failures
// BEFORE the walk produced anything, so both are errors — which the framework
// turns into a refused collect that writes nothing — rather than an empty
// successful result.
func TestWalk_RefusesWithNoSubscriptionAndWithNoCredential(t *testing.T) {
	t.Run("no subscription", func(t *testing.T) {
		c := testCollector(staticSub{name: "azure-vms"})
		_, err := c.Walk(context.Background(), "", Params{}, framework.ForeignContext{})
		if err == nil {
			t.Fatal("a collect naming no subscription was accepted")
		}
		for _, want := range []string{"collect id", "subscription_id", envSubscriptionID} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not name %q: %v", want, err)
			}
		}
	})

	t.Run("no credential", func(t *testing.T) {
		c := testCollector(staticSub{name: "azure-vms"})
		c.newCredential = func(context.Context) (azureCredential, error) {
			return nil, errors.New("DefaultAzureCredential: failed to acquire a token")
		}
		_, err := c.Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
		if err == nil {
			t.Fatal("a collect with no usable credential was accepted")
		}
		if !strings.Contains(err.Error(), "DefaultAzureCredential") {
			t.Errorf("the refusal does not carry the chain's own error: %v", err)
		}
	})
}

// TestWrapCredentialError_NamesTheChainAndWhereToFixIt.
//
// THIS ASSERTS UNCONDITIONALLY, which the test it replaces could not. That one
// guarded its assertions behind an `if err != nil` over
// NewDefaultAzureCredential, which returns no error on a machine with no Azure
// configuration at all — the chain defers its failure to the token request — so
// the assertions never ran and the shipped wording could be replaced with the
// whole suite green.
func TestWrapCredentialError_NamesTheChainAndWhereToFixIt(t *testing.T) {
	cause := errors.New("DefaultAzureCredential: failed to acquire a token")
	err := wrapCredentialError(cause)
	if err == nil {
		t.Fatal("wrapping a credential failure produced no error")
	}

	// THE CLAIM ITSELF, which is what an operator reads first and what a
	// reworded refusal loses before it loses anything else.
	for _, want := range []string{"no usable credential", "default credential chain"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q, so it no longer states what failed: %v", want, err)
		}
	}
	// THE CHAIN, so an operator knows what was tried.
	for _, arm := range []string{
		"environment", "workload identity", "managed identity",
		"Azure CLI", "Azure Developer CLI", "Azure PowerShell",
	} {
		if !strings.Contains(err.Error(), arm) {
			t.Errorf("the refusal does not name the %q arm of the chain: %v", arm, err)
		}
	}
	// WHERE THE FIX IS, because the answer is a config entry rather than
	// anything on the machine running the collector.
	for _, want := range []string{"config entry", "env block"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say where the fix is (%q): %v", want, err)
		}
	}
	// AND THE CAUSE SURVIVES, so the SDK's own diagnosis is not replaced by
	// this collector's framing of it.
	if !errors.Is(err, cause) {
		t.Errorf("the wrapped error does not carry the credential failure it came from: %v", err)
	}
	if !strings.Contains(err.Error(), cause.Error()) {
		t.Errorf("the wrapped error does not print the cause: %v", err)
	}

	// NO VALUE IS REPORTED, only the fact of absence: a credential assertion
	// names a non-reversible property and never a secret. The refusal is built
	// from the cause alone, so there is nothing for it to leak, and this row
	// says so by checking it carries no environment value.
	if strings.Contains(err.Error(), "=") {
		t.Errorf("the refusal renders something that reads as a variable assignment: %v", err)
	}
}

// TestCredential_TheSeamsFailureIsNotSwallowed. The other half, and the one the
// replaced test did observe: whatever produces the credential, its failure
// reaches the caller rather than becoming a walk with no credential.
func TestCredential_TheSeamsFailureIsNotSwallowed(t *testing.T) {
	c := &Collector{newCredential: func(context.Context) (azureCredential, error) {
		return nil, errors.New("no credential")
	}}
	if _, err := c.credential(context.Background()); err == nil {
		t.Fatal("the seam's failure was swallowed")
	}
}

// TestResolveSubscriptionID_EveryArm, including the one where nothing names a
// subscription, which the framework's own id guard makes unreachable through
// the tool and which is reachable here because the function takes its lookup.
func TestResolveSubscriptionID_EveryArm(t *testing.T) {
	env := func(name string) (string, bool) {
		if name == envSubscriptionID {
			return "from-environment", true
		}
		return "", false
	}
	empty := func(string) (string, bool) { return "", false }
	blank := func(string) (string, bool) { return "", true }

	for _, tc := range []struct {
		name   string
		id     string
		params Params
		lookup func(string) (string, bool)
		want   string
	}{
		{"params win over everything", "from-id", Params{SubscriptionID: "from-params"}, env, "from-params"},
		{"the collect id is the ordinary case", "from-id", Params{}, env, "from-id"},
		{"the environment is the fallback", "", Params{}, env, "from-environment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSubscriptionID(tc.id, tc.params, tc.lookup)
			if err != nil {
				t.Fatalf("resolveSubscriptionID: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolved %q, expected %q", got, tc.want)
			}
		})
	}

	if _, err := resolveSubscriptionID("", Params{}, empty); err == nil {
		t.Error("nothing named a subscription and the walk was allowed to start")
	}
	// A variable SET TO EMPTY names no subscription either: an operator who
	// declared the name and left it blank has not named one, and treating the
	// empty string as a subscription would walk a graph instance called "".
	if _, err := resolveSubscriptionID("", Params{}, blank); err == nil {
		t.Error("an empty environment value was accepted as a subscription")
	}
}

// TestWalk_IsDeterministicAcrossTwoIdenticalCollects. A second collect of an
// unchanged subscription must produce the same node set, edge set and
// assertion, or every collect would look like a change to whatever consumes it.
func TestWalk_IsDeterministicAcrossTwoIdenticalCollects(t *testing.T) {
	build := func() *Collector {
		return testCollector(
			staticSub{name: "azure-vms", result: subResult{
				resources: []resource{fx{t}.res(vmResource(virtualMachine(vmID, "vm1")))},
				edges:     vmEdges(virtualMachine(vmID, "vm1"), map[string][]string{strings.ToLower(nicID): {subnetID}}),
			}},
			staticSub{name: "azure-nsgs", result: subResult{
				resources: []resource{fx{t}.res(nsgResource(securityGroup()))},
			}},
			staticSub{name: "azure-keyvault", result: subResult{
				resources: []resource{fx{t}.res(vaultResource(keyVault()))},
				edges:     fx{t}.edges(vaultEdges(keyVault())),
			}},
		)
	}

	first, err := build().Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	for range 5 {
		next, err := build().Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("repeat collect: %v", err)
		}
		if !reflect.DeepEqual(first.Nodes, next.Nodes) {
			t.Fatalf("two collects of one unchanged subscription produced different nodes")
		}
		if !reflect.DeepEqual(first.Edges, next.Edges) {
			t.Fatalf("two collects of one unchanged subscription produced different edges")
		}
		if first.Complete.IsComplete() != next.Complete.IsComplete() {
			t.Fatal("two collects of one unchanged subscription asserted different completeness")
		}
	}
	if len(first.Nodes) == 0 || len(first.Edges) == 0 {
		t.Fatal("the walk produced nothing, so the comparison above proves nothing")
	}
}

// TestWalk_ResolversRunOverTheMergedOutputOfEverySubcollector. The
// relationships they draw need facts from two services, so a resolver that ran
// per subcollector would see neither.
func TestWalk_ResolversRunOverTheMergedOutputOfEverySubcollector(t *testing.T) {
	site := fx{t}.res(siteResource(webSite(), false))
	registry := fx{t}.res(registryResource(containerRegistry()))

	c := testCollector(
		staticSub{name: "azure-appservice", result: subResult{resources: []resource{site}}},
		staticSub{name: "azure-containerregistry", result: subResult{resources: []resource{registry}}},
	)
	result, err := c.Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var found bool
	for _, e := range result.Edges {
		if e.Type == edgeUsesImage && e.FromID == webSiteID && e.ToID == registryID {
			found = true
		}
	}
	if !found {
		t.Errorf("the image-lineage relationship was not drawn across two subcollectors: %v", result.Edges)
	}

	// The control: with the registry's subcollector absent, the same walk
	// draws no lineage, which is what shows the edge came from the merge.
	alone := testCollector(staticSub{name: "azure-appservice", result: subResult{resources: []resource{site}}})
	only, err := alone.Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, e := range only.Edges {
		if e.Type == edgeUsesImage {
			t.Error("lineage was drawn with no registry in the walk")
		}
	}
}

// TestWalk_ConcurrencyComesFromTheParamsThenTheDefault.
func TestWalk_ConcurrencyComesFromTheParamsThenTheDefault(t *testing.T) {
	c := &Collector{}
	if got := c.concurrency(Params{}); got != defaultConcurrency {
		t.Errorf("with nothing set the degree is %d, expected the built-in %d", got, defaultConcurrency)
	}
	if got := c.concurrency(Params{MaxConcurrency: 3}); got != 3 {
		t.Errorf("the caller's degree was ignored: %d", got)
	}
	// A negative or zero degree is not a degree: it falls back rather than
	// producing a runner that can start nothing.
	if got := c.concurrency(Params{MaxConcurrency: -4}); got != defaultConcurrency {
		t.Errorf("a negative degree resolved to %d", got)
	}
}

// TestEdgeEvidence_IsDeterministic. Go map iteration is randomized, so an
// evidence string whose key order changed between two identical collects would
// read downstream as a changed edge.
func TestEdgeEvidence_IsDeterministic(t *testing.T) {
	e := edge{
		from: identityA, to: scopeID, relation: edgeAssumesRole,
		metadata: map[string]string{
			mdSource: "rbac", mdRoleDefID: "role", mdPrincipalType: "Guest",
			"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
		},
	}
	first := e.contractEdge().Evidence
	for range 50 {
		if got := e.contractEdge().Evidence; got != first {
			t.Fatalf("two encodings of one edge's evidence differ:\n%s\n%s", first, got)
		}
	}
	if !strings.Contains(first, `"principal_type":"Guest"`) {
		t.Errorf("the evidence does not carry the key the resolvers gate on: %s", first)
	}

	// An edge with NO metadata carries no evidence and no method, rather than
	// an empty object a consumer would have to decode.
	bare := edge{from: vmID, to: subnetID, relation: edgeUsesSubnet}.contractEdge()
	if bare.Evidence != "" || bare.Method != "" {
		t.Errorf("an edge with no metadata carries evidence %q and method %q", bare.Evidence, bare.Method)
	}
}

// refusingCredential is a credential that cannot acquire a token, which is what
// a subscription-scoped credential does when asked for directory access.
type refusingCredential struct{}

func (refusingCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, errors.New("AADSTS700016: the application was not found in the directory")
}
