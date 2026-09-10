// SPDX-License-Identifier: Apache-2.0

package logpipe

// cardinality.go — which label keys become SHARED GRAPH NODES and which ride
// inline on the stream.
//
// THIS DECIDES THE GRAPH'S SHAPE, not just its size. A low-cardinality key
// becomes one log-label node per distinct value with a HAS_LABEL edge from
// every stream carrying it, which is what makes "every stream in this
// namespace" a graph walk. A high-cardinality key becomes a metadata entry on
// the stream and nothing else, because a node per value would be a node per
// stream and the edges would carry no grouping.
//
// THE CLASSIFICATION IS A PROPERTY OF THE WHOLE ENTRY SET, so every value must
// be observed BEFORE any stream is built. Classifying as streams are built
// would make a key's class depend on the order entries arrived in.

// DefaultCardinalityThreshold is the number of distinct values at which a key
// stops being shared. It is a count of NODES the key would create, so the
// number is about graph size rather than about the source.
const DefaultCardinalityThreshold = 500

// CardinalityTracker counts distinct values per label key.
type CardinalityTracker struct {
	counts    map[string]map[string]struct{}
	threshold int
}

// NewCardinalityTracker returns a tracker. A threshold at or below zero means
// the default rather than "share nothing" — zero is what an unset field looks
// like, and reading it as a real threshold would silently drop every label node
// from the graph.
func NewCardinalityTracker(threshold int) *CardinalityTracker {
	if threshold <= 0 {
		threshold = DefaultCardinalityThreshold
	}
	return &CardinalityTracker{
		counts:    make(map[string]map[string]struct{}),
		threshold: threshold,
	}
}

// Observe records one value for a key.
func (ct *CardinalityTracker) Observe(key, value string) {
	vals, ok := ct.counts[key]
	if !ok {
		vals = make(map[string]struct{})
		ct.counts[key] = vals
	}
	vals[value] = struct{}{}
}

// IsLowCardinality reports whether a key has fewer distinct values than the
// threshold. A key never observed counts as low: it contributes no nodes, so
// the answer is free, and treating an unobserved key as high would make a
// caller that classifies before observing produce a graph with no label nodes
// at all.
func (ct *CardinalityTracker) IsLowCardinality(key string) bool {
	return len(ct.counts[key]) < ct.threshold
}

// Classify splits one label set on the observed classification.
func (ct *CardinalityTracker) Classify(labels map[string]string) (lowCard, highCard map[string]string) {
	lowCard = make(map[string]string, len(labels))
	highCard = make(map[string]string)
	for k, v := range labels {
		if ct.IsLowCardinality(k) {
			lowCard[k] = v
		} else {
			highCard[k] = v
		}
	}
	return lowCard, highCard
}
