// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// pipeline.go — THE STAGE ORDER, which is itself part of the specification.
//
// Clustering runs over the whole entry set before any per-entry template id is
// read, and the cardinality tracker observes every entry before any stream is
// built. Both are whole-collect passes for the same reason: a template's id
// moves while later entries broaden it, and a label key's classification
// depends on values that may only appear in a later stream. A stage order that
// resolved either one incrementally would produce a graph that depends on the
// order events arrived in.

// buildGraph turns normalized entries and whatever cloud context the collect
// carries into the contract's nodes and edges.
func buildGraph(entries []LogEntry, cloud CloudContext) ([]framework.Node, []framework.Edge, error) {
	// Severity is re-derived from the message body BEFORE clustering, because
	// a template's aggregate severity is the maximum over its entries and its
	// alias suffix comes from that aggregate. Reclassifying afterwards would
	// leave every alias describing the pre-reclassification level.
	entries = reclassifySeverity(entries)

	templates, entryTemplateIDs := processEntries(entries, DefaultDrainConfig())
	streams, entryStreamIDs := buildStreams(entries, DefaultCardinalityThreshold)

	chunks, err := assembleChunks(entries, entryStreamIDs, entryTemplateIDs)
	if err != nil {
		return nil, nil, err
	}

	nodes, edges := assembleGraph(templates, streams, chunks)

	// THE RESOLUTIONS SERVE BOTH CLOUD-LINKED HALVES. The proxy nodes and their
	// EMITTED_BY edges are built from them directly, and the correlation
	// evidence names the same resources through proxyMapFrom, so an edge and the
	// evidence citing it cannot disagree about which resource a label named.
	resolutions := resolveStreams(streams, cloud)
	proxyNodes, proxyEdges := materializeProxies(resolutions)
	nodes = append(nodes, proxyNodes...)
	edges = append(edges, proxyEdges...)

	// THE DETECTOR'S REFUSALS ARE THIS WALK'S REFUSALS. It rejects a nil
	// template, chunk or stream, an empty template or stream id, and a time
	// range that ends before it starts — all of which are this pipeline's own
	// output being malformed. Returning the error rather than dropping the
	// correlation half means such a walk writes nothing at all, which is what
	// the contract's whole-or-nothing rule asks and what keeps a structurally
	// broken graph from being asserted as a complete collect.
	//
	// NO ENTRY SET REACHES THIS BRANCH TODAY, and saying so is part of keeping
	// it honest: drain gives every template a non-empty id, a cluster's
	// FirstSeen is its minimum and its LastSeen its maximum, and neither
	// buildStreams nor assembleChunks emits a nil element. It is defensive
	// against a future change to THIS pipeline rather than against any input a
	// collect can carry, and correlate_test.go names that where it tests the
	// refusals themselves.
	correlations, err := findCorrelations(templates, chunks, streams, proxyMapFrom(resolutions), cloud)
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, correlation.MaterializeCorrelations(correlations)...)

	return nodes, edges, nil
}

// processEntries clusters every entry, consolidates the resulting templates,
// and returns the survivors beside a parallel slice naming each entry's final
// template.
//
// THE PER-ENTRY IDS ARE READ AFTER ALL CLUSTERING, from the cluster POINTER
// captured during it. A template's id is recomputed every time a later entry
// broadens its pattern, so an id captured as the entry was added would name a
// template that no longer exists by the end of the walk — and the chunk built
// from it would hang off nothing.
func processEntries(entries []LogEntry, cfg DrainConfig) ([]*LogTemplate, []string) {
	drain := NewDrainEngine(cfg)
	entryTemplates := make([]*LogTemplate, len(entries))
	for i, e := range entries {
		entryTemplates[i] = drain.AddMessage(e)
	}

	before := drain.Templates()
	after, absorbedBy := runConsolidators(DefaultConsolidators(), before)
	remap := buildTemplateRemap(before, after, absorbedBy)

	entryTemplateIDs := make([]string, len(entries))
	for i, tpl := range entryTemplates {
		if tpl == nil {
			// An entry whose message tokenized to nothing joined no cluster.
			// Its id stays empty and chunk assembly skips it, so it
			// contributes no chunk rather than a chunk pointing at nothing.
			continue
		}
		id := tpl.ID
		if mapped, ok := remap[id]; ok {
			id = mapped
		}
		entryTemplateIDs[i] = id
	}
	return after, entryTemplateIDs
}
