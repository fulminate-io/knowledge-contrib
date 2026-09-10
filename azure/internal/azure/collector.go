// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector.go — the framework-facing surface: the tool this collector serves,
// its params, and the walk the framework calls.

// azureCredential is what every subcollector authenticates with. It is an
// ALIAS rather than a new interface so an SDK client takes it directly; the
// name exists because "the credential this collector holds" is what the seams
// below are about, and the SDK's own name for it says nothing about that.
type azureCredential = azcore.TokenCredential

// toolDescription is what an operator reads in an MCP tool listing.
const toolDescription = "Walk an Azure subscription and return its resources and relationships as a knowledge graph."

// envSubscriptionID is the one environment variable this collector itself
// reads. Everything else in the entry's env block is read by the Azure SDK or
// by the Go runtime beneath it.
const envSubscriptionID = "AZURE_SUBSCRIPTION_ID"

// Params is this collector's half of the tool's input schema. The framework
// infers its JSON Schema and splices it into the contract's input document, so
// every field here is advertised to the caller and validated before the walk.
//
// NO FIELD HERE IS A CREDENTIAL, and that is a property rather than an
// oversight: credentials reach this process through the Azure default chain
// reading the entry's env block, and the client passes none. A credential
// property on this schema would invite an operator to put a secret in a collect
// call, where it would travel through the daemon's tool arguments.
//
// EVERY FIELD IS OPTIONAL. A required params key is refused for every collect
// that does not carry it, and the client omits params entirely when a collect
// names none, so a required field here would refuse the ordinary collect before
// the walk ran.
type Params struct {
	// SubscriptionID overrides which subscription this collect walks. It is
	// for the operator whose collect id names a graph instance by some other
	// convention; ordinarily the collect id IS the subscription.
	SubscriptionID string `json:"subscription_id,omitempty" jsonschema:"the Azure subscription id to walk; defaults to the collect id, then to AZURE_SUBSCRIPTION_ID"`
	// MaxConcurrency bounds how many of this collector's per-service walks run
	// at once. Zero means the built-in degree.
	MaxConcurrency int `json:"max_concurrency,omitempty" jsonschema:"how many per-service walks run at once; 0 means the built-in bound"`
}

// Collector is the Azure collector. Its two seams — the credential builder and
// the subcollector builder — are unexported fields left nil in the shipped
// binary and set by tests, so a test drives the whole walk without a
// subscription, and no test hook is reachable from a collector an operator
// installs.
type Collector struct {
	newCredential    func(ctx context.Context) (azureCredential, error)
	buildSubs        func(cred azureCredential, subscriptionID string) []subCollector
	lookupEnv        func(string) (string, bool)
	concurrencyFloor int
}

// New returns the Azure collector as it is served in the shipped binary.
func New() *Collector { return &Collector{} }

// Tool names the MCP tool this collector serves.
func (c *Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{Name: framework.DefaultToolName, Description: toolDescription}
}

// Walk enumerates the subscription and returns everything it found together
// with the completeness assertion for this run.
//
// THE ASSERTION IS THE POINT OF THE ERROR HANDLING BELOW. A failure that stops
// the walk from starting at all — no subscription, no credential — is a walk
// that produced nothing and is returned as an ERROR, which the framework turns
// into a refused collect that writes nothing. A failure INSIDE the walk leaves
// real resources gathered, so it returns them with an INCOMPLETE assertion
// naming what failed. The one outcome that must never happen is a partial walk
// asserting completeness.
//
// THE FOREIGN-CONTEXT BLOCK IS NAMED `_` DELIBERATELY. This collector's config
// entry declares no foreign graph, so the block it would receive is the zero
// value on every call; reading it would mean concluding something from an
// emptiness that carries no information, since an absent field there means "not
// declared" rather than "not present in the operator's graphs". The parameter
// stays in the signature because the framework's interface requires it, and
// naming it makes the omission a choice a reader can see rather than an
// oversight. A later entry that declares cloud or code context changes this
// name and nothing else about the signature.
func (c *Collector) Walk(ctx context.Context, id string, params Params, _ framework.ForeignContext) (framework.Result, error) {
	subscriptionID, err := resolveSubscriptionID(id, params, c.lookup)
	if err != nil {
		return framework.Result{}, err
	}

	cred, err := c.credential(ctx)
	if err != nil {
		return framework.Result{}, err
	}

	subs := c.subCollectors(cred, subscriptionID)
	outcome := runSubCollectors(ctx, subs, c.concurrency(params))

	// The resolvers run over the merged walk, after the fan-out has joined:
	// every input they read is a node or edge this walk already produced, so
	// they cost no further round trips and none of them can fail.
	outcome = runResolvers(outcome)

	result := framework.Result{
		Nodes: nodesOf(dedupeResources(outcome.resources)),
		Edges: edgesOf(dedupeEdges(outcome.edges)),
	}
	if reason := outcome.incompleteReason(); reason != "" {
		result.Complete = framework.Incomplete(reason)
		return result, nil
	}
	result.Complete = framework.Complete()
	return result, nil
}

// resolveSubscriptionID decides which subscription this collect walks, in the
// order the operator would expect: an explicit params value, then the collect
// id, then the environment.
//
// IT IS A PURE FUNCTION taking the environment lookup, so every arm — including
// the one where nothing names a subscription — is reachable from a test without
// touching the process environment.
func resolveSubscriptionID(id string, params Params, lookupEnv func(string) (string, bool)) (string, error) {
	if params.SubscriptionID != "" {
		return params.SubscriptionID, nil
	}
	if id != "" {
		return id, nil
	}
	if v, ok := lookupEnv(envSubscriptionID); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf(
		"azure collector: no subscription to walk: the collect id is empty, params carry no subscription_id, and %s is not set in this collector's environment",
		envSubscriptionID)
}

// credential builds the Azure credential chain, or returns the test seam's.
//
// THE FAILURE NAMES THE CHAIN. A missing credential is the single most common
// way an install is wrong, and azidentity's own error names the arms it tried;
// wrapping it with the chain's name and the fact that this collector supplies
// nothing itself is what turns "DefaultAzureCredential: failed to acquire a
// token" into something an operator can act on in their config entry.
func (c *Collector) credential(ctx context.Context) (azureCredential, error) {
	if c.newCredential != nil {
		return c.newCredential(ctx)
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, wrapCredentialError(err)
	}
	return cred, nil
}

// wrapCredentialError turns the SDK's own credential failure into one an
// operator can act on.
//
// IT IS A FUNCTION RATHER THAN AN INLINE WRAP because the branch that reaches
// it is not reachable from a test: azidentity's chain constructor succeeds on a
// machine with no Azure configuration at all and defers its failure to the
// token request, so a test asserting this wording through Collector.credential
// asserts nothing and silently degrades into an `if err != nil` that never
// fires. Taking the cause as an argument makes the wording testable
// unconditionally, which is the only way this requirement is observed.
//
// WHAT IT ADDS TO THE CAUSE, and each part earns its place: the CHAIN it tried,
// because the SDK's message names arms an operator has never heard of; that
// this collector supplies no credential of its own, because the first guess is
// always that the daemon passes one; and WHERE the fix is, because the answer
// is a config entry rather than anything on this machine.
func wrapCredentialError(cause error) error {
	return fmt.Errorf(
		"azure collector: no usable credential in the Azure default credential chain "+
			"(environment, workload identity, managed identity, Azure CLI, Azure Developer CLI, Azure PowerShell); "+
			"this collector reads credentials only from that chain, and the chain reads only this collector's own "+
			"environment, so the fix is in the env block of its config entry: %w", cause)
}

// subCollectors returns the walk's subcollectors, or the test seam's.
func (c *Collector) subCollectors(cred azureCredential, subscriptionID string) []subCollector {
	if c.buildSubs != nil {
		return c.buildSubs(cred, subscriptionID)
	}
	return buildSubCollectors(cred, subscriptionID)
}

// lookup reads the process environment, or the test seam's.
func (c *Collector) lookup(name string) (string, bool) {
	if c.lookupEnv != nil {
		return c.lookupEnv(name)
	}
	return os.LookupEnv(name)
}

// concurrency resolves the fan-out degree: the caller's, then the collector's
// own floor, then the built-in default.
func (c *Collector) concurrency(params Params) int {
	if params.MaxConcurrency > 0 {
		return params.MaxConcurrency
	}
	if c.concurrencyFloor > 0 {
		return c.concurrencyFloor
	}
	return defaultConcurrency
}

// nodesOf converts walked resources into contract nodes.
func nodesOf(rs []resource) []framework.Node {
	out := make([]framework.Node, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.node())
	}
	return out
}

// edgesOf converts walked edges into contract edges.
func edgesOf(es []edge) []framework.Edge {
	out := make([]framework.Edge, 0, len(es))
	for _, e := range es {
		out = append(out, e.contractEdge())
	}
	return out
}
