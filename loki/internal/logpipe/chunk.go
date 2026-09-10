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

// chunk.go — chunk bucketing, the chunk id preimage, and the entry frame.
//
// THREE RULES HERE HAVE NO OTHER CARRIER, and two independent implementations
// agree on the emitted graph only if all three match:
//
//  1. the bucket key is (stream id, template id, window start) where the window
//     start is FLOORED to a UTC-EPOCH-ALIGNED five-minute bucket;
//  2. the id preimage is streamID '|' templateID '|' big-endian int64 window
//     nanos, sha256'd, first 16 bytes, rendered as "log-chunk:%x";
//  3. Content is ZSTD over a varint frame, not plaintext and not some other
//     framing.
//
// PARITY ON CONTENT IS THE DECODED FRAME PLUS THE FACT THAT IT IS ZSTD, never
// byte-identity of the compressed buffer: a zstd encoder's default-level output
// is not contracted stable across releases, and the two modules already in this
// workspace that need the library pin different versions of it.

// DefaultChunkWindow is the bucket width. A different width produces a
// different chunk set for identical input.
const DefaultChunkWindow = 5 * time.Minute

// chunkKey is one bucket's identity during assembly.
type chunkKey struct {
	StreamID    string
	TemplateID  string
	WindowStart time.Time
}

// chunkEntry is the per-entry payload a chunk holds before compression. Only
// the timestamp and the variable values are kept; the template recovers the
// skeleton at decode time.
type chunkEntry struct {
	Timestamp time.Time
	Vars      []string
}

// assembleChunks buckets entries by (stream, template, window) and compresses
// each bucket. An entry with no stream or no template id is skipped: it could
// not be clustered, and a chunk keyed on an empty id would be an edge into
// nothing.
//
// The result is sorted by (stream, template, start) so two runs over the same
// input emit the chunks in the same order.
func assembleChunks(
	entries []Entry,
	entryStreamIDs, entryTemplateIDs []string,
	templates []*Template,
	window time.Duration,
) ([]*Chunk, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entryStreamIDs) != len(entries) || len(entryTemplateIDs) != len(entries) {
		return nil, fmt.Errorf("logpipe: entry parallel-slice length mismatch: %d entries, %d stream ids, %d template ids",
			len(entries), len(entryStreamIDs), len(entryTemplateIDs))
	}
	if window <= 0 {
		window = DefaultChunkWindow
	}

	byID := templatesByID(templates)
	buckets := make(map[chunkKey][]chunkEntry)
	for i, e := range entries {
		sid, tid := entryStreamIDs[i], entryTemplateIDs[i]
		if sid == "" || tid == "" {
			continue
		}
		k := chunkKey{StreamID: sid, TemplateID: tid, WindowStart: windowStart(e.Timestamp, window)}
		buckets[k] = append(buckets[k], chunkEntry{
			Timestamp: e.Timestamp,
			Vars:      extractEntryVars(byID[tid], e.Message),
		})
	}
	return buildChunksFromBuckets(buckets)
}

func buildChunksFromBuckets(buckets map[chunkKey][]chunkEntry) ([]*Chunk, error) {
	chunks := make([]*Chunk, 0, len(buckets))
	for k, entries := range buckets {
		compressed, err := compressBytes(EncodeChunkData(entries))
		if err != nil {
			return nil, fmt.Errorf("logpipe: compressing chunk %s: %w", ChunkID(k.StreamID, k.TemplateID, k.WindowStart), err)
		}
		start, end := entryTimeRange(entries)
		chunks = append(chunks, &Chunk{
			ID:         ChunkID(k.StreamID, k.TemplateID, k.WindowStart),
			StreamID:   k.StreamID,
			TemplateID: k.TemplateID,
			StartTime:  start,
			EndTime:    end,
			Data:       compressed,
			EntryCount: len(entries),
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

// windowStart floors ts to its bucket, aligned to the UTC EPOCH rather than to
// the walk's own start: an epoch-relative floor makes the same entry land in
// the same bucket whatever window the operator asked for.
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

// ChunkID is the deterministic chunk id. Exported because a test asserts the
// value against the documented preimage rather than against this function's own
// output.
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

// extractEntryVars recovers the entry's values at the template's wildcard
// positions. A token-count mismatch — the sign that the template was
// consolidated out from under this entry — yields nil, and the chunk still
// records the timestamp.
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

// EncodeChunkData serializes entries to the frame stored inside the zstd body:
//
//	[count:uvarint]
//	per entry: [unix_nano:varint][var_count:uvarint][(len:uvarint)(bytes)]...
//
// It is exported and returns the UNCOMPRESSED bytes so a test can assert the
// frame literally. A round trip through this package's own encoder and decoder
// passes on any self-consistent framing, so the frame is what the parity
// assertion has to reach.
func EncodeChunkData(entries []chunkEntry) []byte {
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

// DecodeChunkData is EncodeChunkData's inverse. Every length it reads is
// bounds-checked against the remaining input: the frame arrives from a graph
// rather than from this process, so a truncated one is an error naming the
// entry and the field, never a panic.
func DecodeChunkData(data []byte) ([]chunkEntry, error) {
	count, n := binary.Uvarint(data)
	if n <= 0 {
		return nil, fmt.Errorf("logpipe: decoding a chunk frame: bad entry count")
	}
	pos := n
	entries := make([]chunkEntry, 0, count)
	for i := range count {
		ts, tsN := binary.Varint(data[pos:])
		if tsN <= 0 {
			return nil, fmt.Errorf("logpipe: decoding a chunk frame: bad timestamp at entry %d", i)
		}
		pos += tsN
		vc, vcN := binary.Uvarint(data[pos:])
		if vcN <= 0 {
			return nil, fmt.Errorf("logpipe: decoding a chunk frame: bad var count at entry %d", i)
		}
		pos += vcN
		vars := make([]string, 0, vc)
		for j := range vc {
			vl, vlN := binary.Uvarint(data[pos:])
			if vlN <= 0 {
				return nil, fmt.Errorf("logpipe: decoding a chunk frame: bad var length at entry %d var %d", i, j)
			}
			pos += vlN
			if pos+int(vl) > len(data) {
				return nil, fmt.Errorf("logpipe: decoding a chunk frame: var bytes overflow at entry %d var %d", i, j)
			}
			vars = append(vars, string(data[pos:pos+int(vl)]))
			pos += int(vl)
		}
		entries = append(entries, chunkEntry{Timestamp: time.Unix(0, ts).UTC(), Vars: vars})
	}
	return entries, nil
}

// compressBytes zstd-compresses at the library's default level. The writer is
// closed on every path: it holds encoder goroutines, and this collector's
// package gate reports a leaked one.
func compressBytes(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil), nil
}

// DecompressBytes is compressBytes' inverse, exported so a test can prove a
// chunk's Content is a zstd frame rather than plaintext.
func DecompressBytes(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(data, nil)
}

// DecodeChunk recovers a chunk's timestamps and variable rows from its stored
// Content. It is the reader half of the frame, kept beside the writer so the
// two cannot drift.
func DecodeChunk(c *Chunk) ([]time.Time, [][]string, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("logpipe: DecodeChunk: nil chunk")
	}
	raw, err := DecompressBytes(c.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("logpipe: decompressing chunk %s: %w", c.ID, err)
	}
	entries, err := DecodeChunkData(raw)
	if err != nil {
		return nil, nil, err
	}
	timestamps := make([]time.Time, len(entries))
	vars := make([][]string, len(entries))
	for i, e := range entries {
		timestamps[i] = e.Timestamp
		vars[i] = e.Vars
	}
	return timestamps, vars, nil
}
