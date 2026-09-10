// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"
)

// chunk.go — GROUPING ENTRIES INTO CHUNKS, and the two properties that decide
// whether a re-collect carries its nodes forward.

// DefaultChunkWindow is the width of a chunk's time bucket.
const DefaultChunkWindow = 5 * time.Minute

// chunkContentTimeLayout renders a chunk line's timestamp. It is RFC3339 with
// nanoseconds, fixed width, so the lines sort as text exactly as they sort as
// instants.
const chunkContentTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// chunkKey identifies one (stream, template, window) bucket.
type chunkKey struct {
	StreamID    string
	TemplateID  string
	WindowStart time.Time
}

// assembleChunks groups entries into chunks and renders each one's content.
//
// Entries whose stream or template id is empty are skipped: an empty template
// id means the message tokenized to nothing and joined no cluster, so there is
// no template node for a chunk to hang off.
//
// The bucketing window is DefaultChunkWindow: it is the only window anything in
// this collector groups by, so it is a constant here rather than a parameter
// every caller passes the same value for.
func assembleChunks(
	entries []LogEntry,
	entryStreamIDs []string,
	entryTemplateIDs []string,
) ([]*LogChunk, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entryStreamIDs) != len(entries) || len(entryTemplateIDs) != len(entries) {
		return nil, fmt.Errorf(
			"cloudwatch: internal error assembling chunks: %d entries against %d stream ids and %d template ids",
			len(entries), len(entryStreamIDs), len(entryTemplateIDs))
	}
	buckets := make(map[chunkKey][]LogEntry)
	for i, e := range entries {
		sid, tid := entryStreamIDs[i], entryTemplateIDs[i]
		if sid == "" || tid == "" {
			continue
		}
		k := chunkKey{StreamID: sid, TemplateID: tid, WindowStart: windowStart(e.Timestamp)}
		buckets[k] = append(buckets[k], e)
	}
	return buildChunks(buckets), nil
}

// buildChunks renders one chunk per bucket and sorts the result by stream,
// template and start time. The sort is what makes the emitted slice
// order-stable: map iteration is not.
func buildChunks(buckets map[chunkKey][]LogEntry) []*LogChunk {
	chunks := make([]*LogChunk, 0, len(buckets))
	for k, grouped := range buckets {
		start, end := entryTimeRange(grouped)
		chunks = append(chunks, &LogChunk{
			ID:         chunkID(k.StreamID, k.TemplateID, k.WindowStart),
			StreamID:   k.StreamID,
			TemplateID: k.TemplateID,
			StartTime:  start,
			EndTime:    end,
			Content:    encodeChunkContent(grouped),
			EntryCount: len(grouped),
		})
	}
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].StreamID != chunks[j].StreamID {
			return chunks[i].StreamID < chunks[j].StreamID
		}
		if chunks[i].TemplateID != chunks[j].TemplateID {
			return chunks[i].TemplateID < chunks[j].TemplateID
		}
		return chunks[i].StartTime.Before(chunks[j].StartTime)
	})
	return chunks
}

// windowStart floors a timestamp to its bucket.
//
// THE BUCKETS ARE ALIGNED TO THE UTC EPOCH, not to the requested range and not
// to the first entry in the batch. That is the property a re-collect rides: the
// requested window is a tool parameter and changes between collects by design,
// so a bucket floored relative to it would shift on every collect and every
// chunk id with it — the carry-forward would silently never happen, and no test
// that collects once could see it.
//
// THE WINDOW IS DefaultChunkWindow BY CONSTRUCTION: assembleChunks no longer takes
// one, so this floors to the one bucket width the collector has.
func windowStart(ts time.Time) time.Time {
	w := DefaultChunkWindow.Nanoseconds()
	return time.Unix(0, (ts.UnixNano()/w)*w).UTC()
}

// chunkID is "log-chunk:" followed by 32 hex characters: sha256 truncated to
// the first 16 BYTES over the stream id, a "|", the template id, a "|" and the
// window start.
//
// THE WINDOW START IS FOLDED IN AS EIGHT BIG-ENDIAN BYTES, not as the decimal
// digits of its nanosecond count. An implementation that concatenates the
// number as text produces a different id for the same chunk.
func chunkID(streamID, templateID string, start time.Time) string {
	var b bytes.Buffer
	b.WriteString(streamID)
	b.WriteByte('|')
	b.WriteString(templateID)
	b.WriteByte('|')
	_ = binary.Write(&b, binary.BigEndian, start.UnixNano())
	h := sha256.Sum256(b.Bytes())
	return fmt.Sprintf("log-chunk:%x", h[:16])
}

// entryTimeRange returns the minimum and maximum timestamp among entries.
//
// These are the chunk's start_time and end_time, and they are the ENTRIES'
// instants rather than the window's edges: a chunk in the 12:05 bucket whose
// entries span 12:06:14 to 12:07:02 carries those two, not the bucket. Writing
// the bucket instead is wrong in every chunk and raises no error.
func entryTimeRange(entries []LogEntry) (time.Time, time.Time) {
	if len(entries) == 0 {
		return time.Time{}, time.Time{}
	}
	start, end := entries[0].Timestamp, entries[0].Timestamp
	for _, e := range entries[1:] {
		if e.Timestamp.Before(start) {
			start = e.Timestamp
		}
		if e.Timestamp.After(end) {
			end = e.Timestamp
		}
	}
	return start, end
}

// encodeChunkContent renders a chunk's entries as the node's Content: one line
// per entry, the entry's RFC3339-nano timestamp, a tab, and the message, in
// ascending timestamp order.
//
// WHY PLAIN TEXT RATHER THAN THE BUILT-IN'S COMPRESSED BLOCK, which is this
// collector's one deliberate divergence from the built-in chunk shape. Two
// independent reasons, either sufficient. First, transport: this result crosses
// the wire as JSON, and encoding/json replaces every invalid UTF-8 byte in a Go
// string with the replacement rune, so a compressed block arrives corrupted and
// NO error is returned on the way out or back. Second, lifecycle: this graph is
// summarized, embedded and text-indexed, where the built-in log graph is none
// of those, so a compressed blob would be fed to a summarizer and an embedder
// as garbage. Compression was a storage choice of a graph nobody indexed; the
// produced VOCABULARY is what parity names.
//
// THE PER-ENTRY TIMESTAMP IS CARRIED EXPLICITLY because it is the only
// per-entry temporal data in the vocabulary — the chunk's own start_time and
// end_time bound the whole chunk, so dropping the per-line stamp would lose a
// property the parity target carries.
func encodeChunkContent(entries []LogEntry) string {
	ordered := make([]LogEntry, len(entries))
	copy(ordered, entries)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Timestamp.Before(ordered[j].Timestamp)
	})
	var b strings.Builder
	for _, e := range ordered {
		b.WriteString(e.Timestamp.UTC().Format(chunkContentTimeLayout))
		b.WriteByte('\t')
		b.WriteString(e.Message)
		b.WriteByte('\n')
	}
	return b.String()
}
