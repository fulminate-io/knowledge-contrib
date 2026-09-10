// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/klauspost/compress/zstd"
)

// chunk.go — chunks: the (stream, template, time-window) bucket that actually
// carries the log lines, compressed.
//
// WHAT A CHUNK STORES IS NOT THE LINES. A chunk holds each entry's timestamp
// and the tokens sitting at its template's WILDCARD positions; the template
// recovers the skeleton at read time. That is what makes a log graph smaller
// than its log.
//
// THE WINDOW IS FLOORED TO THE UTC EPOCH, NOT TO THE FIRST ENTRY. Two collects
// covering overlapping time ranges therefore agree about where the bucket
// boundaries are, and a re-collect of unchanged input reproduces the same chunk
// ids. Bucketing relative to the first entry would move every boundary when the
// window's start moved, and carry-forward would see a whole new set of chunks.

// DefaultChunkWindow is the bucket width when a caller names none. Five minutes
// keeps a busy container's chunks readable while leaving a quiet one with a
// single chunk per template.
const DefaultChunkWindow = 5 * time.Minute

// chunkKey identifies one bucket during assembly.
type chunkKey struct {
	StreamID    string
	TemplateID  string
	WindowStart time.Time
}

// chunkEntry is one entry's stored payload.
type chunkEntry struct {
	Timestamp time.Time
	Vars      []string
}

// AssembleChunks groups entries into buckets and compresses each into a chunk.
//
// entryStreamIDs and entryTemplateIDs are parallel to entries. An entry with an
// empty id on either side is SKIPPED rather than bucketed under a placeholder:
// an empty message produced no template, and attaching it to an arbitrary one
// would put a line under a pattern it does not match.
//
// The result is sorted by (stream, template, start) so two runs over the same
// input emit the same order.
func AssembleChunks(
	entries []Entry,
	entryStreamIDs []string,
	entryTemplateIDs []string,
	templates []*Template,
	window time.Duration,
) ([]*Chunk, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entryStreamIDs) != len(entries) || len(entryTemplateIDs) != len(entries) {
		return nil, fmt.Errorf(
			"logpipe: parallel-slice length mismatch: %d entries, %d stream ids, %d template ids",
			len(entries), len(entryStreamIDs), len(entryTemplateIDs))
	}
	if window <= 0 {
		window = DefaultChunkWindow
	}

	tmplByID := TemplatesByID(templates)
	buckets := make(map[chunkKey][]chunkEntry)
	for i, e := range entries {
		sid, tid := entryStreamIDs[i], entryTemplateIDs[i]
		if sid == "" || tid == "" {
			continue
		}
		key := chunkKey{StreamID: sid, TemplateID: tid, WindowStart: WindowStart(e.Timestamp, window)}
		buckets[key] = append(buckets[key], chunkEntry{
			Timestamp: e.Timestamp,
			Vars:      extractEntryVars(tmplByID[tid], e.Message),
		})
	}
	return buildChunks(buckets)
}

// buildChunks compresses each bucket.
func buildChunks(buckets map[chunkKey][]chunkEntry) ([]*Chunk, error) {
	chunks := make([]*Chunk, 0, len(buckets))
	for k, entries := range buckets {
		compressed, err := compressBytes(encodeChunkData(entries))
		if err != nil {
			return nil, fmt.Errorf("logpipe: compressing chunk for stream %s template %s: %w", k.StreamID, k.TemplateID, err)
		}
		start, end := entryTimeRange(entries)
		chunks = append(chunks, &Chunk{
			ID:             ChunkID(k.StreamID, k.TemplateID, k.WindowStart),
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

// WindowStart returns the start of the bucket holding ts, floored to a multiple
// of window measured from the UTC epoch. A window at or below zero returns ts
// itself, which degenerates to one chunk per entry rather than silently
// bucketing by something else.
func WindowStart(ts time.Time, window time.Duration) time.Time {
	if window <= 0 {
		return ts
	}
	w := window.Nanoseconds()
	if w == 0 {
		return ts
	}
	bucket := (ts.UnixNano() / w) * w
	return time.Unix(0, bucket).UTC()
}

// ChunkID is a chunk's node id: the "log-chunk:" PREFIX followed by the first
// sixteen bytes of a sha256 over the two ids and the window start.
//
// THREE WAYS TO GET THIS WRONG while every type assertion still passes: drop
// the prefix, emit the full 64-character hash, or join the fields without the
// separator byte, which lets two different (stream, template) pairs hash to one
// id. The separator and the BIG-ENDIAN timestamp encoding are both load-bearing.
func ChunkID(streamID, templateID string, windowStart time.Time) string {
	var b bytes.Buffer
	b.WriteString(streamID)
	b.WriteByte('|')
	b.WriteString(templateID)
	b.WriteByte('|')
	_ = binary.Write(&b, binary.BigEndian, windowStart.UnixNano())
	h := sha256.Sum256(b.Bytes())
	return fmt.Sprintf("log-chunk:%x", h[:16])
}

// entryTimeRange returns the earliest and latest timestamp in a bucket. These
// are the ENTRIES' bounds, not the window's: a chunk covering one line at
// 12:03 in the 12:00 window reports 12:03 twice.
func entryTimeRange(entries []chunkEntry) (time.Time, time.Time) {
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

// extractEntryVars returns the message tokens at the template's wildcard
// positions. A nil template or a token-count mismatch yields nil: a mismatch
// means the template was consolidated into one covering a different shape, and
// the chunk still records the timestamp so counts and time ranges stay right.
func extractEntryVars(tpl *Template, message string) []string {
	if tpl == nil {
		return nil
	}
	tplTokens := Tokenize(tpl.Pattern)
	msgTokens := Tokenize(PreProcess(message))
	if len(tplTokens) != len(msgTokens) {
		return nil
	}
	return extractVars(tplTokens, msgTokens)
}

// encodeChunkData serializes a bucket to a compact binary form:
//
//	[count:uvarint] then per entry [ts_unix_nano:varint][vars:uvarint]([len:uvarint][bytes])*
//
// Compression is a separate step so the encoding can be tested on its own.
func encodeChunkData(entries []chunkEntry) []byte {
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

// decodeChunkData is the inverse of encodeChunkData. Every length it reads is
// checked against the remaining input before it is used, so a truncated or
// corrupt payload is an error naming the entry rather than a panic.
func decodeChunkData(data []byte) ([]chunkEntry, error) {
	count, n := binary.Uvarint(data)
	if n <= 0 {
		return nil, fmt.Errorf("logpipe: chunk payload has no readable entry count")
	}
	pos := n
	entries := make([]chunkEntry, 0, count)
	for i := range count {
		ts, tsN := binary.Varint(data[pos:])
		if tsN <= 0 {
			return nil, fmt.Errorf("logpipe: chunk payload has no readable timestamp at entry %d", i)
		}
		pos += tsN
		vc, vcN := binary.Uvarint(data[pos:])
		if vcN <= 0 {
			return nil, fmt.Errorf("logpipe: chunk payload has no readable variable count at entry %d", i)
		}
		pos += vcN
		vars := make([]string, 0, vc)
		for j := range vc {
			vl, vlN := binary.Uvarint(data[pos:])
			if vlN <= 0 {
				return nil, fmt.Errorf("logpipe: chunk payload has no readable variable length at entry %d variable %d", i, j)
			}
			pos += vlN
			if pos+int(vl) > len(data) {
				return nil, fmt.Errorf("logpipe: chunk payload variable %d of entry %d runs past the end of the payload", j, i)
			}
			vars = append(vars, string(data[pos:pos+int(vl)]))
			pos += int(vl)
		}
		entries = append(entries, chunkEntry{Timestamp: time.Unix(0, ts).UTC(), Vars: vars})
	}
	return entries, nil
}

// compressBytes zstd-compresses a payload.
func compressBytes(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("logpipe: building a zstd encoder: %w", err)
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil), nil
}

// decompressBytes is the inverse of compressBytes.
func decompressBytes(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("logpipe: building a zstd decoder: %w", err)
	}
	defer dec.Close()
	return dec.DecodeAll(data, nil)
}

// DecodeChunk recovers a chunk's timestamps and per-entry variable values. It
// exists so a test — and any reader holding the emitted bytes — can assert what
// a chunk actually carries rather than trusting its entry count.
func DecodeChunk(chunk *Chunk) ([]time.Time, [][]string, error) {
	if chunk == nil {
		return nil, nil, fmt.Errorf("logpipe: DecodeChunk was given no chunk")
	}
	raw, err := decompressBytes(chunk.CompressedData)
	if err != nil {
		return nil, nil, fmt.Errorf("logpipe: decompressing chunk %s: %w", chunk.ID, err)
	}
	entries, err := decodeChunkData(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("logpipe: decoding chunk %s: %w", chunk.ID, err)
	}
	timestamps := make([]time.Time, len(entries))
	vars := make([][]string, len(entries))
	for i, e := range entries {
		timestamps[i] = e.Timestamp
		vars[i] = e.Vars
	}
	return timestamps, vars, nil
}

// TemplatesByID indexes templates by id, skipping nils.
func TemplatesByID(templates []*Template) map[string]*Template {
	m := make(map[string]*Template, len(templates))
	for _, t := range templates {
		if t == nil {
			continue
		}
		m[t.ID] = t
	}
	return m
}
