// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/klauspost/compress/zstd"
)

// chunk.go — the CHUNK WINDOW, the chunk id, and the compressed entry payload.
//
// A chunk is the unit the entries themselves are stored in: one bucket of
// (stream, template, time window), holding each entry's timestamp and the
// variable values at its template's wildcard positions. The template recovers
// the skeleton text, so the payload never repeats it.

// defaultChunkWindow is the bucket width.
const defaultChunkWindow = 5 * time.Minute

// chunkIDBytes is how much of the bucket hash a chunk id carries.
const chunkIDBytes = 16

// chunkKey identifies one bucket during assembly.
type chunkKey struct {
	StreamID    string
	TemplateID  string
	WindowStart time.Time
}

// chunkPayloadEntry is one entry inside a chunk before compression.
type chunkPayloadEntry struct {
	Timestamp time.Time
	Vars      []string
}

// assembleChunks buckets entries and compresses each bucket.
//
// An entry with no stream or no template is SKIPPED rather than bucketed under
// an empty key: an empty template id names no node, so a chunk built on one
// would carry a CONTAINS edge from a template that does not exist. That is the
// dangling-edge defect this module refuses to inherit, and the skip is where it
// is refused.
func assembleChunks(
	entries []logEntry,
	entryStreamIDs []string,
	entryTemplateIDs []string,
	templates []*logTemplate,
	window time.Duration,
) ([]*logChunk, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entryStreamIDs) != len(entries) || len(entryTemplateIDs) != len(entries) {
		return nil, fmt.Errorf(
			"stackdriver: chunk assembly received %d entries but %d stream ids and %d template ids; "+
				"the three slices are parallel by construction",
			len(entries), len(entryStreamIDs), len(entryTemplateIDs))
	}
	if window <= 0 {
		window = defaultChunkWindow
	}

	tmplByID := make(map[string]*logTemplate, len(templates))
	for _, t := range templates {
		if t != nil {
			tmplByID[t.ID] = t
		}
	}

	buckets := make(map[chunkKey][]chunkPayloadEntry)
	for i, e := range entries {
		sid, tid := entryStreamIDs[i], entryTemplateIDs[i]
		if sid == "" || tid == "" {
			continue
		}
		k := chunkKey{StreamID: sid, TemplateID: tid, WindowStart: windowStart(e.Timestamp, window)}
		buckets[k] = append(buckets[k], chunkPayloadEntry{
			Timestamp: e.Timestamp,
			Vars:      extractEntryVars(tmplByID[tid], e.Message),
		})
	}
	return buildChunksFromBuckets(buckets)
}

// buildChunksFromBuckets compresses each bucket into a chunk, returning them
// sorted by (stream, template, start) so the emitted node list does not depend
// on a map walk.
func buildChunksFromBuckets(buckets map[chunkKey][]chunkPayloadEntry) ([]*logChunk, error) {
	chunks := make([]*logChunk, 0, len(buckets))
	for k, entries := range buckets {
		compressed, err := compressBytes(encodeChunkData(entries))
		if err != nil {
			return nil, fmt.Errorf("stackdriver: compressing the chunk for stream %s template %s: %w",
				k.StreamID, k.TemplateID, err)
		}
		start, end := entryTimeRange(entries)
		chunks = append(chunks, &logChunk{
			ID:             chunkID(k.StreamID, k.TemplateID, k.WindowStart),
			StreamID:       k.StreamID,
			TemplateID:     k.TemplateID,
			StartTime:      start,
			EndTime:        end,
			CompressedData: compressed,
			EntryCount:     len(entries),
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
	return chunks, nil
}

// windowStart floors a timestamp to its bucket.
//
// THE BUCKET IS ALIGNED TO THE UTC EPOCH, not to the collect and not to the
// first entry, and that is what makes a chunk id reproducible. Floor to the
// first entry instead and the same log lines collected over a different window
// land in differently-aligned buckets, so every chunk id moves and the second
// collect reconciles against nothing.
func windowStart(ts time.Time, window time.Duration) time.Time {
	if window <= 0 {
		return ts
	}
	w := window.Nanoseconds()
	if w == 0 {
		return ts
	}
	return time.Unix(0, (ts.UnixNano()/w)*w).UTC()
}

// chunkID is the deterministic id of a bucket: the truncated sha256 of
// stream|template|windowStart, with the window written as big-endian nanoseconds
// so the preimage is fixed-width and cannot be confused with a neighboring
// field's bytes.
func chunkID(streamID, templateID string, start time.Time) string {
	var b bytes.Buffer
	b.WriteString(streamID)
	b.WriteByte('|')
	b.WriteString(templateID)
	b.WriteByte('|')
	_ = binary.Write(&b, binary.BigEndian, start.UnixNano())
	sum := sha256.Sum256(b.Bytes())
	return "log-chunk:" + hex.EncodeToString(sum[:chunkIDBytes])
}

// entryTimeRange returns the earliest and latest timestamps in a bucket. These
// are the ACTUAL entry bounds rather than the window's, so a bucket holding one
// entry reports that entry's instant.
func entryTimeRange(entries []chunkPayloadEntry) (time.Time, time.Time) {
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

// extractEntryVars recovers one entry's variable values against its template.
// A token-count mismatch yields nil rather than a partial row: the mismatch
// means this entry's message no longer aligns with the template it was
// consolidated onto, and pairing values with the wrong wildcard positions would
// be worse than recording none.
func extractEntryVars(tpl *logTemplate, message string) []string {
	if tpl == nil {
		return nil
	}
	tplTokens := tokenize(tpl.Pattern)
	msgTokens := tokenize(preProcess(message))
	if len(tplTokens) != len(msgTokens) {
		return nil
	}
	return extractVars(tplTokens, msgTokens)
}

// encodeChunkData serializes a bucket to the compact form the payload is
// defined in:
//
//	[count:uvarint]
//	per entry: [ts_unix_nano:varint][var_count:uvarint]
//	           per var: [len:uvarint][bytes]
//
// It does not compress. Keeping the two steps separate is what lets a test
// exercise the encoding without a compressor in the way.
func encodeChunkData(entries []chunkPayloadEntry) []byte {
	var buf bytes.Buffer
	tmp := make([]byte, binary.MaxVarintLen64)

	n := binary.PutUvarint(tmp, uint64(len(entries)))
	buf.Write(tmp[:n])
	for _, e := range entries {
		n = binary.PutVarint(tmp, e.Timestamp.UnixNano())
		buf.Write(tmp[:n])
		n = binary.PutUvarint(tmp, uint64(len(e.Vars)))
		buf.Write(tmp[:n])
		for _, v := range e.Vars {
			n = binary.PutUvarint(tmp, uint64(len(v)))
			buf.Write(tmp[:n])
			buf.WriteString(v)
		}
	}
	return buf.Bytes()
}

// decodeChunkData is the inverse. Every length it reads is bounds-checked
// against the remaining input, so a truncated or corrupt payload is an error
// naming where it failed rather than a panic or a silently short result.
func decodeChunkData(data []byte) ([]chunkPayloadEntry, error) {
	count, n := binary.Uvarint(data)
	if n <= 0 {
		return nil, fmt.Errorf("stackdriver: chunk payload does not open with an entry count")
	}
	pos := n
	entries := make([]chunkPayloadEntry, 0, count)
	for i := range count {
		ts, tsN := binary.Varint(data[pos:])
		if tsN <= 0 {
			return nil, fmt.Errorf("stackdriver: chunk payload entry %d has no timestamp", i)
		}
		pos += tsN
		vc, vcN := binary.Uvarint(data[pos:])
		if vcN <= 0 {
			return nil, fmt.Errorf("stackdriver: chunk payload entry %d has no variable count", i)
		}
		pos += vcN
		vars := make([]string, 0, vc)
		for j := range vc {
			vl, vlN := binary.Uvarint(data[pos:])
			if vlN <= 0 {
				return nil, fmt.Errorf("stackdriver: chunk payload entry %d variable %d has no length", i, j)
			}
			pos += vlN
			if pos+int(vl) > len(data) {
				return nil, fmt.Errorf(
					"stackdriver: chunk payload entry %d variable %d claims %d bytes but only %d remain",
					i, j, vl, len(data)-pos)
			}
			vars = append(vars, string(data[pos:pos+int(vl)]))
			pos += int(vl)
		}
		entries = append(entries, chunkPayloadEntry{Timestamp: time.Unix(0, ts).UTC(), Vars: vars})
	}
	return entries, nil
}

// compressBytes zstd-compresses at the default level.
func compressBytes(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("stackdriver: creating the chunk compressor: %w", err)
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil), nil
}

// decompressBytes is the inverse, used by the module's own round-trip tests and
// by any reader recovering a chunk's entries.
func decompressBytes(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("stackdriver: creating the chunk decompressor: %w", err)
	}
	defer dec.Close()
	out, err := dec.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("stackdriver: decompressing a chunk payload: %w", err)
	}
	return out, nil
}
