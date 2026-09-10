// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// resolve.go — turning the collect's DECLARED CLOUD SLICE into the
// (label, cloud resource) resolutions the proxy emitter consumes.
//
// THIS IS THE INPUT THIS COLLECTOR CANNOT COMPUTE FOR ITSELF. The built-in logs
// pipeline gets its resolutions from a resolver the client injects out of a
// store-fetched cloud subgraph; a collector is a separate process holding only
// its config entry's environment and has no cloud session and no cloud graph.
// What arrives instead is the context block: the cloud graphs the registration
// entry DECLARED this collector needs, read out of the operator's own graphs by
// the client and sent with the call. Matching a log label to a resource in that
// slice is this module's own, and the rules are below.
//
// AN EMPTY SLICE IS NOT AN ERROR AND MEANS NOTHING ABOUT THE OPERATOR'S GRAPHS.
// A collector whose entry declares no cloud family receives no cloud arm, and
// resolves nothing; that is the parity-correct outcome, because the built-in
// path with no cloud graph attached emits no proxy either.

// serviceIdentifyingKeys are the label keys worth resolving against a cloud
// graph, and the set is the built-in path's own. A log stream carries many
// labels and most of them name a pod, a container or a filename; these four are
// the ones that name a SERVICE, which is what a cloud resource is.
var serviceIdentifyingKeys = map[string]bool{
	"service":    true,
	"namespace":  true,
	"deployment": true,
	"app":        true,
}

// CloudContext is the cloud-graph knowledge one collect carries: the two facts
// a collector process cannot derive for itself. It is an interface so a nil
// value is a legitimate, meaningful input — no context — rather than a
// half-built struct.
type CloudContext interface {
	// ResolveService maps one service-identifying label value to a cloud
	// resource, reporting whether it resolved at all.
	ResolveService(value string) (Resource, bool)
	// HasDependency reports whether two resolved resources are connected in the
	// declared slice, which is what upgrades a temporal coincidence to a
	// correlation.
	HasDependency(a, b Resource) bool
}

// Resource is one cloud resource a label resolved to.
type Resource struct {
	Account string
	ID      string
}

// foreignCloudContext answers both questions from the declared foreign-graph
// slice the collect input carried.
type foreignCloudContext struct {
	// byName indexes every declared node by a name it can be matched on, each
	// mapping to the resources carrying that name in a fixed order.
	byName map[string][]Resource
	// dependsOn holds one key per declared edge, in BOTH directions, so a
	// dependency question is a lookup rather than a scan.
	dependsOn map[string]struct{}
}

// NewCloudContext builds the context from the declared cloud slice, returning
// NIL when no cloud graph was declared at all.
//
// A NIL RETURN AND AN EMPTY DECLARED GRAPH ARE DIFFERENT STATES and both are
// correct. Nil is an entry that declared no cloud family; empty is an operator
// who declared it and whose graph holds nothing matching. Both resolve nothing
// and confirm nothing, and neither is a degrade: nothing was dropped, no read
// failed, and no resolution was attempted and abandoned.
func NewCloudContext(cloud []framework.ForeignGraph) CloudContext {
	if len(cloud) == 0 {
		return nil
	}
	c := &foreignCloudContext{
		byName:    make(map[string][]Resource),
		dependsOn: make(map[string]struct{}),
	}
	for _, graph := range cloud {
		if graph.GraphName == "" {
			continue
		}
		for _, node := range graph.Nodes {
			if node.ID == "" {
				continue
			}
			resource := Resource{Account: graph.GraphName, ID: node.ID}
			for _, name := range matchableNames(node) {
				c.byName[name] = append(c.byName[name], resource)
			}
		}
		for _, edge := range graph.Edges {
			if edge.FromID == "" || edge.ToID == "" {
				continue
			}
			// BOTH DIRECTIONS. "These two resources are connected" is what
			// upgrades a coincidence to a correlation, and connection is
			// symmetric even where the declared edge is not.
			c.dependsOn[dependencyKey(graph.GraphName, edge.FromID, edge.ToID)] = struct{}{}
			c.dependsOn[dependencyKey(graph.GraphName, edge.ToID, edge.FromID)] = struct{}{}
		}
	}
	// NOT SORTED. The candidates stay in the order the block declared them, and
	// ResolveService takes the first — which is the contract this module has
	// always documented and the one it can honestly keep.
	//
	// A SORT WOULD PROMISE SOMETHING THIS MODULE CANNOT SEE. It would make the
	// winner independent of the block's order, which sounds stronger and is not:
	// the block is built by the client, and the client ALREADY sorts the graph
	// names before filling it (cmd/knowledge/internal/tools/collect_context.go,
	// fillCloudContext, so that one unchanged store yields one byte-identical
	// block across runs). So on a real block the two rules cannot disagree, and
	// on a hand-written one declaration order is what its author wrote.
	return c
}

// matchableNames are the values a declared node can be matched against a log
// label by.
//
// THE SET IS THREE VALUES AND EVERY MATCH IS EXACT. Every resolution becomes a
// proxy node and an EMITTED_BY edge asserting that this log stream came from
// that cloud resource, and a wrong edge there is worse than a missing one, so
// nothing here is fuzzy. What it is not is symbol-name-only, and the reason is
// the declaration: an operator's entry decides which node FIELDS the client
// fetches, so a block declaring `metadata` and not `symbol_name` would resolve
// nothing at all under a symbol-only rule and be silently useless. The two
// metadata keys are the ones a cloud collector conventionally writes a
// resource's name into.
//
// THE ORDER IS THE PRECEDENCE. A node whose symbol and metadata carry the SAME
// name is indexed under it more than once, and that is deliberately not
// de-duplicated: every entry maps to the same resource, so the winner is the
// same either way and a dedup here would be a guard with no observable effect.
// Two DIFFERENT names on one node are two index entries by design — that is how
// a label matching any of them finds it.
func matchableNames(node framework.ForeignNode) []string {
	var names []string
	for _, candidate := range []string{node.SymbolName, node.Metadata["name"], node.Metadata["service"]} {
		if candidate != "" {
			names = append(names, candidate)
		}
	}
	return names
}

// dependencyKey is one directed pair within one declared graph. The graph name
// is part of the key so two accounts holding same-named resources cannot answer
// each other's dependency questions.
func dependencyKey(graphName, from, to string) string {
	return graphName + "\x00" + from + "\x00" + to
}

// ResolveService maps one label value to a declared resource. THE FIRST
// DECLARED wins: the first matching node of the first declared graph carrying
// the name.
func (c *foreignCloudContext) ResolveService(value string) (Resource, bool) {
	matches := c.byName[value]
	if len(matches) == 0 {
		return Resource{}, false
	}
	return matches[0], true
}

// HasDependency reports whether the declared slice carries an edge between the
// two resources. TWO RESOURCES IN DIFFERENT ACCOUNTS ARE NEVER DEPENDENT HERE,
// because the declared edges are within-graph by the block's own shape. The
// common detector applies no account rule of its own and hands both resources
// across whole, which is what keeps that refusal here, where its reason is.
func (c *foreignCloudContext) HasDependency(a, b Resource) bool {
	if a.Account != b.Account {
		return false
	}
	_, ok := c.dependsOn[dependencyKey(a.Account, a.ID, b.ID)]
	return ok
}

// ResolutionsFromContext derives this walk's resolutions against an
// already-built cloud context, so one collect builds the context ONCE and the
// proxies and the correlations resolve through the same matching rule. Two
// rules would drift, and a proxy naming one resource beside a correlation
// naming another is exactly the disagreement no test would catch.
//
// THE RULES, each of which decides which proxies a collect emits:
//
//  1. Only LOW-CARDINALITY labels are considered. A high-cardinality label names
//     an instance rather than a service, and a proxy per pod is not what the
//     cloud graph holds.
//  2. Only the four service-identifying keys above, and only with a non-empty
//     value.
//  3. Pairs are DEDUPLICATED by key=value: two streams naming one service
//     produce one resolution, and through it one proxy node.
//  4. A pair resolves against the FIRST DECLARED cloud graph carrying a node
//     that matches the label's value, and against the first such node within it.
//     A node matches on its SymbolName, its metadata `name` or its metadata
//     `service`, in that order, always exactly. The graph's name is the ACCOUNT
//     — the cloud arm's graph_name is the per-graph key of the fetch that filled
//     it — and the node's id is the resource.
//  5. A pair that matches nothing is skipped, not reported: the operator's
//     declaration decides what was fetched, and a label naming something outside
//     it is an ordinary label rather than a failure.
//
// THE ORDER IS DETERMINISTIC, and that is a deliberate divergence from the
// built-in path rather than an accident. Its equivalent walks each stream's
// label MAP, whose iteration Go randomizes, so two runs over identical input
// emit the resolutions — and the EMITTED_BY edges built from them — in different
// orders. Here the streams are already in first-appearance order and each
// stream's keys are sorted before they are read, so a collect is reproducible.
func ResolutionsFromContext(streams []*Stream, cloud CloudContext) []Resolution {
	if len(streams) == 0 || cloud == nil {
		return nil
	}

	var out []Resolution
	seen := make(map[string]struct{})
	for _, s := range streams {
		if s == nil {
			continue
		}
		for _, key := range sortedServiceKeys(s.LowCardLabels) {
			value := s.LowCardLabels[key]
			pair := key + "=" + value
			if _, dup := seen[pair]; dup {
				continue
			}
			seen[pair] = struct{}{}

			resolved, ok := cloud.ResolveService(value)
			if !ok {
				continue
			}
			out = append(out, Resolution{
				LabelKey:   key,
				LabelValue: value,
				Account:    resolved.Account,
				ResourceID: resolved.ID,
			})
		}
	}
	return out
}

// sortedServiceKeys returns the service-identifying keys of one label set that
// carry a value, in sorted order. The sort is what makes the walk deterministic;
// the value check is rule 2.
func sortedServiceKeys(labels map[string]string) []string {
	keys := make([]string, 0, len(labels))
	for k, v := range labels {
		if v == "" || !serviceIdentifyingKeys[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
