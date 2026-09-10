// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"
	"time"
)

// pipeline.go — the whole transform, from entries to the emitted graph.
//
// It is SERIAL by construction rather than by omission: Drain's cluster state
// evolves with every message, so two messages cannot be clustered concurrently
// and mean anything. The walk's wall clock is dominated by clustering rather
// than by Loki round trips, and reaching for concurrency inside it would change
// the emitted template set.

// Options tunes the transform. The zero value takes every default.
type Options struct {
	Drain                DrainConfig
	ChunkWindow          time.Duration
	CardinalityThreshold int
}

// withDefaults fills the unset knobs. A zero DrainConfig is replaced whole
// rather than field by field: a config with SimThreshold set and MaxClusters
// zero would cluster every message into one template, so a partially filled
// config is an error rather than a default.
func (o Options) withDefaults() (Options, error) {
	if o.Drain == (DrainConfig{}) {
		o.Drain = DefaultDrainConfig()
	} else if o.Drain.MaxDepth <= 0 || o.Drain.MaxChildren <= 0 || o.Drain.MaxClusters <= 0 {
		return Options{}, fmt.Errorf(
			"logpipe: a partially filled DrainConfig is not a default: MaxDepth=%d MaxChildren=%d MaxClusters=%d must all be positive",
			o.Drain.MaxDepth, o.Drain.MaxChildren, o.Drain.MaxClusters)
	}
	if o.ChunkWindow <= 0 {
		o.ChunkWindow = DefaultChunkWindow
	}
	if o.CardinalityThreshold <= 0 {
		o.CardinalityThreshold = DefaultCardinalityThreshold
	}
	return o, nil
}

// Graph is one walk's product: the four unconditional node families plus the
// per-entry mapping the chunk keys were built from.
type Graph struct {
	Templates []*Template
	Streams   []*Stream
	Chunks    []*Chunk
}

// Build runs the whole transform over pre-read entries.
func Build(entries []Entry, opts Options) (*Graph, error) {
	opts, err := opts.withDefaults()
	if err != nil {
		return nil, err
	}
	templates, entryTemplateIDs, err := processEntries(entries, opts.Drain)
	if err != nil {
		return nil, err
	}
	streams, entryStreamIDs := buildStreams(entries, opts.CardinalityThreshold)
	chunks, err := assembleChunks(entries, entryStreamIDs, entryTemplateIDs, templates, opts.ChunkWindow)
	if err != nil {
		return nil, err
	}
	return &Graph{Templates: templates, Streams: streams, Chunks: chunks}, nil
}

// processEntries clusters every entry, consolidates the template set, and
// resolves each entry to its FINAL template id.
//
// THE ID IS READ AFTER ALL CLUSTERING, NOT AT AddMessage TIME. Drain rewrites a
// cluster's id whenever a later message broadens its pattern, so the pointer is
// captured in pass one and dereferenced in pass three; reading the id inside
// pass one would give entries of one cluster different ids.
//
// An entry whose message tokenizes to nothing contributes no template and gets
// the empty string, which chunk assembly skips.
func processEntries(entries []Entry, cfg DrainConfig) ([]*Template, []string, error) {
	drain := NewDrainEngine(cfg)
	entryTemplates := make([]*Template, len(entries))
	for i, e := range entries {
		entryTemplates[i] = drain.AddMessage(e)
	}

	templates := drain.Templates()
	before := templatesByID(templates)
	consolidated := RunConsolidators(DefaultConsolidators(), templates)
	after := templatesByID(consolidated.Templates)
	remap, err := buildTemplateRemap(before, after, consolidated.Absorbed)
	if err != nil {
		return nil, nil, err
	}

	entryTemplateIDs := make([]string, len(entries))
	for i, tpl := range entryTemplates {
		if tpl == nil {
			continue
		}
		id := tpl.ID
		if mapped, ok := remap[id]; ok {
			id = mapped
		}
		entryTemplateIDs[i] = id
	}
	return consolidated.Templates, entryTemplateIDs, nil
}

// buildStreams groups entries into streams in two passes: pass one observes
// every label VALUE of every entry so the tracker can classify keys, pass two
// builds one stream per distinct full label set.
//
// The two passes are what make the cardinality split collect-scoped, and the
// order is not an optimisation: classifying while observing would classify each
// key against a partial count.
func buildStreams(entries []Entry, threshold int) ([]*Stream, []string) {
	tracker := NewCardinalityTracker(threshold)
	for _, e := range entries {
		for k, v := range e.Labels {
			tracker.Observe(k, v)
		}
	}

	streamsByID := make(map[string]*Stream)
	entryStreamIDs := make([]string, len(entries))
	order := make([]string, 0)
	for i, e := range entries {
		labels := e.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		id := FingerprintLabels(labels)
		entryStreamIDs[i] = id
		if _, exists := streamsByID[id]; exists {
			continue
		}
		streamsByID[id] = NewStream(labels, tracker)
		order = append(order, id)
	}

	// FIRST-APPEARANCE ORDER, not map order. The built-in pipeline ranges a map
	// here, so its stream slice is shuffled on every run; nothing downstream
	// depends on the order today, and emitting a shuffled slice would make two
	// collects over identical input produce byte-different results for no
	// reason.
	streams := make([]*Stream, 0, len(order))
	for _, id := range order {
		streams = append(streams, streamsByID[id])
	}
	return streams, entryStreamIDs
}
