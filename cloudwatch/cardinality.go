// SPDX-License-Identifier: Apache-2.0

package main

// cardinality.go — THE CLASSIFICATION THAT DECIDES THE GRAPH'S SHAPE.
//
// It is not a field value: a low-cardinality label becomes a shared node plus
// one HAS_LABEL edge per stream that carries it, and a high-cardinality one
// stays inline on the stream node with no node and no edge. A collector that
// classified everything one way emits either a label node per stream or none at
// all, and both are quietly wrong.

// DefaultCardinalityThreshold is the number of distinct values a key may hold
// while staying low-cardinality. The comparison is strictly less-than, so a key
// AT the threshold is high-cardinality.
const DefaultCardinalityThreshold = 500

// CardinalityTracker counts the distinct values seen per label key.
type CardinalityTracker struct {
	counts    map[string]map[string]struct{}
	threshold int
}

// NewCardinalityTracker builds a tracker; a non-positive threshold means the
// default.
func NewCardinalityTracker(threshold int) *CardinalityTracker {
	if threshold <= 0 {
		threshold = DefaultCardinalityThreshold
	}
	return &CardinalityTracker{counts: make(map[string]map[string]struct{}), threshold: threshold}
}

// Observe records one key/value pair.
func (ct *CardinalityTracker) Observe(key, value string) {
	vals, ok := ct.counts[key]
	if !ok {
		vals = make(map[string]struct{})
		ct.counts[key] = vals
	}
	vals[value] = struct{}{}
}

// IsLowCardinality reports whether a key is under the threshold.
//
// A KEY NEVER OBSERVED READS AS LOW-CARDINALITY, because an absent key has zero
// distinct values. That is the built-in behavior and it is the safe default: an
// unobserved key becomes a shared node rather than disappearing inline.
func (ct *CardinalityTracker) IsLowCardinality(key string) bool {
	return len(ct.counts[key]) < ct.threshold
}

// Classify splits one label set by the tracker's observations.
//
// THE OBSERVE PASS IS WHOLE-COLLECT, which is why buildStreams walks every
// entry before it builds any stream: a key crosses the threshold because of
// entries anywhere in the collect, including in streams built later, so the
// classification of an early stream depends on the whole input rather than on
// itself.
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
