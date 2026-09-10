// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// stream.go — STREAM IDENTITY, and the TWO DISTINCT HASHES over two different
// label sets that a reimplementation most often conflates.

// FingerprintLabels hashes a label set: keys sorted, each written as
// "key=value\n", sha256, rendered WHOLE — 64 hex characters, not truncated like
// a template or chunk id.
//
// An empty set hashes the empty input rather than returning "", so a stream
// with no labels still has a stable identity.
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
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// NewLogStream builds a stream from a full label set.
//
// THE ID AND THE FINGERPRINT ARE TWO HASHES OVER TWO DIFFERENT INPUTS: the id
// covers the FULL label set, the fingerprint only the LOW-CARD ones. Two
// streams differing in one high-cardinality label therefore share a fingerprint
// and have different ids, which is exactly what the fingerprint is for.
func NewLogStream(labels map[string]string, tracker *CardinalityTracker) *LogStream {
	lowCard, highCard := tracker.Classify(labels)
	s := &LogStream{
		ID:             FingerprintLabels(labels),
		Labels:         labels,
		LowCardLabels:  lowCard,
		HighCardLabels: highCard,
		Fingerprint:    FingerprintLabels(lowCard),
	}
	s.Alias = AliasFor(s)
	return s
}

// LabelNodeID is a label node's deterministic id: the literal "log-label:"
// followed by the key, "=" and the value. It is not hashed — the pair is short
// and readable, and readers reference it directly.
func LabelNodeID(key, value string) string { return "log-label:" + key + "=" + value }

// buildStreams groups entries into streams in TWO PASSES.
//
// Pass one observes every label value of every entry so the tracker's
// classification reflects the whole collect; pass two builds one stream per
// distinct label fingerprint. Collapsing them into one pass would classify each
// stream against only the entries seen so far, making an early stream's shape
// depend on arrival order.
//
// It returns the streams and a parallel slice naming each entry's stream.
func buildStreams(entries []LogEntry, threshold int) ([]*LogStream, []string) {
	tracker := NewCardinalityTracker(threshold)
	for _, e := range entries {
		for k, v := range e.Labels {
			tracker.Observe(k, v)
		}
	}

	streamsByID := make(map[string]*LogStream)
	order := make([]string, 0, len(entries))
	entryStreamIDs := make([]string, len(entries))
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
		streamsByID[id] = NewLogStream(labels, tracker)
		order = append(order, id)
	}

	// FIRST-OBSERVED ORDER, NOT MAP ORDER. The built-in pipeline returns
	// streams in Go map order, which differs between runs of the same input;
	// this collector returns them in the order the entries first named them so
	// the emitted node slice is byte-identical across runs. Node IDENTITY does
	// not depend on it — every id is a hash — but a deterministic slice is what
	// lets a golden file compare whole outputs rather than sorted projections.
	streams := make([]*LogStream, 0, len(order))
	for _, id := range order {
		streams = append(streams, streamsByID[id])
	}
	return streams, entryStreamIDs
}
