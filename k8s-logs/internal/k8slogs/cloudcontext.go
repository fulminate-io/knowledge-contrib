// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// cloudcontext.go — the cloud-provider slice this collector needs to emit proxy nodes,
// EMITTED_BY edges and confirmed CORRELATES_WITH edges, and the pure functions
// that turn it into those.
//
// WHY A COLLECTOR CANNOT COMPUTE THIS ITSELF. A collector is a separate process
// reached over MCP: it has no graph caller and can fetch nothing. Everything it
// knows about the operator's other graphs arrives in the declared block or does
// not arrive at all. With no provider slice it emits zero proxies, zero
// EMITTED_BY edges and zero confirmed correlations, which is the honest answer
// for a run with nothing to resolve against.
//
// THE TRANSPORT IS THE COLLECT INPUT'S DECLARED FOREIGN-GRAPH CONTEXT BLOCK: a
// collector's registration entry declares which foreign graph families and
// which of their fields it needs, the client fills exactly that and nothing
// else, and the block arrives beside `params` on the collect call as the walk's
// fourth argument. The types below are the FRAMEWORK'S, so this module reads the
// same shape the contract sends rather than a copy of it that could drift.
//
// AN ENTRY THAT DECLARES NOTHING YIELDS AN EMPTY BLOCK, and the collect then
// emits zero proxies, zero EMITTED_BY edges and zero CORRELATES_WITH edges. That
// zero is CORRECT rather than degraded: nothing was dropped, no cloud read
// failed, and no resolution was attempted and abandoned. There is simply nothing
// to resolve against, which is also what the built-in log pipeline produces with
// no cloud graph attached.
//
// WHAT THE DECLARATION ASKS FOR IS IN declaration.go, as code rather than as a
// comment here: DeclaredForeignContext returns one entry per registered
// cloud-provider graph type, each asking for the resource nodes' id and type,
// the five matching metadata keys and the two edge endpoints. The metadata keys
// are what a resource is MATCHED on and what a proxy carries for display; the
// edges are what confirms a correlation.
//
// THE FAMILY IS NO LONGER A BUILT-IN `cloud`. Cloud inventory is collected by
// contrib collectors registering their own graph types, so the declaration names
// providers and this file reads every declared family but `code` — see
// CloudContextFrom for why excluding is the right direction.

// CloudContext is the `cloud` arm of the declared foreign-graph context block,
// as this module reads it.
//
// It carries the framework's own slice type rather than a local copy: the graph
// name is the ACCOUNT a proxy id is built under, which is why it travels with
// the nodes rather than being derived from them.
type CloudContext struct {
	Cloud []framework.ForeignGraph
}

// CloudContextFrom takes the cloud-provider arms out of a collect's declared
// block: every declared family EXCEPT code, flattened.
//
// IT NAMES WHAT TO EXCLUDE RATHER THAN WHAT TO INCLUDE. This module matches a
// log stream against resource metadata — namespace, cluster name, region — and
// never against a provider name, so a resource's provider is not a fact it
// reads; declaring aws and gcp and azure and receiving them as one set is
// exactly its input. Listing families to include would have to be revised every
// time an operator registers a provider, and a stale list drops correlations
// silently rather than failing.
func CloudContextFrom(foreign framework.ForeignContext) CloudContext {
	return CloudContext{Cloud: foreign.Except(framework.FamilyCode)}
}

// The metadata keys a supplied cloud node is matched and rendered on.
const (
	cloudMetaNamespace    = "namespace"
	cloudMetaCluster      = "cluster_name"
	cloudMetaResourceType = "resource_type"
	cloudMetaRegion       = "region"
	cloudMetaProvider     = "provider"
)

// IsEmpty reports whether the block carries nothing to resolve against.
func (c CloudContext) IsEmpty() bool {
	for _, g := range c.Cloud {
		if len(g.Nodes) > 0 {
			return false
		}
	}
	return true
}

// Resolutions matches the streams' NAMESPACE labels against the supplied cloud
// nodes and returns one resolution per confirmed match.
//
// THE MATCH IS ON THE NAMESPACE, and that is the only key this collector
// resolves on. A pod's namespace is the unit a cloud graph models as a
// workload, and it is the label the built-in resolver treats as
// service-identifying too. A resolution is emitted only for a cloud node whose
// own namespace metadata equals a namespace some stream carried — a MISS is
// silent and produces no proxy, exactly as the built-in pipeline's resolver
// does, so an unresolvable label is simply a label with no cloud counterpart.
//
// WHERE A CLUSTER IS KNOWN ON BOTH SIDES IT MUST AGREE. Two clusters in one
// cloud account can hold a namespace of the same name, so a cloud node carrying
// a cluster is matched only against streams from that cluster. A cloud node
// carrying NO cluster matches on the namespace alone, which is the right answer
// for a cloud graph that does not model clusters.
func (c CloudContext) Resolutions(streams []*logpipe.Stream) []logpipe.Resolution {
	if c.IsEmpty() || len(streams) == 0 {
		return nil
	}
	wanted := namespaceClusters(streams)
	if len(wanted) == 0 {
		return nil
	}

	seen := make(map[[2]string]struct{})
	var out []logpipe.Resolution
	for _, g := range c.Cloud {
		if g.GraphName == "" {
			continue
		}
		for _, n := range g.Nodes {
			ns := n.Metadata[cloudMetaNamespace]
			if ns == "" || n.ID == "" {
				continue
			}
			clusters, ok := wanted[ns]
			if !ok {
				continue
			}
			if cluster := n.Metadata[cloudMetaCluster]; cluster != "" {
				if _, match := clusters[cluster]; !match {
					continue
				}
			}
			key := [2]string{ns, n.ID}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, logpipe.Resolution{
				LabelKey:     LabelNamespace,
				LabelValue:   ns,
				Account:      g.GraphName,
				ResourceID:   n.ID,
				ResourceType: firstNonEmpty(n.Metadata[cloudMetaResourceType], n.Type),
				Region:       n.Metadata[cloudMetaRegion],
				Provider:     n.Metadata[cloudMetaProvider],
			})
		}
	}
	return out
}

// namespaceClusters indexes the streams' namespaces to the set of clusters each
// was seen on. A stream with no cluster label contributes the empty cluster,
// which no cloud node's non-empty cluster can match — so an unclustered stream
// is matched only by an unclustered cloud node.
func namespaceClusters(streams []*logpipe.Stream) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, s := range streams {
		ns := s.Labels[LabelNamespace]
		if ns == "" {
			continue
		}
		if out[ns] == nil {
			out[ns] = make(map[string]struct{})
		}
		out[ns][s.Labels[LabelCluster]] = struct{}{}
	}
	return out
}

// ResourcesByService maps each namespace to the cloud resource it resolved to.
// It is the backing of both cloud answers the correlation detector asks for.
//
// A NAMESPACE RESOLVING TO SEVERAL RESOURCES IS DROPPED rather than resolved to
// one of them. Picking arbitrarily would make the correlation edges a function
// of the supplied slice's order, and asserting a dependency on the wrong
// resource is worse than asserting none.
func (c CloudContext) ResourcesByService(streams []*logpipe.Stream) map[string]correlation.ResolvedResource {
	resolutions := c.Resolutions(streams)
	counts := make(map[string]int, len(resolutions))
	out := make(map[string]correlation.ResolvedResource, len(resolutions))
	for _, r := range resolutions {
		counts[r.LabelValue]++
		out[r.LabelValue] = correlation.ResolvedResource{Account: r.Account, ID: r.ResourceID}
	}
	for svc, n := range counts {
		if n > 1 {
			delete(out, svc)
		}
	}
	return out
}

// ProxyMap renders each resolved service as the "account:resource" diagnostic
// string a correlation carries in its evidence.
func (c CloudContext) ProxyMap(streams []*logpipe.Stream) map[string]string {
	resolved := c.ResourcesByService(streams)
	out := make(map[string]string, len(resolved))
	for svc, r := range resolved {
		out[svc] = r.Account + ":" + r.ID
	}
	return out
}

// cloudResolver answers the correlation detector's two questions from a
// declared cloud slice. It implements this module's pipeline-side resolver and
// oracle interfaces, and it is built only when there is something to answer
// from — a nil one is what "no declared cloud context" looks like all the way
// down.
type cloudResolver struct {
	// byService is the resolution set, keyed on the service label VALUE the
	// detector reads out of a stream.
	byService map[string]correlation.ResolvedResource
	// dependsOn holds one key per declared edge in BOTH directions. A log
	// correlation says two services' failures are related, which does not
	// depend on which way the dependency points.
	//
	// THE KEY CARRIES THE DECLARING GRAPH, and that is the whole of the rule
	// below rather than a refinement of it. The block's cloud arm is an ARRAY of
	// graphs, so flattening every graph's edges into one set keyed on the two
	// ids alone lets an edge declared by one account confirm a pair resolved out
	// of another — and comparing the two RESOURCES' accounts does not catch it,
	// because both of them are in the account that declared nothing.
	dependsOn map[[3]string]struct{}
}

// Correlation returns the resolver and oracle for these streams, or nil when the
// declared slice answers neither question.
//
// A NIL RETURN IS RETURNED AS AN UNTYPED NIL by its caller rather than being
// wrapped in an interface: an interface holding a typed nil pointer is not nil,
// and the detector would call through it instead of leaving every candidate
// unconfirmed.
func (c CloudContext) Correlation(streams []*logpipe.Stream) *cloudResolver {
	byService := c.ResourcesByService(streams)
	dependsOn := make(map[[3]string]struct{})
	// NO SKIP FOR A GRAPH WITH NO NAME, and its absence is a finding rather than
	// an omission. One stood here and nothing could discriminate it: a nameless
	// graph's edges would be stored under an empty account, and a lookup can only
	// reach that key when the resource it asks about carries an empty account —
	// which Resolutions makes impossible, because it refuses a nameless graph
	// before any resolution is built. A guard whose absence no test can notice
	// reads as protection and protects nothing; the rule is held one function
	// away, where it decides something.
	for _, g := range c.Cloud {
		for _, e := range g.Edges {
			if e.FromID == "" || e.ToID == "" {
				continue
			}
			dependsOn[dependencyKey(g.GraphName, e.FromID, e.ToID)] = struct{}{}
			dependsOn[dependencyKey(g.GraphName, e.ToID, e.FromID)] = struct{}{}
		}
	}
	if len(byService) == 0 && len(dependsOn) == 0 {
		return nil
	}
	return &cloudResolver{byService: byService, dependsOn: dependsOn}
}

// ResolveService maps a service label value to a declared cloud resource. A
// miss is silent: the detector returns such a pair UNCONFIRMED rather than
// dropping it, which is the honest answer when the entry may simply have
// declared too little.
func (r *cloudResolver) ResolveService(_ *correlation.Stream, service string) (correlation.ResolvedResource, bool) {
	res, ok := r.byService[service]
	return res, ok
}

// dependencyKey is the shape a declared edge is stored and looked up under. It
// is ONE function so the two sides cannot disagree, and so the property it
// carries — that the declaring graph is part of a dependency's identity — is
// stated once rather than twice.
func dependencyKey(graph, from, to string) [3]string { return [3]string{graph, from, to} }

// HasDependency reports whether ONE DECLARED GRAPH connects the two resources.
//
// THE ACCOUNT IS PART OF THE ANSWER IN BOTH OF THE WAYS IT HAS TO BE. The
// detector hands both resources across whole and applies no account rule of its
// own; this module's declared edges join two ids INSIDE one graph, so the slice
// cannot have asserted a pair that SPANS two accounts, and it cannot have
// asserted a same-account pair whose only connecting edge was declared by a
// DIFFERENT account either. The comparison below is the first; the account in
// the key is the second, and comparing the two resources alone would miss it
// entirely because under it both of them sit in the account that declared
// nothing.
//
// AN EMPTY RESOURCE ID IS NOT GUARDED HERE, and its absence is a finding rather
// than an omission. A guard for it stood here and nothing could discriminate it:
// the build skips an edge with an empty endpoint, so no key ever contains an
// empty id and a lookup carrying one always misses. A guard whose absence no
// test can notice reads as protection and protects nothing.
// TestTheOracleRefusesAnEmptyResourceID asserts the behavior, which the lookup
// itself produces.
func (r *cloudResolver) HasDependency(a, b correlation.ResolvedResource) bool {
	if a.Account != b.Account {
		return false
	}
	_, ok := r.dependsOn[dependencyKey(a.Account, a.ID, b.ID)]
	return ok
}

// firstNonEmpty returns the first non-empty of its arguments.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
