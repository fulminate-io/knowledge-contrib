// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// stream.go — stream identity: the id, the fingerprint, and the label node ids.
//
// TWO HASHES OVER THE SAME FUNCTION, AND COLLAPSING THEM DESTROYS THE
// FINGERPRINT'S PURPOSE. The stream ID hashes the COMPLETE label set, so two
// pods are two streams. The FINGERPRINT hashes the LOW-CARDINALITY labels
// alone, so those two pods share a fingerprint and a reader can ask for
// "everything with this shape". Feeding the same map to both makes the
// fingerprint a second copy of the id and the grouping disappears with nothing
// failing.

// FingerprintLabels hashes a label map deterministically: keys sorted, joined
// as newline-terminated key=value lines, sha256, hex.
//
// THE OUTPUT IS THE FULL 64 HEX CHARACTERS, unlike the template id, which is
// truncated. The two are different lengths on purpose and a stream id
// truncated to match a template id names a node the log graph does not have.
//
// An empty map hashes the EMPTY INPUT rather than the empty string built from
// no lines. Both happen to be the same bytes here; it is written explicitly so
// a future change to the joining does not silently move the empty case.
func FingerprintLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return fmt.Sprintf("%x", sha256.Sum256(nil))
	}
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
	h := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", h)
}

// NewStream builds a stream from a complete label set and a tracker that has
// already observed every value in the entry set.
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

// LabelNodeID is a label node's id: a LITERAL, not a hash.
//
// It is readable on purpose — a reader looking at an EMITTED_BY edge or a
// HAS_LABEL edge can tell which label it names without a lookup — and it is
// also what lets the proxy edges below name a label node they never built.
func LabelNodeID(key, value string) string { return "log-label:" + key + "=" + value }

// BuildStreams groups entries into streams in the two passes the
// classification requires: pass one observes every label value, pass two builds
// one stream per distinct full label set.
//
// It returns the streams and a per-entry parallel slice of stream ids. The
// parallel slice is not a convenience: chunk assembly needs to know which
// stream each entry belongs to, and recomputing it later would rehash every
// entry's labels.
//
// A threshold at or below zero means the default; see [NewCardinalityTracker].
func BuildStreams(entries []Entry, threshold int) ([]*Stream, []string) {
	tracker := NewCardinalityTracker(threshold)
	for _, e := range entries {
		for k, v := range e.Labels {
			tracker.Observe(k, v)
		}
	}

	streamsByID := make(map[string]*Stream)
	entryStreamIDs := make([]string, len(entries))
	for i, e := range entries {
		labels := e.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		id := FingerprintLabels(labels)
		entryStreamIDs[i] = id
		if _, exists := streamsByID[id]; !exists {
			streamsByID[id] = NewStream(labels, tracker)
		}
	}

	// SORTED BY ID, not returned in map order. Map iteration is randomized per
	// run, and an unsorted slice here would make the emitted node ORDER differ
	// between two collects of identical input — which is not a correctness
	// failure for the graph (every node carries a deterministic id) but does
	// make a diff of two results unreadable and a golden fixture impossible.
	ids := make([]string, 0, len(streamsByID))
	for id := range streamsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	streams := make([]*Stream, 0, len(ids))
	for _, id := range ids {
		streams = append(streams, streamsByID[id])
	}
	return streams, entryStreamIDs
}
