// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// stream.go — the STREAM FINGERPRINT and stream assembly.
//
// THE FINGERPRINT IS THE RECONCILIATION KEY. A stream's id is the hash of its
// full label set, so two collects over the same entries produce the same stream
// ids and the second reconciles against the first instead of duplicating it.
// Every rule that decides what is IN the label set is therefore an identity
// rule, which is why normalize.go's empty-value skip and its two provenance
// labels carry their own tests.

// fingerprintLabels hashes a label set: keys sorted, each written as "k=v\n",
// sha256 of the whole, hex.
//
// THE SORT IS WHAT MAKES IT A FINGERPRINT rather than a hash of a map walk, and
// the trailing newline per pair is what stops two different label sets from
// sharing a preimage — without a terminator, {"ab":"c"} and {"a":"bc"} would
// both write "ab=c".
//
// An EMPTY label set hashes the empty input rather than returning a constant,
// so a stream with no labels still has a well-formed id in the same space.
func fingerprintLabels(labels map[string]string) string {
	if len(labels) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
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
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// newLogStream builds one stream from a label set and the collect's classifier.
func newLogStream(labels map[string]string, tracker *cardinalityTracker) *logStream {
	lowCard, highCard := tracker.classify(labels)
	s := &logStream{
		ID:             fingerprintLabels(labels),
		Labels:         labels,
		LowCardLabels:  lowCard,
		HighCardLabels: highCard,
		Fingerprint:    fingerprintLabels(lowCard),
	}
	s.Alias = aliasForStream(s)
	return s
}

// labelNodeID is the deterministic id of a shared label node. It is the one id
// in this module built by concatenation rather than by hashing, because it is
// also the FROM endpoint of every EMITTED_BY edge and a reader resolving one
// should be able to read the label out of the id.
func labelNodeID(key, value string) string { return "log-label:" + key + "=" + value }

// buildStreams groups entries into streams in two passes over the entry set.
//
// THE TWO PASSES ARE NOT AN OPTIMIZATION AND CANNOT BE COLLAPSED. Pass one
// shows the tracker every label value in the collect; pass two classifies. A
// single pass would classify each stream against the values seen SO FAR, so the
// first entry's stream and the last entry's stream would be classified under
// different thresholds and the emitted label-node set would depend on the order
// entries arrived in.
//
// It returns the streams and a parallel slice naming each entry's stream.
func buildStreams(entries []logEntry, threshold int) ([]*logStream, []string) {
	tracker := newCardinalityTracker(threshold)
	for _, e := range entries {
		for k, v := range e.Labels {
			tracker.observe(k, v)
		}
	}

	streamsByID := make(map[string]*logStream)
	order := make([]string, 0)
	entryStreamIDs := make([]string, len(entries))
	for i, e := range entries {
		labels := e.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		id := fingerprintLabels(labels)
		entryStreamIDs[i] = id
		if _, exists := streamsByID[id]; exists {
			continue
		}
		streamsByID[id] = newLogStream(labels, tracker)
		order = append(order, id)
	}

	// Sorted rather than returned in first-seen order: the emitted node list is
	// then a function of the entry SET rather than of the order it arrived in,
	// which is one fewer thing for a second collect to differ on.
	sort.Strings(order)
	streams := make([]*logStream, 0, len(order))
	for _, id := range order {
		streams = append(streams, streamsByID[id])
	}
	return streams, entryStreamIDs
}
