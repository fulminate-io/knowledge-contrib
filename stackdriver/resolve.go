// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// resolve.go — TURNING LOG LABELS INTO CLOUD RESOURCES, and the one seam this
// module cannot close on its own.
//
// WHAT THIS COLLECTOR CAN DECIDE ALONE: which labels are worth resolving, in
// which order, deduplicated how. That is serviceLabelPairs below and it needs
// nothing but the streams.
//
// WHAT IT CANNOT: which cloud resource a given label VALUE names, and whether
// two resources depend on each other. Both are facts about the operator's cloud
// graph. This process holds its tool arguments and its declared environment and
// has no access to that graph, so the answers must arrive as INPUT.
//
// THE INPUT IS THE DECLARED FOREIGN-GRAPH CONTEXT ON THE COLLECT: a collector
// declares in its config entry which cloud graphs and which node fields it
// needs, the client reads them out of the operator's own graphs, and they
// arrive as the walk's fourth argument. foreignCloudContext below is what turns
// that block into the two answers this module asks for.
//
// AN ENTRY THAT DECLARES NOTHING YIELDS NO CONTEXT, and the collect then emits
// zero proxies, zero EMITTED_BY edges and zero CORRELATES_WITH edges. THAT ZERO
// IS CORRECT RATHER THAN DEGRADED, and it is what the built-in log pipeline also
// produces when it runs with no cloud graph attached. It is not a silent
// fallback: nothing is dropped, no cloud read failed, and no resolution was
// attempted and abandoned. There is simply nothing to resolve against.

// foreignCloudContext answers the two cloud questions from the declared
// foreign-graph slice the collect input carried.
//
// THE SLICE IS DECLARED, NOT QUERIED. This collector's config entry says which
// cloud graphs and which node fields it needs; the client reads them out of the
// operator's own graphs and sends them with the call. So an absent field means
// "not declared" rather than "not present in the graph", and a match this
// resolver cannot make may mean the entry asked for too little rather than that
// the resource does not exist. That is why an unresolved label is skipped rather
// than reported as an absence.
type foreignCloudContext struct {
	// byName indexes every declared cloud node by the name it can be matched on,
	// each mapping to the resources that name, in a deterministic order.
	byName map[string][]resolvedResource
	// dependsOn holds one key per declared edge, in both directions, so a
	// dependency question is a lookup rather than a scan.
	dependsOn map[string]struct{}
}

// newForeignCloudContext builds the resolver from the declared block, returning
// nil when no cloud graph was declared at all.
//
// A NIL RETURN IS THE SAME INPUT THE MODULE SAW BEFORE THIS ROUTE EXISTED: no
// cloud context, so no resolutions and no confirmed correlations. It is
// distinguished from an EMPTY declared graph, which is an operator who declared
// the family and whose graph holds nothing matching — also zero resolutions, but
// for a different reason and from a block that did arrive.
func newForeignCloudContext(cloud []framework.ForeignGraph) cloudContext {
	if len(cloud) == 0 {
		return nil
	}
	c := &foreignCloudContext{
		byName:    make(map[string][]resolvedResource),
		dependsOn: make(map[string]struct{}),
	}
	for _, graph := range cloud {
		for _, node := range graph.Nodes {
			if node.ID == "" {
				continue
			}
			resource := resolvedResource{Account: graph.GraphName, ID: node.ID}
			for _, name := range matchableNames(node) {
				c.byName[name] = append(c.byName[name], resource)
			}
		}
		for _, edge := range graph.Edges {
			if edge.FromID == "" || edge.ToID == "" {
				continue
			}
			// BOTH DIRECTIONS. "These two resources are connected" is what
			// upgrades a temporal coincidence to a correlation, and connection
			// is symmetric even where the declared edge is not.
			c.dependsOn[dependencyKey(graph.GraphName, edge.FromID, edge.ToID)] = struct{}{}
			c.dependsOn[dependencyKey(graph.GraphName, edge.ToID, edge.FromID)] = struct{}{}
		}
	}
	// The order is fixed here rather than left as the block's, so two collects
	// carrying the same resources in a different order resolve identically.
	for name := range c.byName {
		sort.Slice(c.byName[name], func(i, j int) bool {
			if c.byName[name][i].Account != c.byName[name][j].Account {
				return c.byName[name][i].Account < c.byName[name][j].Account
			}
			return c.byName[name][i].ID < c.byName[name][j].ID
		})
	}
	return c
}

// matchableNames are the values a declared cloud node can be matched against a
// log label by.
//
// THE SET IS SMALL AND EXACT-MATCH ON PURPOSE. A fuzzy rule would resolve a
// label to a resource that merely resembles it, and every resolution becomes a
// proxy node and an EMITTED_BY edge asserting that this log stream came from
// that cloud resource. A wrong edge there is worse than a missing one, so the
// names are the node's own symbol and the two metadata keys a cloud collector
// conventionally writes a resource's name into.
func matchableNames(node framework.ForeignNode) []string {
	var names []string
	for _, candidate := range []string{node.SymbolName, node.Metadata["name"], node.Metadata["service"]} {
		if candidate != "" && !slices.Contains(names, candidate) {
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

// ResolveService maps one service-identifying label to a declared cloud
// resource. The FIRST resource under the fixed order wins; an ambiguous name
// therefore resolves the same way on every run rather than by declaration order.
func (c *foreignCloudContext) ResolveService(_ *logStream, _, value string) (resolvedResource, bool) {
	matches := c.byName[value]
	if len(matches) == 0 {
		return resolvedResource{}, false
	}
	return matches[0], true
}

// HasDependency reports whether the declared slice carries an edge between the
// two resources. Two resources in DIFFERENT accounts are never dependent here,
// because the declared edges are within-graph by the block's own shape.
func (c *foreignCloudContext) HasDependency(a, b resolvedResource) bool {
	if a.Account != b.Account {
		return false
	}
	_, ok := c.dependsOn[dependencyKey(a.Account, a.ID, b.ID)]
	return ok
}

// resolvedResource is one cloud resource a label resolved to.
type resolvedResource struct {
	Account string
	ID      string
}

// cloudContext is the cloud-graph knowledge a collect may carry. It is an
// interface rather than a data slice so the two questions this module asks stay
// explicit and so a nil value is a legitimate, meaningful input: no context.
type cloudContext interface {
	// ResolveService maps one service-identifying label to a cloud resource,
	// using the stream for the surrounding context labels (project, cluster)
	// that disambiguate which graph the resource lives in.
	ResolveService(stream *logStream, key, value string) (resolvedResource, bool)
	// HasDependency reports whether two resolved resources are connected in the
	// cloud graph, which is what upgrades a temporal coincidence to a
	// correlation.
	HasDependency(a, b resolvedResource) bool
}

// servicePair is one service-identifying label and the first stream it was seen
// on. The stream travels with it because resolution needs the surrounding
// context labels, not just the value.
type servicePair struct {
	key    string
	value  string
	stream *logStream
}

// serviceLabelPairs returns the deduplicated service-identifying labels across
// every stream, each with the first stream that carried it.
//
// DEDUPLICATION IS BY (key, value) AND IT IS WHY A DUPLICATE EMITTED_BY EDGE
// CANNOT ARISE. Two streams tagged `service=api` are one service, so they
// produce one pair, one resolution and one edge — while two DIFFERENT labels
// resolving to the same cloud resource stay two pairs and two edges, which is
// the asymmetry proxy.go preserves.
//
// The empty value is skipped: it identifies no service, and resolving it would
// ask the cloud graph about the empty string.
func serviceLabelPairs(streams []*logStream) []servicePair {
	seen := make(map[string]struct{})
	var out []servicePair
	for _, s := range streams {
		if s == nil {
			continue
		}
		for _, k := range sortedKeys(s.LowCardLabels) {
			v := s.LowCardLabels[k]
			if !isServiceIdentifyingKey(k) || v == "" {
				continue
			}
			dupKey := k + "=" + v
			if _, dup := seen[dupKey]; dup {
				continue
			}
			seen[dupKey] = struct{}{}
			out = append(out, servicePair{key: k, value: v, stream: s})
		}
	}
	return out
}

// isServiceIdentifyingKey reports whether a label key names a service.
//
// The set is small and closed on purpose. Every key in it becomes a proxy node
// and an edge when it resolves, so widening it widens the graph; `host` and
// `pod_name` are deliberately absent, because an instance is not a service and
// resolving one would point the correlation pass at the wrong resource.
func isServiceIdentifyingKey(key string) bool {
	switch key {
	case fieldService, fieldNamespace, "deployment", "app":
		return true
	}
	return false
}

// computeStreamResolutions resolves every service-identifying label against the
// supplied cloud context.
//
// A NIL CONTEXT YIELDS NO RESOLUTIONS, which is the state at this tree and the
// same state the built-in pipeline is in with no cloud graph attached. A label
// the context does not resolve is skipped rather than emitted with an empty
// account: a proxy that names no resource is worse than an absent one.
func computeStreamResolutions(streams []*logStream, cloud cloudContext) []resolvedProxyEntry {
	if cloud == nil {
		return nil
	}
	pairs := serviceLabelPairs(streams)
	out := make([]resolvedProxyEntry, 0, len(pairs))
	for _, pair := range pairs {
		resolved, ok := cloud.ResolveService(pair.stream, pair.key, pair.value)
		if !ok {
			continue
		}
		out = append(out, resolvedProxyEntry{
			LabelKey:   pair.key,
			LabelValue: pair.value,
			Account:    resolved.Account,
			ResourceID: resolved.ID,
		})
	}
	return out
}

// sortedKeys returns a label map's keys in ascending order, so a pass over a
// label set visits it the same way on every run.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// proxyDiagnosticMap indexes a resolution set by label value, in the
// "account:resource" form the correlation evidence string carries.
func proxyDiagnosticMap(resolutions []resolvedProxyEntry) map[string]string {
	if len(resolutions) == 0 {
		return nil
	}
	out := make(map[string]string, len(resolutions))
	for _, r := range resolutions {
		out[r.LabelValue] = r.Account + ":" + r.ResourceID
	}
	return out
}
