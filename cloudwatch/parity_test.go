// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// parity_test.go — THE PARITY ARM: the whole emitted graph against an answer
// key produced OUTSIDE this module.
//
// WHERE THE KEY COMES FROM, and why that decides whether this test can fail at
// all. testdata/parity_golden.json was produced by running the BUILT-IN
// CloudWatch adapter and the BUILT-IN log pipeline over this module's own
// recorded fixture, from inside the client module where those packages are
// importable, and writing what they emitted. The built-in path IS the parity
// target this ticket is measured against, so this comparison is the direct
// measurement rather than a proxy for one.
//
// A GOLDEN CAPTURED FROM THIS MODULE'S OWN FIRST RUN WOULD BE ITS OWN ANSWER
// KEY: a systematically wrong scheme — a hash truncated to 16 hex characters
// instead of 16 bytes, a window floored to the requested range instead of the
// epoch, a nanosecond count concatenated as text instead of eight big-endian
// bytes — would be frozen into the key and every later run would agree with it.
// Each of those three produces ids of exactly the right SHAPE, so no structural
// assertion catches them either.
//
// THE ONE VALUE THE KEY DOES NOT CARRY is a chunk's Content, because this
// collector deliberately emits entry text where the built-in emits a compressed
// block (see encodeChunkContent). The golden marks those nodes and this test
// skips that one field; TestChunkContentIsTheEntryBlock asserts it directly
// against a hand-computed expectation instead.

// goldenFile is the external answer key.
//
// THIS MODULE IS OWED NO TEST-CACHE FENCE, and that is a fact about where its
// inputs live rather than an omission. The go tool keys a stored test result on
// the files a run opened but DROPS any opened name that does not resolve inside
// the tested package's own module root, so a test whose subject lives in another
// module can be cacheable against a subject it never read. Every file this
// package opens — the recorded fixture and this answer key — sits under this
// module's own testdata, where the recording hook covers it natively. A test
// added later that reaches outside the module would need a fence; none does.
const goldenFile = "testdata/parity/golden.json"

// goldenNode is one node of the answer key.
type goldenNode struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	SymbolName  string            `json:"symbol_name"`
	Source      string            `json:"source"`
	Description string            `json:"description"`
	ContentOmit bool              `json:"content_omitted"`
	Metadata    map[string]string `json:"metadata"`
}

// goldenEdge is one edge of the answer key.
type goldenEdge struct {
	FromID     string  `json:"from_id"`
	ToID       string  `json:"to_id"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Method     string  `json:"method"`
	Evidence   string  `json:"evidence"`
}

type goldenGraph struct {
	Nodes []goldenNode `json:"nodes"`
	Edges []goldenEdge `json:"edges"`
}

// walkFixture runs the whole walk over the recorded fixture with the fixture's
// DECLARED CLOUD BLOCK attached, and returns the result beside the fixture.
//
// THE PROXY HALF COMES THROUGH THE PRODUCTION ROUTE. The block is handed to the
// walk as the argument the framework delivers it on, so the resolutions the
// parity comparison checks are ones this module derived from declared resources
// rather than ones a test handed it. TestTheDeclaredBlockProducesTheExpectedResolutions
// pins that the derivation lands on the fixture's recorded answer.
//
// THE CORRELATION HALF IS NOW PRODUCTION TOO, and nothing is appended. The
// detector runs inside the walk over this collect's own templates, chunks and
// streams, confirmed against the declared block's cloud edges, and the common
// module renders the edge. An earlier version of this harness supplied the
// correlations from the fixture because the detector did not exist here; the
// fixture no longer carries any.
func walkFixture(t *testing.T) (framework.Result, recordedFixture) {
	t.Helper()
	f := loadFixture(t)

	result, err := collectorWithFake(newFakeClient(t, f)).Walk(
		context.Background(), "parity", f.params(), f.declaredBlock())
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	return result, f
}

// TestTheDeclaredBlockProducesTheExpectedResolutions is the round trip in
// process: the fixture's declared cloud resources in, the resolutions it records
// as expected out, derived rather than handed over.
func TestTheDeclaredBlockProducesTheExpectedResolutions(t *testing.T) {
	f := loadFixture(t)
	if f.declaredBlock().IsEmpty() {
		t.Fatal("the fixture declares no cloud block; every proxy row below would be vacuous")
	}

	streams, _ := buildStreams(fixtureEntries(t, f), DefaultCardinalityThreshold)
	got := resolveStreams(streams, cloudContextFrom(f.declaredBlock()))

	want := f.resolutions()
	if len(got) != len(want) {
		t.Fatalf("the declared block resolved %d labels, the fixture records %d: %+v", len(got), len(want), got)
	}
	index := make(map[string]ResolvedProxy, len(got))
	for _, r := range got {
		index[r.LabelKey+"="+r.LabelValue] = r
	}
	for _, w := range want {
		g, ok := index[w.LabelKey+"="+w.LabelValue]
		if !ok {
			t.Errorf("the declared block did not resolve %s=%s", w.LabelKey, w.LabelValue)
			continue
		}
		if g.Account != w.Account || g.ResourceID != w.ResourceID {
			t.Errorf("%s=%s resolved to %s/%s, want %s/%s",
				w.LabelKey, w.LabelValue, g.Account, g.ResourceID, w.Account, w.ResourceID)
		}
	}
}

// fixtureEntries normalizes the fixture's recorded events without running a
// walk, for tests that need the entries rather than the graph.
func fixtureEntries(t *testing.T, f recordedFixture) []LogEntry {
	t.Helper()
	var out []LogEntry
	for _, g := range f.Groups {
		for _, page := range g.Pages {
			for _, e := range page.Events {
				entry, err := normalizeEntry(buildEvents([]recordedEvent{e})[0], g.LogGroup)
				if err != nil {
					t.Fatalf("normalizing the fixture: %v", err)
				}
				out = append(out, entry)
			}
		}
	}
	return reclassifySeverity(out)
}

// TestParityAgainstBuiltinGolden is R3c, R5-PROXY-PARITY and R5-CORRELATES: the
// whole node and edge set against the built-in path's own output.
func TestParityAgainstBuiltinGolden(t *testing.T) {
	result, _ := walkFixture(t)

	raw, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("reading the answer key: %v", err)
	}
	var want goldenGraph
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decoding the answer key: %v", err)
	}
	if len(want.Nodes) == 0 || len(want.Edges) == 0 {
		t.Fatalf("the answer key carries %d nodes and %d edges; an empty key passes vacuously",
			len(want.Nodes), len(want.Edges))
	}

	compareNodes(t, want.Nodes, result.Nodes)
	compareEdges(t, want.Edges, result.Edges)
}
