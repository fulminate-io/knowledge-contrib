// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// chunk_test.go — the window alignment a re-collect rides, the chunk id, the
// entry-bounded times, and the entry block.

// TestWindowIsAlignedToTheUTCEpoch is the property that makes a chunk id stable
// across two collects whose requested ranges differ. An implementation that
// floors relative to the requested range or the first entry passes every other
// cell here.
func TestWindowIsAlignedToTheUTCEpoch(t *testing.T) {
	// 12:07:02 floors to 12:05:00, which is a multiple of five minutes from
	// the epoch and unrelated to any entry or request.
	ts := time.Date(2026, 3, 1, 12, 7, 2, 0, time.UTC)
	want := time.Date(2026, 3, 1, 12, 5, 0, 0, time.UTC)
	if got := windowStart(ts); !got.Equal(want) {
		t.Errorf("windowStart(%s) = %s, want %s", ts, got, want)
	}
	// An instant exactly on a boundary floors to ITSELF, so it belongs to the
	// later bucket rather than the earlier one.
	boundary := time.Date(2026, 3, 1, 12, 10, 0, 0, time.UTC)
	if got := windowStart(boundary); !got.Equal(boundary) {
		t.Errorf("windowStart(%s) = %s, want the boundary itself", boundary, got)
	}
	// One nanosecond earlier belongs to the earlier bucket.
	just := boundary.Add(-time.Nanosecond)
	if got := windowStart(just); !got.Equal(time.Date(2026, 3, 1, 12, 5, 0, 0, time.UTC)) {
		t.Errorf("windowStart(%s) = %s, want the 12:05 bucket", just, got)
	}
}

// TestEntriesOneSecondApartAcrossABoundaryLandInDifferentChunks is the
// observable consequence of the alignment.
func TestEntriesOneSecondApartAcrossABoundaryLandInDifferentChunks(t *testing.T) {
	before := time.Date(2026, 3, 1, 12, 9, 59, 0, time.UTC)
	after := time.Date(2026, 3, 1, 12, 10, 0, 0, time.UTC)
	entries := []LogEntry{{Timestamp: before, Message: "m"}, {Timestamp: after, Message: "m"}}
	chunks, err := assembleChunks(entries, []string{"s", "s"}, []string{"t", "t"})
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("two entries straddling a window boundary produced %d chunks, want 2", len(chunks))
	}
	if chunks[0].ID == chunks[1].ID {
		t.Error("the two chunks share an id")
	}
}

// TestTwoCollectsWithDifferentRequestedRangesProduceTheSameChunkIDs is the
// carry-forward cell. The requested range is a tool parameter and changes
// between collects by design, so a chunk id that moved with it would make every
// re-collect look entirely rewritten.
func TestTwoCollectsWithDifferentRequestedRangesProduceTheSameChunkIDs(t *testing.T) {
	entries := []LogEntry{
		{Timestamp: time.Date(2026, 3, 1, 12, 6, 14, 0, time.UTC), Message: "served request"},
		{Timestamp: time.Date(2026, 3, 1, 12, 7, 2, 0, time.UTC), Message: "served request"},
	}
	first, err := assembleChunks(entries, []string{"s", "s"}, []string{"t", "t"})
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	// A second collect over the same entries, reached through a wider request.
	// Nothing about the request enters chunk assembly, which is the point.
	second, err := assembleChunks(entries, []string{"s", "s"}, []string{"t", "t"})
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("chunk counts %d and %d, want 1 each", len(first), len(second))
	}
	if first[0].ID != second[0].ID {
		t.Errorf("chunk id %q then %q; the id must not depend on the requested range", first[0].ID, second[0].ID)
	}
}

// TestChunkIDShape pins the prefix and the hex length. A hash truncated to 16
// hex characters instead of 16 bytes has the right shape at a glance and
// matches nothing.
func TestChunkIDShape(t *testing.T) {
	id := chunkID("stream", "template", time.Unix(0, 0).UTC())
	if !strings.HasPrefix(id, "log-chunk:") {
		t.Errorf("chunk id %q does not carry the log-chunk prefix", id)
	}
	if hex := strings.TrimPrefix(id, "log-chunk:"); len(hex) != 32 {
		t.Errorf("chunk id hex is %d characters, want 32 (16 bytes of sha256)", len(hex))
	}
}

// TestChunkIDFoldsTheWindowAsBigEndianBytes discriminates the byte encoding
// from the decimal one, by computing BOTH in the test and asserting which one
// the production id equals. Both encodings produce a well-formed id, so a shape
// assertion cannot tell them apart; only this can.
func TestChunkIDFoldsTheWindowAsBigEndianBytes(t *testing.T) {
	start := time.Date(2026, 3, 1, 12, 5, 0, 0, time.UTC)

	var bigEndian bytes.Buffer
	bigEndian.WriteString("s")
	bigEndian.WriteByte('|')
	bigEndian.WriteString("t")
	bigEndian.WriteByte('|')
	if err := binary.Write(&bigEndian, binary.BigEndian, start.UnixNano()); err != nil {
		t.Fatalf("encoding the control: %v", err)
	}
	wantHash := sha256.Sum256(bigEndian.Bytes())
	want := fmt.Sprintf("log-chunk:%x", wantHash[:16])

	decimalHash := sha256.Sum256([]byte("s|t|" + strconv.FormatInt(start.UnixNano(), 10)))
	decimal := fmt.Sprintf("log-chunk:%x", decimalHash[:16])

	got := chunkID("s", "t", start)
	if got != want {
		t.Errorf("chunkID = %s, want the big-endian encoding %s", got, want)
	}
	if got == decimal {
		t.Error("chunkID equals the DECIMAL-text encoding; the window start folds in as eight raw bytes")
	}
}

// TestChunkTimesAreTheEntriesNotTheWindow is wrong in every chunk when it is
// wrong, and raises no error when it is.
func TestChunkTimesAreTheEntriesNotTheWindow(t *testing.T) {
	start := time.Date(2026, 3, 1, 12, 6, 14, 0, time.UTC)
	end := time.Date(2026, 3, 1, 12, 7, 2, 0, time.UTC)
	entries := []LogEntry{{Timestamp: end, Message: "m"}, {Timestamp: start, Message: "m"}}
	chunks, err := assembleChunks(entries, []string{"s", "s"}, []string{"t", "t"})
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if !chunks[0].StartTime.Equal(start) {
		t.Errorf("chunk start %s, want the earliest ENTRY %s, not the window's edge", chunks[0].StartTime, start)
	}
	if !chunks[0].EndTime.Equal(end) {
		t.Errorf("chunk end %s, want the latest ENTRY %s, not the window's edge", chunks[0].EndTime, end)
	}
	if chunks[0].EntryCount != 2 {
		t.Errorf("entry count %d, want 2", chunks[0].EntryCount)
	}
}

// TestChunkContentIsTheEntryBlock is the one value the parity golden cannot
// carry, because this collector deliberately emits entry text where the
// built-in emits a compressed block. It is asserted directly instead, against a
// hand-written expectation.
func TestChunkContentIsTheEntryBlock(t *testing.T) {
	later := time.Date(2026, 3, 1, 12, 7, 2, 0, time.UTC)
	earlier := time.Date(2026, 3, 1, 12, 6, 14, 0, time.UTC)
	// Supplied out of order, so the ascending-order rule is observable.
	entries := []LogEntry{{Timestamp: later, Message: "second"}, {Timestamp: earlier, Message: "first"}}

	const want = "2026-03-01T12:06:14.000000000Z\tfirst\n" +
		"2026-03-01T12:07:02.000000000Z\tsecond\n"
	if got := encodeChunkContent(entries); got != want {
		t.Errorf("chunk content =\n%q\nwant\n%q", got, want)
	}
}

// TestChunkContentCarriesThePerEntryTimestamp is the reason the block carries a
// stamp per line at all: the chunk's own start and end bound the WHOLE chunk,
// so without the per-line stamp the per-entry instants are lost.
func TestChunkContentCarriesThePerEntryTimestamp(t *testing.T) {
	entries := []LogEntry{
		{Timestamp: time.Date(2026, 3, 1, 12, 6, 14, 0, time.UTC), Message: "a"},
		{Timestamp: time.Date(2026, 3, 1, 12, 6, 55, 0, time.UTC), Message: "b"},
	}
	content := encodeChunkContent(entries)
	for _, want := range []string{"2026-03-01T12:06:14.000000000Z", "2026-03-01T12:06:55.000000000Z"} {
		if !strings.Contains(content, want) {
			t.Errorf("the entry block does not carry the instant %s:\n%s", want, content)
		}
	}
}

// TestChunkAssemblySkipsEntriesWithNoTemplate covers the entry that clustered
// nowhere: it contributes no chunk rather than a chunk pointing at a template
// with no node.
func TestChunkAssemblySkipsEntriesWithNoTemplate(t *testing.T) {
	entries := []LogEntry{{Timestamp: time.Unix(1, 0).UTC(), Message: ""}}
	chunks, err := assembleChunks(entries, []string{"s"}, []string{""})
	if err != nil {
		t.Fatalf("assembleChunks: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("got %d chunks for an entry with no template, want none", len(chunks))
	}
}

// TestChunkAssemblyRefusesMismatchedParallelSlices is the internal-consistency
// arm: a length mismatch is a programming error and is refused rather than
// silently truncating the walk.
func TestChunkAssemblyRefusesMismatchedParallelSlices(t *testing.T) {
	entries := []LogEntry{{Timestamp: time.Unix(1, 0).UTC()}, {Timestamp: time.Unix(2, 0).UTC()}}
	if _, err := assembleChunks(entries, []string{"s"}, []string{"t", "t"}); err == nil {
		t.Fatal("a stream-id slice shorter than the entry slice was accepted")
	}
}
