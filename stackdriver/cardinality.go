// SPDX-License-Identifier: Apache-2.0

package main

// cardinality.go — the LABEL CLASSIFIER that decides which labels become shared
// graph nodes and which stay inline on their stream.
//
// THE CLASSIFICATION IS A PROPERTY OF THE WHOLE COLLECT, NOT OF ONE ENTRY. A
// key is low-cardinality when the number of distinct values it took across
// every entry in this collect stays under the threshold, so the tracker must
// observe everything before it classifies anything. That also means the
// classification can FLIP between two collects when a key crosses the threshold
// — which moves a stream's Fingerprint and its label-node set while leaving its
// ID alone, because the id hashes all labels and the fingerprint hashes only
// the low-cardinality ones.

// defaultCardinalityThreshold is the number of distinct values at which a label
// key stops being worth a shared node. A key with more values than this
// produces roughly one node per stream, which is a node set nobody queries by.
const defaultCardinalityThreshold = 500

// cardinalityTracker counts the distinct values seen per label key.
type cardinalityTracker struct {
	counts    map[string]map[string]struct{}
	threshold int
}

// newCardinalityTracker builds a tracker. A non-positive threshold takes the
// default rather than meaning "everything is high-cardinality": zero is what an
// unset field looks like, and reading it as a threshold would silently empty
// the label-node set.
func newCardinalityTracker(threshold int) *cardinalityTracker {
	if threshold <= 0 {
		threshold = defaultCardinalityThreshold
	}
	return &cardinalityTracker{
		counts:    make(map[string]map[string]struct{}),
		threshold: threshold,
	}
}

// observe records one value for one key.
func (ct *cardinalityTracker) observe(key, value string) {
	vals, ok := ct.counts[key]
	if !ok {
		vals = make(map[string]struct{})
		ct.counts[key] = vals
	}
	vals[value] = struct{}{}
}

// isLowCardinality reports whether a key stayed under the threshold. The
// comparison is strict, so a key at exactly the threshold is HIGH-cardinality:
// the threshold is the first count that is too many.
//
// A key never observed reads as low-cardinality, which is the right answer for
// the only way that happens — classifying a label set the tracker was not shown.
func (ct *cardinalityTracker) isLowCardinality(key string) bool {
	return len(ct.counts[key]) < ct.threshold
}

// classify splits a label set on the observed cardinality.
func (ct *cardinalityTracker) classify(labels map[string]string) (lowCard, highCard map[string]string) {
	lowCard = make(map[string]string, len(labels))
	highCard = make(map[string]string)
	for k, v := range labels {
		if ct.isLowCardinality(k) {
			lowCard[k] = v
		} else {
			highCard[k] = v
		}
	}
	return lowCard, highCard
}
