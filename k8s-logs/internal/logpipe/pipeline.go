// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// pipeline.go — the one call that turns entries into the graph.
//
// THE ORDER IS NOT INTERCHANGEABLE. Severity is reclassified before clustering,
// because a template's level is taken from its entries. Streams are built
// before chunks, because a chunk is keyed on a stream id. Templates are
// consolidated before chunks, because consolidation moves template ids. And the
// cloud half runs last, because it attaches to label nodes the streams
// produced.

// Options tunes one pipeline run. Every field has a working zero value except
// the cloud slice, which is absent rather than empty when no context was
// supplied.
type Options struct {
	// Drain tunes clustering. The zero value means DefaultDrainConfig.
	Drain DrainConfig
	// ChunkWindow is the chunk bucket width. Zero means DefaultChunkWindow.
	ChunkWindow time.Duration
	// CardinalityThreshold is where a label key stops being shared. Zero means
	// DefaultCardinalityThreshold.
	CardinalityThreshold int
	// Resolutions maps log labels to cloud resources. Supplied by the caller
	// from the collect input's declared foreign-graph context block; empty when
	// none was supplied, which emits no proxies and no EMITTED_BY edges.
	Resolutions []Resolution
	// ProxyMap maps a service label VALUE to the "account:resource" diagnostic
	// string a correlation carries. It is the caller's own resolution set.
	ProxyMap map[string]string
	// Resolver and Oracle are the two cloud facts no collector process can
	// derive: which resource a service label names, and whether two resources
	// depend on each other. BOTH ARE OPTIONAL AND A NIL VALUE IS MEANINGFUL: a
	// collect that carried no declared cloud context resolves nothing and
	// confirms nothing, which is the honest zero and not a degraded one.
	//
	// They are the COMMON DETECTOR'S interfaces rather than this package's, so a
	// caller writes one implementation and it travels unwrapped.
	Resolver correlation.Resolver
	Oracle   correlation.DependencyOracle
}

// Result is one pipeline run's output: the graph, plus the intermediate
// products a caller may want to report on.
type Result struct {
	Nodes     []framework.Node
	Edges     []framework.Edge
	Templates []*Template
	Streams   []*Stream
	Chunks    []*Chunk
	// Correlations holds every candidate, confirmed or not. Only the confirmed
	// ones became edges; the rest are material for a summary, never facts.
	Correlations []correlation.Result
}

// Build runs the whole pipeline over a set of entries.
func Build(entries []Entry, opts Options) (Result, error) {
	drainCfg := opts.Drain
	if drainCfg.SimThreshold == 0 && drainCfg.MaxDepth == 0 && drainCfg.MaxChildren == 0 && drainCfg.MaxClusters == 0 {
		drainCfg = DefaultDrainConfig()
	}

	entries = ReclassifySeverity(entries)
	templates, entryTemplateIDs := ProcessEntries(entries, drainCfg)
	streams, entryStreamIDs := BuildStreams(entries, opts.CardinalityThreshold)
	chunks, err := AssembleChunks(entries, entryStreamIDs, entryTemplateIDs, templates, opts.ChunkWindow)
	if err != nil {
		return Result{}, err
	}

	nodes, edges := AssembleGraph(templates, streams, chunks)

	proxyNodes, proxyEdges, err := MaterializeProxies(opts.Resolutions, labelNodeIDs(nodes))
	if err != nil {
		return Result{}, err
	}
	nodes = append(nodes, proxyNodes...)
	edges = append(edges, proxyEdges...)

	correlations, err := findCorrelations(templates, chunks, streams, opts)
	if err != nil {
		return Result{}, err
	}
	edges = append(edges, correlation.MaterializeCorrelations(correlations)...)

	return Result{
		Nodes:        nodes,
		Edges:        edges,
		Templates:    templates,
		Streams:      streams,
		Chunks:       chunks,
		Correlations: correlations,
	}, nil
}

// labelNodeIDs is the set of label node ids a node slice carries. It is what
// MaterializeProxies checks a supplied resolution against.
func labelNodeIDs(nodes []framework.Node) map[string]struct{} {
	out := make(map[string]struct{})
	for _, n := range nodes {
		if n.Type == NodeLogLabel {
			out[n.ID] = struct{}{}
		}
	}
	return out
}

// ValidateEntries refuses an entry set the pipeline cannot describe honestly.
// An entry with no labels at all would join the single unlabeled stream, whose
// alias is empty and whose identity is a hash of nothing — which reads in the
// graph as a real source and is not one.
func ValidateEntries(entries []Entry) error {
	for i, e := range entries {
		if len(e.Labels) == 0 {
			return fmt.Errorf("logpipe: entry %d carries no labels, so it identifies no log source", i)
		}
	}
	return nil
}
