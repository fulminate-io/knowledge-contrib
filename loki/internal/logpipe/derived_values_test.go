// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// literal_test coverage — EVERY EMITTED ID AND EVERY EMITTED TIME, against an
// expectation the producer cannot supply.
//
// WHY THE PARITY ROW ABOVE IS NOT ENOUGH. It looks a chunk node up by the id the
// producer put on the Chunk struct and asserts the times against that same
// struct's fields, so the derivation is compared against itself. Five mutations
// survive it: the chunk id built from the bucket's earliest entry instead of the
// epoch-floored window, start and end swapped, entryTimeRange's min/max
// neutered, FirstSeen tracking the latest, LastSeen frozen at the first. Each is
// a graph that stores, reads back and disagrees with every other producer of
// this vocabulary.
//
// The fixture below is small enough to know by hand: one message shape, one
// label set, three entries whose timestamps are fed OUT OF ORDER and straddle a
// five-minute bucket edge. Every expectation is a literal written here or a hash
// computed here from the documented preimage.

// The literal fixture's own values, written once so a reader can check the
// arithmetic against the assertions.
var (
	litBucketA = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) // a five-minute edge
	litBucketB = time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)

	litLater    = time.Date(2026, 9, 7, 12, 3, 30, 0, time.UTC) // bucket A, fed FIRST
	litEarliest = time.Date(2026, 9, 7, 12, 1, 15, 0, time.UTC) // bucket A, fed SECOND
	litLatest   = time.Date(2026, 9, 7, 12, 7, 45, 0, time.UTC) // bucket B

	litMessage = "disk pressure detected"
	litLabels  = map[string]string{"app": "checkout", "instance": "host-3"}
)

// literalGraph runs the whole produce path over that fixture.
func literalGraph(t *testing.T) ([]framework.Node, []framework.Edge) {
	t.Helper()
	entries := []Entry{
		{Timestamp: litLater, Severity: SeverityInfo, Message: litMessage, Labels: litLabels},
		{Timestamp: litEarliest, Severity: SeverityInfo, Message: litMessage, Labels: litLabels},
		{Timestamp: litLatest, Severity: SeverityInfo, Message: litMessage, Labels: litLabels},
	}
	graph, err := Build(entries, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, edges, err := Emit(graph, nil, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	return nodes, edges
}

// expectedTemplateID hashes the pattern HERE, from the documented rule.
func expectedTemplateID(t *testing.T, pattern string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(pattern))
	return fmt.Sprintf("%x", sum[:16])
}

// expectedStreamID renders and hashes the label set HERE, sorted, from the
// documented rule.
func expectedStreamID(t *testing.T, labels map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte('\n')
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// expectedChunkID builds the documented preimage HERE — stream id, '|', template
// id, '|', the window start as big-endian unix nanoseconds — and hashes it. The
// window is passed in as a LITERAL bucket edge, which is what makes this an
// expectation about the composition of windowStart with ChunkID rather than
// about ChunkID alone.
func expectedChunkID(t *testing.T, streamID, templateID string, window time.Time) string {
	t.Helper()
	var b bytes.Buffer
	b.WriteString(streamID)
	b.WriteByte('|')
	b.WriteString(templateID)
	b.WriteByte('|')
	if err := binary.Write(&b, binary.BigEndian, window.UnixNano()); err != nil {
		t.Fatalf("building the chunk preimage: %v", err)
	}
	sum := sha256.Sum256(b.Bytes())
	return fmt.Sprintf("log-chunk:%x", sum[:16])
}

// TestEveryEmittedIDAgainstAnExpectationComputedInTheTest covers the five id
// families: template, stream, chunk, label and the edge endpoints that reuse
// them. The proxy id has its own literal assertion in proxy_test.go.
func TestEveryEmittedIDAgainstAnExpectationComputedInTheTest(t *testing.T) {
	nodes, edges := literalGraph(t)

	wantTemplate := expectedTemplateID(t, litMessage)
	wantStream := expectedStreamID(t, litLabels)
	wantChunkA := expectedChunkID(t, wantStream, wantTemplate, litBucketA)
	wantChunkB := expectedChunkID(t, wantStream, wantTemplate, litBucketB)

	byType := map[string][]framework.Node{}
	for _, n := range nodes {
		byType[n.Type] = append(byType[n.Type], n)
	}

	t.Run("template", func(t *testing.T) {
		got := byType[NodeLogTemplate]
		if len(got) != 1 {
			t.Fatalf("template nodes = %d, want 1", len(got))
		}
		if got[0].ID != wantTemplate {
			t.Fatalf("template id = %q, want sha256(%q)[:16] = %q", got[0].ID, litMessage, wantTemplate)
		}
		// The alias is a literal too, since the pattern and the severity are.
		if got[0].SymbolName != "disk-pressure-detected@info" {
			t.Fatalf("template SymbolName = %q, want %q", got[0].SymbolName, "disk-pressure-detected@info")
		}
	})

	t.Run("stream", func(t *testing.T) {
		got := byType[NodeLogStream]
		if len(got) != 1 {
			t.Fatalf("stream nodes = %d, want 1", len(got))
		}
		if got[0].ID != wantStream {
			t.Fatalf("stream id = %q, want the sorted-rendering hash %q", got[0].ID, wantStream)
		}
		if got[0].SymbolName != "checkout@host-3" {
			t.Fatalf("stream SymbolName = %q, want %q", got[0].SymbolName, "checkout@host-3")
		}
	})

	t.Run("chunk", func(t *testing.T) {
		got := byType[NodeLogChunk]
		if len(got) != 2 {
			t.Fatalf("chunk nodes = %d, want 2; the fixture straddles a bucket edge", len(got))
		}
		ids := map[string]bool{got[0].ID: true, got[1].ID: true}
		// THE ID IS BUILT FROM THE EPOCH-FLOORED WINDOW, not from any entry's
		// own timestamp. Both expectations are hashed here over the literal
		// bucket edges, so an id derived from the bucket's earliest entry
		// disagrees with both.
		if !ids[wantChunkA] {
			t.Fatalf("no chunk carries the id for the 12:00 bucket (%q); emitted %v", wantChunkA, ids)
		}
		if !ids[wantChunkB] {
			t.Fatalf("no chunk carries the id for the 12:05 bucket (%q); emitted %v", wantChunkB, ids)
		}
	})

	t.Run("labels", func(t *testing.T) {
		got := byType[NodeLogLabel]
		if len(got) != 2 {
			t.Fatalf("label nodes = %d, want 2", len(got))
		}
		want := map[string]bool{"log-label:app=checkout": true, "log-label:instance=host-3": true}
		for _, n := range got {
			if !want[n.ID] {
				t.Fatalf("unexpected label node id %q", n.ID)
			}
			delete(want, n.ID)
		}
		if len(want) != 0 {
			t.Fatalf("missing label nodes: %v", want)
		}
	})

	t.Run("edge endpoints reuse those ids", func(t *testing.T) {
		seen := map[string]int{}
		for _, e := range edges {
			seen[e.Type]++
			switch e.Type {
			case EdgeContains:
				if e.FromID != wantTemplate {
					t.Fatalf("CONTAINS from %q, want the template id %q", e.FromID, wantTemplate)
				}
				if e.ToID != wantChunkA && e.ToID != wantChunkB {
					t.Fatalf("CONTAINS to %q, want one of the two chunk ids", e.ToID)
				}
			case EdgeBelongsTo:
				if e.ToID != wantStream {
					t.Fatalf("BELONGS_TO to %q, want the stream id %q", e.ToID, wantStream)
				}
				if e.FromID != wantChunkA && e.FromID != wantChunkB {
					t.Fatalf("BELONGS_TO from %q, want one of the two chunk ids", e.FromID)
				}
			case EdgeHasLabel:
				if e.FromID != wantStream {
					t.Fatalf("HAS_LABEL from %q, want the stream id %q", e.FromID, wantStream)
				}
			}
		}
		if seen[EdgeContains] != 2 || seen[EdgeBelongsTo] != 2 || seen[EdgeHasLabel] != 2 {
			t.Fatalf("edges = %v, want two of each", seen)
		}
	})
}

// TestEveryEmittedTimeAgainstALiteralExpectation covers the four time values.
// The entries are fed out of order and straddle a bucket edge, so a range that
// took the first entry twice, a swapped pair, a FirstSeen tracking the latest
// and a frozen LastSeen each produce a value no literal here matches.
func TestEveryEmittedTimeAgainstALiteralExpectation(t *testing.T) {
	nodes, _ := literalGraph(t)

	const (
		earliestText = "2026-09-07T12:01:15.000000000Z"
		laterText    = "2026-09-07T12:03:30.000000000Z"
		latestText   = "2026-09-07T12:07:45.000000000Z"
	)

	t.Run("the template's first_seen and last_seen span the whole fixture", func(t *testing.T) {
		for _, n := range nodes {
			if n.Type != NodeLogTemplate {
				continue
			}
			assertMeta(t, n, "first_seen", earliestText)
			assertMeta(t, n, "last_seen", latestText)
		}
	})

	t.Run("each chunk's start_time and end_time bound its own bucket", func(t *testing.T) {
		wantStream := expectedStreamID(t, litLabels)
		wantTemplate := expectedTemplateID(t, litMessage)
		wantByID := map[string][2]string{
			expectedChunkID(t, wantStream, wantTemplate, litBucketA): {earliestText, laterText},
			expectedChunkID(t, wantStream, wantTemplate, litBucketB): {latestText, latestText},
		}
		seen := 0
		for _, n := range nodes {
			if n.Type != NodeLogChunk {
				continue
			}
			want, ok := wantByID[n.ID]
			if !ok {
				t.Fatalf("a chunk carries the unexpected id %q", n.ID)
			}
			seen++
			// START IS THE EARLIEST AND END IS THE LATEST, and in bucket A they
			// are two different entries fed in the opposite order.
			assertMeta(t, n, "start_time", want[0])
			assertMeta(t, n, "end_time", want[1])
		}
		if seen != 2 {
			t.Fatalf("saw %d chunk nodes, want 2", seen)
		}
	})
}
