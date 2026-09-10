// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector.go — the COLLECTOR the framework serves: its params, its walk, and
// the order the walk's four phases run in.
//
// THE FOUR PHASES, AND WHY THE ORDER IS NOT ARBITRARY:
//
//  1. ENUMERATE. Every fixed kind and every served custom resource, converted
//     to a node, with the edges each object states about itself.
//  2. DERIVE. The nine families that are a function of the enumeration rather
//     than of any single object — namespace membership, label-selector
//     matching, network-policy reachability. They read the nodes phase 1
//     produced and nothing else, which is why they need no graph access and run
//     in this process.
//  3. PROXY. The six families that terminate on a node in ANOTHER graph. Each
//     mints that node as a proxy INTO THIS RESULT and points its edge at it, so
//     both endpoints of every edge exist in the batch that writes them.
//  4. LINK. The two edge types that name an endpoint in another graph without
//     minting it. All four of their shapes are emitted, each naming its family
//     in whichever of the contract's two family fields matches the end that is
//     foreign. See linkage.go.
//
// COMPLETENESS IS TRACKED THROUGHOUT, NOT DECIDED AT THE END. Every phase that
// can fail to see something records a reason, and the walk asserts an
// incomplete enumeration if any reason was recorded. A walk that asserted
// completeness after a partial enumeration would arm the server's
// whole-remainder deletion against a graph it only half-read.

// Params is this collector's tool parameters. It is the type the framework
// infers the advertised params schema from, so a field added here becomes a
// parameter the client validates before the call.
type Params struct {
	// Context names a KUBECONFIG CONTEXT to collect. It is OPTIONAL: omitted
	// means the kubeconfig's currently selected context.
	//
	// NOT TO BE CONFUSED with the collect input's declared foreign-graph
	// context block, which is graph context from the knowledge store. This one
	// is a Kubernetes cluster selector and nothing else.
	Context string `json:"context,omitempty"`
}

// k8sCollector is the collector the framework serves.
type k8sCollector struct {
	// sources is where the credential resolution reads from. Production uses
	// [defaultCredentialSources]; a test substitutes an environment.
	sources credentialSources
	// newClients builds the three API clients from a resolved credential. It is
	// a field so a test can hand the walk fake clientsets without dialing.
	newClients func(credential) (clientBundle, error)
}

// clientBundle is the three API surfaces a Kubernetes walk needs: the typed
// clientset for the built-in groups, the dynamic client for Gateway API,
// AdminNetworkPolicy and custom resources, and the apiextensions clientset for
// discovering which custom resources are served at all.
type clientBundle struct {
	typed       kubernetes.Interface
	dynamic     dynamic.Interface
	apiext      apiextensionsclient.Interface
	contextName string
}

func (c *k8sCollector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: "collect",
		Description: "Enumerate a Kubernetes cluster's objects and the relationships between them, " +
			"reading through the standard kubeconfig resolution or an in-cluster service account.",
	}
}

// Walk enumerates the cluster and returns what it found.
//
// id names the graph INSTANCE the result lands in and is the client's to
// choose; it is not the cluster selector and it is not the graph family.
//
// foreign is the DECLARED foreign-graph context, filled by the client from the
// families this collector's registration asked for. A collector never requests
// it at run time, and an empty block means "nothing was declared" rather than
// "the operator's graphs are empty" — so the two linkage shapes that need it
// produce nothing when it is absent, while the two that compose their target
// from local metadata emit regardless. See linkage.go.
func (c *k8sCollector) Walk(
	ctx context.Context, id string, params Params, foreign framework.ForeignContext,
) (framework.Result, error) {
	if err := ctx.Err(); err != nil {
		return framework.Result{}, fmt.Errorf("the collect was cancelled before it began: %w", err)
	}

	cred, err := resolveCredential(params.Context, c.sources)
	if err != nil {
		return framework.Result{}, err
	}

	build := c.newClients
	if build == nil {
		build = buildClients
	}
	clients, err := build(cred)
	if err != nil {
		return framework.Result{}, fmt.Errorf("building a Kubernetes client for context %q: %w", cred.ContextName, err)
	}

	return walkCluster(ctx, clients, foreign)
}

// walkResult accumulates one walk's output and its completeness reasons.
//
// IT IS THE ONLY PLACE A REASON IS RECORDED. A phase that cannot see something
// calls incomplete with a sentence naming what it could not see; nothing else
// decides completeness, and there is no way to assert a complete walk while a
// reason is outstanding.
type walkResult struct {
	nodes   []Node
	edges   []Edge
	reasons []string
}

func (w *walkResult) addNode(n Node) { w.nodes = append(w.nodes, n) }

func (w *walkResult) addEdges(edges ...Edge) { w.edges = append(w.edges, edges...) }

// incomplete records that this walk did not see everything, and why.
func (w *walkResult) incomplete(format string, args ...any) {
	w.reasons = append(w.reasons, fmt.Sprintf(format, args...))
}

// completeness turns the recorded reasons into the walk's assertion.
func (w *walkResult) completeness() framework.Completeness {
	if len(w.reasons) == 0 {
		return framework.Complete()
	}
	reasons := append([]string(nil), w.reasons...)
	sort.Strings(reasons)
	return framework.Incomplete(strings.Join(reasons, "; "))
}

// walkCluster runs the four phases against an already-built set of clients. It
// is separate from [k8sCollector.Walk] so a test drives it with fake clientsets
// and no credential resolution at all.
func walkCluster(ctx context.Context, clients clientBundle, foreign framework.ForeignContext) (framework.Result, error) {
	w := &walkResult{}

	// PHASE 1 — enumerate.
	if err := enumerateTyped(ctx, clients, w); err != nil {
		return framework.Result{}, err
	}
	if err := enumerateDynamic(ctx, clients, w); err != nil {
		return framework.Result{}, err
	}
	if err := enumerateCustomResources(ctx, clients, w); err != nil {
		return framework.Result{}, err
	}

	// PHASE 2 — derive, from the enumeration alone.
	w.addEdges(buildNamespaceMembershipEdges(w.nodes)...)
	w.addEdges(buildSelectsEdges(w.nodes)...)
	w.addEdges(buildNetworkPolicyRestrictionEdges(w.nodes)...)
	w.addEdges(buildNetworkPolicyReachabilityEdges(w.nodes)...)
	w.addEdges(buildAdminNetworkPolicyEdges(w.nodes)...)
	w.addEdges(buildImageLineageEdges(w.nodes)...)

	// PHASE 3 — proxy. This APPENDS NODES as well as edges: every proxy an edge
	// terminates on is minted into this same result, so the write that lands
	// the edges lands both of their endpoints.
	proxyNodes, proxyEdges := buildProxyFamilies(w.nodes, clients.contextName)
	w.nodes = append(w.nodes, proxyNodes...)
	w.addEdges(proxyEdges...)

	// PHASE 4 — link, through the contract's target-graph field, against the
	// declared foreign context. See linkage.go.
	w.addEdges(buildLinkageEdges(w.nodes, foreign)...)

	if err := ctx.Err(); err != nil {
		return framework.Result{}, fmt.Errorf("the collect was cancelled mid-walk: %w", err)
	}
	return framework.Result{Nodes: w.nodes, Edges: w.edges, Complete: w.completeness()}, nil
}

// buildClients is the production client constructor.
func buildClients(cred credential) (clientBundle, error) {
	typed, err := kubernetes.NewForConfig(cred.Config)
	if err != nil {
		return clientBundle{}, fmt.Errorf("typed clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cred.Config)
	if err != nil {
		return clientBundle{}, fmt.Errorf("dynamic client: %w", err)
	}
	apiext, err := apiextensionsclient.NewForConfig(cred.Config)
	if err != nil {
		return clientBundle{}, fmt.Errorf("apiextensions clientset: %w", err)
	}
	return clientBundle{typed: typed, dynamic: dyn, apiext: apiext, contextName: cred.ContextName}, nil
}
