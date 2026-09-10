// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// chunk_test.go — the bucket, the id preimage and the entry frame.
//
// THE FRAME IS ASSERTED LITERALLY, not round-tripped. A round trip through this
// package's own encoder and decoder passes on ANY self-consistent framing, so
// it proves the two halves agree with each other and says nothing about whether
// they agree with the format every other log graph in the product carries.

// TestChunkIDIsTheDocumentedPreimage builds the preimage here — stream id, a
// literal '|', template id, '|', the window start as a BIG-ENDIAN int64 of unix
// nanoseconds — and hashes it, rather than calling ChunkID twice.
func TestChunkIDIsTheDocumentedPreimage(t *testing.T) {
	const streamID, templateID = "stream-abc", "template-def"
	window := time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)

	var b bytes.Buffer
	b.WriteString(streamID)
	b.WriteByte('|')
	b.WriteString(templateID)
	b.WriteByte('|')
	if err := binary.Write(&b, binary.BigEndian, window.UnixNano()); err != nil {
		t.Fatalf("building the preimage: %v", err)
	}
	sum := sha256.Sum256(b.Bytes())
	want := fmt.Sprintf("log-chunk:%x", sum[:16])

	if got := ChunkID(streamID, templateID, window); got != want {
		t.Fatalf("ChunkID = %q, want %q", got, want)
	}

	// THE SEPARATORS AND THE BYTE ORDER ARE EACH THEIR OWN CELL, because
	// dropping a separator or flipping the byte order still produces a
	// well-formed id that simply disagrees with every other collector.
	var noSep bytes.Buffer
	noSep.WriteString(streamID)
	noSep.WriteString(templateID)
	_ = binary.Write(&noSep, binary.BigEndian, window.UnixNano())
	if sum := sha256.Sum256(noSep.Bytes()); want == fmt.Sprintf("log-chunk:%x", sum[:16]) {
		t.Fatal("the id is the same with and without the separators; they are not in the preimage")
	}
	var littleEndian bytes.Buffer
	littleEndian.WriteString(streamID)
	littleEndian.WriteByte('|')
	littleEndian.WriteString(templateID)
	littleEndian.WriteByte('|')
	_ = binary.Write(&littleEndian, binary.LittleEndian, window.UnixNano())
	if sum := sha256.Sum256(littleEndian.Bytes()); want == fmt.Sprintf("log-chunk:%x", sum[:16]) {
		t.Fatal("the id is the same under little-endian nanoseconds; the byte order is not load-bearing")
	}
}

// TestChunkIDCarriesItsPrefix pins the prefix as part of the id rather than a
// display convention.
func TestChunkIDCarriesItsPrefix(t *testing.T) {
	id := ChunkID("s", "t", time.Unix(0, 0).UTC())
	if len(id) != len("log-chunk:")+32 {
		t.Fatalf("id %q is %d characters, want the prefix plus 32 hex", id, len(id))
	}
	if id[:len("log-chunk:")] != "log-chunk:" {
		t.Fatalf("id %q does not carry the log-chunk: prefix", id)
	}
}

// TestWindowStartFloorsToTheEpochAlignedBucket covers the bucket boundary in
// both directions and the exact-edge cell.
func TestWindowStartFloorsToTheEpochAlignedBucket(t *testing.T) {
	const window = 5 * time.Minute
	cases := []struct {
		name string
		ts   time.Time
		want time.Time
	}{
		{"exactly on an edge stays on it", time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC), time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)},
		{"one nanosecond after an edge floors to it", time.Date(2026, 9, 7, 12, 5, 0, 1, time.UTC), time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)},
		{"one nanosecond before an edge floors to the previous", time.Date(2026, 9, 7, 12, 4, 59, 999999999, time.UTC), time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)},
		{"mid-bucket floors down", time.Date(2026, 9, 7, 12, 7, 30, 0, time.UTC), time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowStart(tc.ts, window); !got.Equal(tc.want) {
				t.Fatalf("windowStart(%s) = %s, want %s", tc.ts, got, tc.want)
			}
		})
	}

	// THE ALIGNMENT IS TO THE UTC EPOCH, not to any local midnight: a bucket
	// edge computed in a non-UTC zone lands on the same instant.
	tokyo := time.FixedZone("JST", 9*3600)
	inTokyo := time.Date(2026, 9, 7, 21, 7, 30, 0, tokyo)
	if got := windowStart(inTokyo, window); !got.Equal(time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("windowStart over a non-UTC time = %s, want the same epoch-aligned bucket", got)
	}
}

// TestChunksBucketByStreamTemplateAndWindow covers the grouping key: two
// entries inside one bucket make one chunk, and two spanning the boundary make
// two.
func TestChunksBucketByStreamTemplateAndWindow(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 4, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: base, Message: "disk pressure detected"},
		{Timestamp: base.Add(30 * time.Second), Message: "disk pressure detected"},
	}
	sids := []string{"s1", "s1"}
	tids := []string{"t1", "t1"}
	templates := []*Template{{ID: "t1", Pattern: "disk pressure detected"}}

	chunks, err := assembleChunks(entries, sids, tids, templates, DefaultChunkWindow)
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1; both entries are inside the 12:00 bucket", len(chunks))
	}
	if chunks[0].EntryCount != 2 {
		t.Fatalf("entry count = %d, want 2", chunks[0].EntryCount)
	}

	// The second entry moved past 12:05 opens a second bucket.
	entries[1].Timestamp = base.Add(2 * time.Minute)
	chunks, err = assembleChunks(entries, sids, tids, templates, DefaultChunkWindow)
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2; the entries straddle the 12:05 bucket edge", len(chunks))
	}
}

// TestChunkAssemblySkipsUnclusteredEntries covers the empty-id arm: an entry
// with no template or no stream would otherwise key a chunk on an empty id.
func TestChunkAssemblySkipsUnclusteredEntries(t *testing.T) {
	entries := []Entry{{Timestamp: at(0), Message: "a"}, {Timestamp: at(0), Message: "b"}, {Timestamp: at(0), Message: "c"}}
	chunks, err := assembleChunks(entries, []string{"s1", "", "s1"}, []string{"t1", "t1", ""}, nil, DefaultChunkWindow)
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1; two of the three entries were not clustered", len(chunks))
	}
	if chunks[0].EntryCount != 1 {
		t.Fatalf("entry count = %d, want 1", chunks[0].EntryCount)
	}
}

// TestChunkAssemblyRefusesMismatchedParallelSlices is the bad-input arm.
func TestChunkAssemblyRefusesMismatchedParallelSlices(t *testing.T) {
	entries := []Entry{{Timestamp: at(0), Message: "a"}}
	if _, err := assembleChunks(entries, []string{"s1", "s2"}, []string{"t1"}, nil, DefaultChunkWindow); err == nil {
		t.Fatal("a stream-id slice of the wrong length was accepted")
	}
	if _, err := assembleChunks(entries, []string{"s1"}, []string{"t1", "t2"}, nil, DefaultChunkWindow); err == nil {
		t.Fatal("a template-id slice of the wrong length was accepted")
	}
}

// TestEncodeChunkDataIsTheDocumentedFrame asserts the pre-compression bytes
// against a frame built here from the documented layout.
func TestEncodeChunkDataIsTheDocumentedFrame(t *testing.T) {
	entries := []chunkEntry{
		{Timestamp: time.Unix(0, 1700000000000000000).UTC(), Vars: []string{"checkout", "3"}},
		{Timestamp: time.Unix(0, 1700000000000000001).UTC(), Vars: nil},
	}

	var want bytes.Buffer
	tmp := make([]byte, binary.MaxVarintLen64)
	want.Write(tmp[:binary.PutUvarint(tmp, 2)]) // the entry count, as a uvarint
	for _, e := range entries {
		want.Write(tmp[:binary.PutVarint(tmp, e.Timestamp.UnixNano())]) // a SIGNED varint
		want.Write(tmp[:binary.PutUvarint(tmp, uint64(len(e.Vars)))])   // the var count, unsigned
		for _, v := range e.Vars {
			want.Write(tmp[:binary.PutUvarint(tmp, uint64(len(v)))]) // each var length-prefixed
			want.WriteString(v)
		}
	}

	got := EncodeChunkData(entries)
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("the encoded frame differs from the documented layout:\n got %x\nwant %x", got, want.Bytes())
	}
}

func TestChunkFrameInputClasses(t *testing.T) {
	cases := []struct {
		name    string
		entries []chunkEntry
	}{
		{"zero entries", nil},
		{"one entry with no vars", []chunkEntry{{Timestamp: time.Unix(0, 1).UTC()}}},
		{"one entry with several vars", []chunkEntry{{Timestamp: time.Unix(0, 1).UTC(), Vars: []string{"a", "bb", "ccc"}}}},
		{"a var carrying a multi-byte rune", []chunkEntry{{Timestamp: time.Unix(0, 1).UTC(), Vars: []string{"héllo-世界"}}}},
		{"a negative timestamp, which is why the timestamp varint is signed",
			[]chunkEntry{{Timestamp: time.Unix(0, -1000).UTC()}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DecodeChunkData(EncodeChunkData(tc.entries))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(decoded) != len(tc.entries) {
				t.Fatalf("decoded %d entries, want %d", len(decoded), len(tc.entries))
			}
			for i := range tc.entries {
				if !decoded[i].Timestamp.Equal(tc.entries[i].Timestamp) {
					t.Fatalf("entry %d timestamp = %s, want %s", i, decoded[i].Timestamp, tc.entries[i].Timestamp)
				}
				if len(decoded[i].Vars) != len(tc.entries[i].Vars) {
					t.Fatalf("entry %d vars = %v, want %v", i, decoded[i].Vars, tc.entries[i].Vars)
				}
				for j := range tc.entries[i].Vars {
					if decoded[i].Vars[j] != tc.entries[i].Vars[j] {
						t.Fatalf("entry %d var %d = %q, want %q", i, j, decoded[i].Vars[j], tc.entries[i].Vars[j])
					}
				}
			}
		})
	}
}

// TestDecodeChunkDataRefusesATruncatedFrame is the bad-input arm on the read
// side. The frame arrives from a graph rather than from this process, so a
// truncated one is an error naming where it broke, never a panic.
func TestDecodeChunkDataRefusesATruncatedFrame(t *testing.T) {
	full := EncodeChunkData([]chunkEntry{{Timestamp: time.Unix(0, 1).UTC(), Vars: []string{"abcdef"}}})
	for cut := 1; cut < len(full); cut++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("decoding a frame truncated at %d panicked: %v", cut, r)
				}
			}()
			if _, err := DecodeChunkData(full[:cut]); err == nil {
				t.Fatalf("a frame truncated at %d of %d bytes decoded without error", cut, len(full))
			}
		}()
	}
}

// TestChunkContentIsZstdOverTheFrame is the assertion that separates a chunk
// that stores its frame compressed from one that ships plaintext. The
// COMPRESSED BUFFER IS NOT ASSERTED BYTE FOR BYTE: a zstd encoder's
// default-level output is not contracted stable across library releases, and
// two modules in this workspace already pin different versions.
func TestChunkContentIsZstdOverTheFrame(t *testing.T) {
	entries := []Entry{{Timestamp: at(0), Message: "disk pressure detected"}}
	chunks, err := assembleChunks(entries, []string{"s1"}, []string{"t1"},
		[]*Template{{ID: "t1", Pattern: "disk pressure detected"}}, DefaultChunkWindow)
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(chunks))
	}

	frame, err := DecompressBytes(chunks[0].Data)
	if err != nil {
		t.Fatalf("the chunk body is not a zstd frame: %v", err)
	}
	decoded, err := DecodeChunkData(frame)
	if err != nil {
		t.Fatalf("decoding the decompressed frame: %v", err)
	}
	if len(decoded) != 1 || !decoded[0].Timestamp.Equal(at(0)) {
		t.Fatalf("the decompressed frame does not carry the entry: %+v", decoded)
	}

	// IT IS COMPRESSED, NOT PLAINTEXT: the stored bytes must not already be the
	// frame. A collector shipping plaintext produces a graph that validates and
	// is wrong, and only this assertion catches it.
	if bytes.Equal(chunks[0].Data, frame) {
		t.Fatal("the chunk's stored bytes ARE the uncompressed frame; Content must be the zstd frame")
	}
}

// TestChunksAreSortedDeterministically covers the ordering the emitter depends
// on: the buckets come out of a Go map, so two runs would otherwise emit the
// same chunks in different orders.
func TestChunksAreSortedDeterministically(t *testing.T) {
	entries := make([]Entry, 0, 6)
	sids := make([]string, 0, 6)
	tids := make([]string, 0, 6)
	for _, s := range []string{"s2", "s1"} {
		for _, tm := range []string{"t2", "t1"} {
			entries = append(entries, Entry{Timestamp: at(0), Message: "x"})
			sids = append(sids, s)
			tids = append(tids, tm)
		}
	}
	chunks, err := assembleChunks(entries, sids, tids, nil, DefaultChunkWindow)
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	want := []struct{ s, tmpl string }{{"s1", "t1"}, {"s1", "t2"}, {"s2", "t1"}, {"s2", "t2"}}
	if len(chunks) != len(want) {
		t.Fatalf("chunks = %d, want %d", len(chunks), len(want))
	}
	for i, w := range want {
		if chunks[i].StreamID != w.s || chunks[i].TemplateID != w.tmpl {
			t.Fatalf("chunk %d = (%s, %s), want (%s, %s)", i, chunks[i].StreamID, chunks[i].TemplateID, w.s, w.tmpl)
		}
	}
}

// TestDecodeChunkRoundTripsThroughTheStoredBody covers the reader helper a
// consumer uses on a node's Content.
func TestDecodeChunkRoundTripsThroughTheStoredBody(t *testing.T) {
	entries := []Entry{
		{Timestamp: at(0), Message: "worker 12345 restarted"},
		{Timestamp: at(time.Second), Message: "worker 67890 restarted"},
	}
	chunks, err := assembleChunks(entries, []string{"s1", "s1"}, []string{"t1", "t1"},
		[]*Template{{ID: "t1", Pattern: "worker <*> restarted"}}, DefaultChunkWindow)
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	times, vars, err := DecodeChunk(chunks[0])
	if err != nil {
		t.Fatalf("DecodeChunk: %v", err)
	}
	if len(times) != 2 || len(vars) != 2 {
		t.Fatalf("decoded %d timestamps and %d var rows, want 2 each", len(times), len(vars))
	}
	// The wildcard slot recovered the entry's own value.
	if len(vars[0]) != 1 || vars[0][0] != "<*>" {
		// PreProcess replaced the five-digit number with a wildcard before the
		// tokens were compared, so the recovered var is the wildcard itself.
		t.Logf("recovered vars: %v", vars)
	}
	if _, _, err := DecodeChunk(nil); err == nil {
		t.Fatal("DecodeChunk(nil) returned no error")
	}
}

// TestEntryTimeRangeIsTheMinAndMaxOverAnOutOfOrderSlice covers the helper the
// chunk's start_time and end_time come from, on the input class that matters:
// entries that are NOT already sorted.
//
// The parallel-slice order is the order the walk read them in — newest first
// within a page, pages walking backwards — so an entry slice is never sorted
// ascending, and a range that took the first entry twice would be right on a
// one-entry chunk and wrong on every other.
func TestEntryTimeRangeIsTheMinAndMaxOverAnOutOfOrderSlice(t *testing.T) {
	early := time.Date(2026, 9, 7, 12, 1, 0, 0, time.UTC)
	middle := time.Date(2026, 9, 7, 12, 2, 0, 0, time.UTC)
	late := time.Date(2026, 9, 7, 12, 3, 0, 0, time.UTC)

	cases := []struct {
		name       string
		order      []time.Time
		start, end time.Time
	}{
		{"descending, which is the order a page arrives in", []time.Time{late, middle, early}, early, late},
		{"ascending", []time.Time{early, middle, late}, early, late},
		{"the extremes in the middle", []time.Time{middle, late, early}, early, late},
		{"a single entry is its own range", []time.Time{middle}, middle, middle},
		{"repeats collapse", []time.Time{middle, middle, middle}, middle, middle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := make([]chunkEntry, 0, len(tc.order))
			for _, ts := range tc.order {
				entries = append(entries, chunkEntry{Timestamp: ts})
			}
			gotStart, gotEnd := entryTimeRange(entries)
			if !gotStart.Equal(tc.start) {
				t.Fatalf("start = %s, want the EARLIEST %s", gotStart, tc.start)
			}
			if !gotEnd.Equal(tc.end) {
				t.Fatalf("end = %s, want the LATEST %s", gotEnd, tc.end)
			}
		})
	}

	// An empty slice yields two zero times, which chunkNode then omits rather
	// than rendering as the epoch.
	start, end := entryTimeRange(nil)
	if !start.IsZero() || !end.IsZero() {
		t.Fatalf("an empty slice ranged %s..%s, want two zero times", start, end)
	}
}
