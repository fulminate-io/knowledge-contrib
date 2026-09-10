// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// stream.go — stream identity, the cardinality split, and the label vocabulary.
//
// TWO HASHES OVER TWO DIFFERENT LABEL SETS. The stream ID hashes the FULL label
// set and is therefore stable across collects; the fingerprint hashes the
// LOW-CARDINALITY subset only, and that subset is computed over one walk's
// entries, so it can move between collects while the id does not. That
// asymmetry is what a carry-forward diff rests on.

// DefaultCardinalityThreshold is the number of distinct values at which a label
// key stops being shared as a node and is stored inline on the stream instead.
const DefaultCardinalityThreshold = 500

// CardinalityTracker counts distinct values per label key over ONE walk.
type CardinalityTracker struct {
	counts    map[string]map[string]struct{}
	threshold int
}

// NewCardinalityTracker builds a tracker. A threshold at or below zero takes
// DefaultCardinalityThreshold.
func NewCardinalityTracker(threshold int) *CardinalityTracker {
	if threshold <= 0 {
		threshold = DefaultCardinalityThreshold
	}
	return &CardinalityTracker{counts: make(map[string]map[string]struct{}), threshold: threshold}
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
// threshold. A key NEVER OBSERVED reads as LOW, because zero is fewer than any
// positive threshold — that is the arm a classifier written as "observed and
// under" gets wrong.
func (ct *CardinalityTracker) IsLowCardinality(key string) bool {
	return len(ct.counts[key]) < ct.threshold
}

// Classify splits one label set by the observed cardinality of its keys.
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

// FingerprintLabels is the hex sha256 over the label set rendered as sorted
// "key=value\n" lines. The SORT is what makes it an identity rather than a
// function of Go's map iteration order; an unsorted or partial rendering
// produces a different id for the same stream on every run.
//
// AN EMPTY LABEL SET NEEDS NO SPECIAL CASE, and this is written down because
// the obvious one is dead code: an empty set renders to the empty string, and
// sha256 over the empty string is byte-identical to sha256 over nil. A branch
// returning the second would be a guard no test could ever distinguish from its
// absence, which is worse than none.
func FingerprintLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte('\n')
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// NewStream builds one stream from a full label set and the walk's tracker.
func NewStream(labels map[string]string, tracker *CardinalityTracker) *Stream {
	lowCard, highCard := tracker.Classify(labels)
	s := &Stream{
		ID:             FingerprintLabels(labels),
		Labels:         labels,
		LowCardLabels:  lowCard,
		HighCardLabels: highCard,
		Fingerprint:    FingerprintLabels(lowCard),
	}
	s.Alias = AliasFor(s)
	return s
}

// LabelNodeID is the deterministic id of a shared label node. The "log-label:"
// prefix is part of the id, not a display convention.
func LabelNodeID(key, value string) string { return "log-label:" + key + "=" + value }
