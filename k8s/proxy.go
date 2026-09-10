// SPDX-License-Identifier: Apache-2.0

package main

import "sort"

// proxy.go — the PROXY NODE: how this collector names something that lives in
// another graph, and why the naming has to be byte-exact.
//
// === WHAT A PROXY IS, AND WHY IT IS NOT A CROSS-GRAPH EDGE ===
//
// Six of this collector's edge families terminate on a resource in a CLOUD
// graph: a load balancer, a VM, a cloud IAM identity, a disk, an external
// service, the managed cluster itself. The collector contract has no way to
// name an endpoint in another graph — that is a separate mechanism, and it is
// what linkage.go waits on.
//
// A proxy sidesteps the problem without needing it. The collector mints a
// lightweight NODE into ITS OWN result, carrying the foreign graph, the foreign
// id and the account as metadata, and points its edge at that node. Both
// endpoints then exist in the same batch that writes them, and nothing dangles.
// The proxy is an ordinary contract node: id, type, and the rest in metadata.
//
// === THE ID SCHEME IS A CONTRACT WITH EVERY OTHER PRODUCER ===
//
// The id is "proxy:cloud:<account>:<foreign id>", which is byte-for-byte what
// the shared cross-graph proxy builder produces for a cloud target. That is not
// cosmetic: a second collect, or a different collector reaching the same
// resource, must land on the SAME id or the graph grows a duplicate proxy per
// run. This is also what makes the proxy families idempotent under the
// carry-forward semantics.
//
// THERE IS A SECOND FORM, AND IT IS NOT AN ERROR PATH. When the account cannot
// be determined, the id is "proxy:cloud::<foreign id>" with an EMPTY account
// segment. It is still deterministic, so repeat runs still converge; it is
// distinct from the account-bearing form, so a later pass that learns the
// account can enrich rather than collide. A collector that always took one arm
// mints no id at all for an unknown account, or the wrong id when the account
// is known — which is why every family that can reach both arms is tested on
// both.

// proxyIDFor builds the id for a cloud-graph proxy.
func proxyIDFor(account, foreignID string) string {
	return "proxy:cloud:" + account + ":" + foreignID
}

// proxyTarget is a resource in another graph that an edge terminates on.
type proxyTarget struct {
	// Account is the cloud account or graph name the resource belongs to. EMPTY
	// IS A LEGITIMATE VALUE and selects the dangling id form.
	Account string
	// ID is the foreign resource's own id, and is REQUIRED: a proxy with no
	// foreign id names nothing.
	ID string
	// ResourceType, Provider and Region are carried as metadata for a consumer
	// resolving the proxy later. They are descriptive and never part of the id.
	ResourceType string
	Provider     string
	Region       string
	// SymbolName is the human-readable name of the foreign resource.
	SymbolName string
}

// proxyAccumulator collects the distinct proxies a walk needs, so a resource
// referenced by twenty pods is minted once.
type proxyAccumulator struct {
	byID map[string]Node
}

func newProxyAccumulator() *proxyAccumulator {
	return &proxyAccumulator{byID: map[string]Node{}}
}

// proxy interns one target and returns the id an edge should point at.
//
// IT REPORTS false FOR A TARGET WITH NO FOREIGN ID. A proxy id built from an
// empty foreign id is "proxy:cloud:<account>:" — a well-formed string naming
// nothing, which every later resolution would treat as a real reference. The
// caller's own gate is what keeps that out; this is the backstop.
func (a *proxyAccumulator) proxy(t proxyTarget) (string, bool) {
	if t.ID == "" {
		return "", false
	}
	id := proxyIDFor(t.Account, t.ID)
	if _, exists := a.byID[id]; exists {
		return id, true
	}

	meta := map[string]string{
		"foreign_graph": "cloud",
		"foreign_id":    t.ID,
		"account":       t.Account,
	}
	if t.ResourceType != "" {
		meta["resource_type"] = t.ResourceType
	}
	if t.Provider != "" {
		meta["provider"] = t.Provider
	}
	if t.Region != "" {
		meta["region"] = t.Region
	}

	source := "proxy:cloud:" + t.Account
	if t.Account == "" {
		// The dangling form names itself, so an operator reading the graph can
		// tell a proxy whose account is unknown from one whose account is the
		// empty string by accident.
		source = "proxy:cloud:dangling"
	}
	name := t.SymbolName
	if name == "" {
		name = t.ID
	}

	a.byID[id] = Node{
		ID:         id,
		Type:       nodeTypeProxy,
		SymbolName: name,
		Source:     source,
		Metadata:   meta,
	}
	return id, true
}

// nodes returns the interned proxies in a stable order, so two collects of an
// unchanged cluster produce the same result bytes.
func (a *proxyAccumulator) nodes() []Node {
	out := make([]Node, 0, len(a.byID))
	for _, n := range a.byID {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// buildProxyFamilies runs all six proxy families and returns the proxies they
// minted beside the edges that point at them.
//
// THE NODES AND THE EDGES ARE RETURNED TOGETHER because they must be WRITTEN
// together: an edge whose proxy is missing from the same result is a dangling
// edge nothing later repairs.
func buildProxyFamilies(nodes []Node, contextName string) ([]Node, []Edge) {
	acc := newProxyAccumulator()

	var edges []Edge
	edges = append(edges, buildExposedByEdges(nodes, acc)...)
	edges = append(edges, buildRunsInClusterEdges(nodes, contextName, acc)...)
	edges = append(edges, buildBackedByVMEdges(nodes, acc)...)
	edges = append(edges, buildAssumesIdentityEdges(nodes, acc)...)
	edges = append(edges, buildUsesDiskEdges(nodes, acc)...)
	edges = append(edges, buildConnectsToEdges(nodes, acc)...)

	return acc.nodes(), edges
}
