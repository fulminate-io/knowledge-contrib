// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy.go — the cloud-facing half of the log graph: proxy nodes standing for
// cloud resources, and the EMITTED_BY edges from a log label to its resource.
//
// A PROXY IS AN ORDINARY LOCAL NODE. Nothing about it points out of the graph
// it is written into: its cross-graph reference rides its ID, its Source and
// two metadata keys, and the edge below joins two ids INSIDE this result. That
// is why this module needs no cross-graph edge carrier — every endpoint it
// emits is its own.
//
// THE RESOLUTIONS ARE SUPPLIED, NOT COMPUTED HERE, and that is the honest
// boundary of what a collector process can know. The built-in pipeline gets its
// label-to-resource resolutions from a cloud SUBGRAPH the client fetches from
// the store; a collector child has no graph caller and can fetch nothing. So
// the cloud slice reaches this module through the collect input's declared
// foreign-graph context block, and everything below is a pure function of it.
//
// WITH NO CLOUD CONTEXT SUPPLIED, NOTHING HERE IS EMITTED — and that is parity,
// not a gap. The built-in pipeline emits exactly zero proxies and zero
// EMITTED_BY when no cloud graph is attached.

// ProxyID is a cloud proxy's node id. The shape is the convention both the
// client and the server build proxy ids under, so a proxy this collector emits
// and one the client builds for the same resource are the SAME node rather than
// two nodes for one resource.
func ProxyID(account, resourceID string) string {
	return "proxy:cloud:" + account + ":" + resourceID
}

// proxySource is a cloud proxy's Source: the id convention without the
// resource, which is what groups a graph's proxies by the account they came
// from.
func proxySource(account string) string { return "proxy:cloud:" + account }

// MaterializeProxies builds one proxy node per distinct resolved resource, and
// one EMITTED_BY edge per resolution from the label node to that proxy.
//
// knownLabels is the set of label node ids this result carries. A resolution
// naming a label no stream produced is an ERROR rather than a dangling edge: a
// dangling EMITTED_BY is a silent claim about a node that does not exist, and
// the supplied context is input this collector must not accept quietly when it
// does not match what the walk found. Pass a nil set to skip the check, which
// only a caller that has already made it should do.
func MaterializeProxies(resolutions []Resolution, knownLabels map[string]struct{}) ([]framework.Node, []framework.Edge, error) {
	if len(resolutions) == 0 {
		return nil, nil, nil
	}
	sorted := sortedResolutions(resolutions)

	nodes := make([]framework.Node, 0, len(sorted))
	edges := make([]framework.Edge, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	for _, r := range sorted {
		if r.Account == "" || r.ResourceID == "" {
			return nil, nil, fmt.Errorf(
				"logpipe: the cloud resolution for label %s=%s names no %s",
				r.LabelKey, r.LabelValue, missingResolutionField(r))
		}
		labelID := LabelNodeID(r.LabelKey, r.LabelValue)
		if knownLabels != nil {
			if _, ok := knownLabels[labelID]; !ok {
				return nil, nil, fmt.Errorf(
					"logpipe: the cloud resolution for %s=%s names a log label this walk did not emit (%s); "+
						"an EMITTED_BY edge from it would name a node that does not exist",
					r.LabelKey, r.LabelValue, labelID)
			}
		}
		proxyID := ProxyID(r.Account, r.ResourceID)
		if _, dup := seen[proxyID]; !dup {
			seen[proxyID] = struct{}{}
			nodes = append(nodes, proxyNode(r, proxyID))
		}
		edges = append(edges, framework.Edge{FromID: labelID, ToID: proxyID, Type: EdgeEmittedBy})
	}
	return nodes, edges, nil
}

// missingResolutionField names which half of a resolution is empty, so the
// refusal above tells an operator what to fix rather than that something is
// wrong.
func missingResolutionField(r Resolution) string {
	if r.Account == "" {
		return "cloud account"
	}
	return "cloud resource id"
}

// proxyNode renders one proxy. The display metadata is copied from the supplied
// cloud node when it carried any, so a reader rendering the proxy does not have
// to resolve back into the cloud graph for a type or a region.
func proxyNode(r Resolution, proxyID string) framework.Node {
	meta := map[string]string{
		"foreign_graph": "cloud",
		"foreign_id":    r.ResourceID,
		"account":       r.Account,
	}
	if r.ResourceType != "" {
		meta["resource_type"] = r.ResourceType
		meta["foreign_type"] = r.ResourceType
	}
	if r.Region != "" {
		meta["region"] = r.Region
	}
	if r.Provider != "" {
		meta["provider"] = r.Provider
	}
	return framework.Node{
		ID:         proxyID,
		Type:       NodeProxy,
		Source:     proxySource(r.Account),
		SymbolName: r.LabelKey + "=" + r.LabelValue,
		Description: fmt.Sprintf("cloud proxy for log label %s=%s (resolved to %s/%s)",
			r.LabelKey, r.LabelValue, r.Account, r.ResourceID),
		Metadata: meta,
	}
}

// sortedResolutions returns the resolutions in a stated total order, so the
// emitted node and edge ORDER is a function of the input alone and two collects
// of identical input produce byte-identical results.
func sortedResolutions(resolutions []Resolution) []Resolution {
	out := make([]Resolution, len(resolutions))
	copy(out, resolutions)
	sort.Slice(out, func(i, j int) bool {
		if out[i].LabelKey != out[j].LabelKey {
			return out[i].LabelKey < out[j].LabelKey
		}
		if out[i].LabelValue != out[j].LabelValue {
			return out[i].LabelValue < out[j].LabelValue
		}
		if out[i].Account != out[j].Account {
			return out[i].Account < out[j].Account
		}
		return out[i].ResourceID < out[j].ResourceID
	})
	return out
}
