// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// pipeline.go — the ORDER THE STAGES RUN IN, which is the one thing about this
// module that cannot be read off any single file.
//
//	entries -> cluster -> consolidate -> remap -> streams -> chunks -> graph
//
// THREE ORDERING FACTS ARE LOAD-BEARING AND EACH IS A DEFECT IF REVERSED:
//
//   - The clusterer's template ids MOVE while it runs, because merging an entry
//     can broaden a pattern. So this pass records each entry's template POINTER
//     and reads the id only after clustering finishes. Recording the id at
//     cluster time gives entries in one cluster different ids.
//   - Consolidation runs BEFORE chunk assembly, because a chunk id folds the
//     template id: bucketing first and consolidating after would leave every
//     chunk keyed on a template that no longer exists.
//   - Stream classification observes the WHOLE entry set before classifying any
//     of it, which is why buildStreams is two passes and why it takes the entry
//     slice rather than being fed one entry at a time.

// pipelineConfig carries the tunables one collect runs under. They are values
// rather than constants so a test can drive a boundary — a five-minute chunk
// window and a five-hundred-value cardinality threshold are both expensive to
// reach with real data.
type pipelineConfig struct {
	Drain              drainConfig
	ChunkWindow        time.Duration
	CardinalityCutoff  int
	CorrelationEnabled bool
}

// defaultPipelineConfig is the configuration the emitted graph is defined at.
func defaultPipelineConfig() pipelineConfig {
	return pipelineConfig{
		Drain:              defaultDrainConfig(),
		ChunkWindow:        defaultChunkWindow,
		CardinalityCutoff:  defaultCardinalityThreshold,
		CorrelationEnabled: true,
	}
}

// pipelineOutput is one run's produced objects, before they become nodes.
type pipelineOutput struct {
	Templates   []*logTemplate
	Streams     []*logStream
	Chunks      []*logChunk
	Resolutions []resolvedProxyEntry
	// Correlations is the COMMON module's result type rather than one of this
	// module's: the detector is shared with every other logs collector, and a
	// local mirror of its result shape would be one more thing to drift.
	Correlations []correlation.Result
}

// runPipeline turns normalized entries into the produced objects.
//
// cloud is the foreign-graph knowledge this collect carries. It is nil at this
// tree, which yields no resolutions and no confirmed correlations — see
// resolve.go for why that zero is the correct result rather than a degraded one.
func runPipeline(entries []logEntry, cfg pipelineConfig, cloud cloudContext) (pipelineOutput, error) {
	templates, entryTemplateIDs := processEntries(entries, cfg.Drain)
	streams, entryStreamIDs := buildStreams(entries, cfg.CardinalityCutoff)
	chunks, err := assembleChunks(entries, entryStreamIDs, entryTemplateIDs, templates, cfg.ChunkWindow)
	if err != nil {
		return pipelineOutput{}, err
	}

	resolutions := computeStreamResolutions(streams, cloud)
	var correlations []correlation.Result
	if cfg.CorrelationEnabled {
		// THE ERROR IS PROPAGATED AND NO TEST CAN RED THIS BRANCH, which is worth
		// saying rather than leaving as an apparent gap. The detector refuses a
		// nil element, an empty template or stream id, and a range that runs
		// backwards; none of the three is producible here. drain.go creates a
		// template with FirstSeen and LastSeen both at the entry timestamp
		// (drain.go:139-140) and then widens by min and max (:288-293), and the
		// consolidators merge the same way (consolidator_gostack.go:118-125), so
		// a backwards range cannot arise; ids are derived and never empty; the
		// slices carry no nil. The refusal itself is observed one frame down, by
		// TestMalformedTemplatesReachTheDetectorAndAreRefused, which calls
		// findCorrelations directly. What this branch guarantees is that a future
		// producer which CAN emit one of those shapes fails the collect loudly
		// instead of correlating a set the pipeline never produced.
		correlations, err = findCorrelations(templates, chunks, streams, proxyDiagnosticMap(resolutions), cloud)
		if err != nil {
			return pipelineOutput{}, err
		}
	}

	return pipelineOutput{
		Templates:    templates,
		Streams:      streams,
		Chunks:       chunks,
		Resolutions:  resolutions,
		Correlations: correlations,
	}, nil
}

// processEntries clusters, consolidates, and resolves every entry to a template
// that is actually in the emitted set.
//
// THE FINAL LOOP IS WHERE THE ORPHAN DEFECT IS REFUSED. Every entry whose
// template was consolidated away is re-pointed through the remap, and an entry
// whose mapped target is somehow absent from the surviving set gets an EMPTY
// template id — which chunk assembly then skips — rather than keeping an id that
// names no node. The knowledge client's own pipeline keeps the dead id, and the
// CONTAINS edges built from it dangle in every logs graph it has written.
func processEntries(entries []logEntry, cfg drainConfig) ([]*logTemplate, []string) {
	drain := newDrainEngine(cfg)
	entryTemplates := make([]*logTemplate, len(entries))
	for i, e := range entries {
		entryTemplates[i] = drain.addMessage(e)
	}

	before := drain.templates()
	consolidated := runConsolidators(defaultConsolidators(), before)
	consolidated.Templates = foldDuplicateTemplates(consolidated.Templates)
	remap := buildTemplateRemap(before, consolidated)

	return consolidated.Templates, resolveEntryTemplateIDs(entryTemplates, remap, consolidated.Templates)
}

// resolveEntryTemplateIDs turns each entry's captured template POINTER into the
// id of a template that is actually in the emitted set.
//
// IT IS THE LAST GATE BEFORE THE ORPHAN DEFECT. The remap is total by
// construction — every dropped template has either an absorber or the total
// order's fallback — so in ordinary operation the live check below never fires.
// It fires when the remap is WRONG, which is exactly the case that must not
// reach the graph: an entry carrying an id that names no node produces a chunk
// whose CONTAINS edge dangles, and the client admits a dangling edge silently, so
// nothing downstream would report it. An entry with no live template gets an
// EMPTY id instead, which chunk assembly skips.
//
// It is a separate function so the failure it guards against is reachable in a
// test without a remap bug being introduced to reach it.
func resolveEntryTemplateIDs(entryTemplates []*logTemplate, remap map[string]string,
	surviving []*logTemplate) []string {
	live := make(map[string]struct{}, len(surviving))
	for _, t := range surviving {
		if t != nil {
			live[t.ID] = struct{}{}
		}
	}
	out := make([]string, len(entryTemplates))
	for i, tpl := range entryTemplates {
		if tpl == nil {
			continue
		}
		id := tpl.ID
		if mapped, ok := remap[id]; ok {
			id = mapped
		}
		if _, ok := live[id]; !ok {
			continue
		}
		out[i] = id
	}
	return out
}

// buildResult assembles the contract result from a pipeline run and the read's
// own completeness.
//
// THE COMPLETENESS ASSERTION IS THE READ'S, NOT THE PIPELINE'S. A truncated read
// produced a real graph of what it saw, so the nodes are emitted — but the walk
// says so, which is what stops the server from treating everything this collect
// did not carry as deleted.
func buildResult(out pipelineOutput, truncated bool, bound int) (framework.Result, error) {
	nodes, edges, err := assembleGraph(out.Templates, out.Streams, out.Chunks, out.Resolutions, out.Correlations)
	if err != nil {
		return framework.Result{}, err
	}
	complete := framework.Complete()
	if truncated {
		complete = framework.Incomplete(incompleteReason(bound))
	}
	return framework.Result{Nodes: nodes, Edges: edges, Complete: complete}, nil
}

// incompleteReason names why a walk stopped short, for the collector's own logs
// and for a reviewer judging whether the arm is reachable.
func incompleteReason(bound int) string {
	return fmt.Sprintf(
		"the entry bound of %d stopped the read before Cloud Logging was exhausted; "+
			"widen the bound or narrow the time range and filter", bound)
}
