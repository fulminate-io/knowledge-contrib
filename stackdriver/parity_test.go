// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// parity_test.go — the whole emitted graph compared, node for node and edge for
// edge, against what the knowledge client's own log materializer produces from
// the SAME five inputs.
//
// THE COMPARATOR IS A CHECKED-IN GOLDEN AND NOT A LIVE CALL, because it lives
// under cmd/knowledge/internal and this module may not import it. The golden
// carries its own provenance — the generating command, the tree it was taken at
// and the date — because a golden whose origin is not written down is a golden
// nobody can regenerate. testdata/parity/generate.go is the program that made it.
//
// ITS LIFETIME IS BOUNDED BY DESIGN. Once the internal log pipeline is deleted
// this file stops being a snapshot of a live comparator and becomes the
// SPECIFICATION of the emitted graph. That is the intended end state.

// The two template ids and the two chunk ids the fixture fixes on both sides,
// because their derivations are unexported on the comparator's side.
const (
	parityTemplateAID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	parityTemplateBID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	parityChunkAID    = "log-chunk:cccccccccccccccccccccccccccccccc"
	parityChunkBID    = "log-chunk:dddddddddddddddddddddddddddddddd"
)

type goldenNode struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	SymbolName  string            `json:"symbol_name,omitempty"`
	Content     string            `json:"content_base64,omitempty"`
	Description string            `json:"description,omitempty"`
	Source      string            `json:"source,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type goldenEdge struct {
	FromID     string  `json:"from_id"`
	ToID       string  `json:"to_id"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence,omitempty"`
	Method     string  `json:"method,omitempty"`
	Evidence   string  `json:"evidence,omitempty"`
}

type goldenGraph struct {
	GeneratedBy string       `json:"generated_by"`
	Command     string       `json:"command"`
	TreeSHA     string       `json:"tree_sha"`
	Date        string       `json:"date"`
	Nodes       []goldenNode `json:"nodes"`
	Edges       []goldenEdge `json:"edges"`
}

// TestTheEmittedGraphMatchesTheComparator is the parity row.
func TestTheEmittedGraphMatchesTheComparator(t *testing.T) {
	want := loadGolden(t)
	templates, streams, chunks, resolutions, correlations := parityFixture()

	nodes, edges, err := assembleGraph(templates, streams, chunks, resolutions, correlations)
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}

	compareNodes(t, want.Nodes, nodes)
	compareEdges(t, want.Edges, edges)
}

// TestTheGoldenCarriesItsProvenance is what keeps the row above regenerable.
func TestTheGoldenCarriesItsProvenance(t *testing.T) {
	g := loadGolden(t)
	if g.GeneratedBy == "" || g.Command == "" || g.TreeSHA == "" || g.Date == "" {
		t.Fatalf("the golden is missing provenance: %+v", struct {
			By, Cmd, SHA, Date string
		}{g.GeneratedBy, g.Command, g.TreeSHA, g.Date})
	}
	if _, err := time.Parse(time.RFC3339, g.Date); err != nil {
		t.Errorf("the golden's date is not a timestamp: %q", g.Date)
	}
}

// TestTheGoldenIsNotEmpty is the same-run control for the comparison: an empty
// golden compared against an empty emission would agree perfectly and prove
// nothing.
func TestTheGoldenIsNotEmpty(t *testing.T) {
	g := loadGolden(t)
	if len(g.Nodes) == 0 || len(g.Edges) == 0 {
		t.Fatalf("the golden holds %d nodes and %d edges", len(g.Nodes), len(g.Edges))
	}
	byType := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		byType[n.Type]++
	}
	for _, want := range []string{nodeTypeLogTemplate, nodeTypeLogStream, nodeTypeLogChunk,
		nodeTypeLogLabel, nodeTypeProxy} {
		if byType[want] == 0 {
			t.Errorf("the golden carries no %s node, so the comparison never covers one", want)
		}
	}
	byEdge := make(map[string]int, len(g.Edges))
	for _, e := range g.Edges {
		byEdge[e.Type]++
	}
	for _, want := range []string{edgeTypeHasLabel, edgeTypeBelongsTo, edgeTypeContains,
		edgeTypeEmittedBy, edgeTypeCorrelatesWith} {
		if byEdge[want] == 0 {
			t.Errorf("the golden carries no %s edge, so the comparison never covers one", want)
		}
	}
}

// parityFixture is the five inputs, duplicated field for field from
// testdata/parity/generate.go. The aliases and the stream ids are DERIVED here
// by this module's own code rather than copied, which is what makes the
// comparison a cross-check of those derivations rather than of a constant.
func parityFixture() ([]*logTemplate, []*logStream, []*logChunk, []resolvedProxyEntry, []correlation.Result) {
	templateA := &logTemplate{
		ID:        parityTemplateAID,
		Pattern:   "connect failed to <*>",
		Severity:  severityError,
		Count:     3,
		FirstSeen: baseTime,
		LastSeen:  baseTime.Add(time.Minute),
	}
	templateA.Alias = templateAliasFor(templateA)

	templateB := &logTemplate{
		ID:        parityTemplateBID,
		Pattern:   "queue drain failed",
		Severity:  severityError,
		Count:     2,
		FirstSeen: baseTime,
		LastSeen:  baseTime.Add(30 * time.Second),
	}
	templateB.Alias = templateAliasFor(templateB)

	tracker := newCardinalityTracker(0)
	apiLabels := labels("service", "api", "namespace_name", "prod")
	workerLabels := labels("service", "worker", "namespace_name", "prod")
	for _, set := range []map[string]string{apiLabels, workerLabels} {
		for k, v := range set {
			tracker.observe(k, v)
		}
	}
	streamA := newLogStream(apiLabels, tracker)
	streamB := newLogStream(workerLabels, tracker)

	chunks := []*logChunk{
		{
			ID:             parityChunkAID,
			StreamID:       streamA.ID,
			TemplateID:     templateA.ID,
			StartTime:      baseTime,
			EndTime:        baseTime.Add(time.Minute),
			CompressedData: []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x01, 0x02, 0xff},
			EntryCount:     3,
		},
		{
			ID:             parityChunkBID,
			StreamID:       streamB.ID,
			TemplateID:     templateB.ID,
			StartTime:      baseTime,
			EndTime:        baseTime.Add(30 * time.Second),
			CompressedData: []byte{0x28, 0xb5, 0x2f, 0xfd, 0xfe, 0xfd, 0xfc, 0x00},
			EntryCount:     2,
		},
	}

	correlations := []correlation.Result{
		{
			TemplateA: templateA.ID, TemplateB: templateB.ID,
			ServiceA: "api", ServiceB: "worker",
			ResourceA: "acct:res-api", ResourceB: "acct:res-worker",
			CooccurrenceScore: 0.5, StructurallyConfirmed: true,
		},
		{
			TemplateA: templateB.ID, TemplateB: templateA.ID,
			ServiceA: "worker", ServiceB: "api",
			CooccurrenceScore: 0.9, StructurallyConfirmed: false,
		},
	}

	resolutions := []resolvedProxyEntry{
		{LabelKey: "service", LabelValue: "api", Account: "acct", ResourceID: "res-api"},
		{LabelKey: "namespace", LabelValue: "prod", Account: "acct", ResourceID: "res-api"},
		{LabelKey: "service", LabelValue: "worker", Account: "acct", ResourceID: "res-worker"},
	}

	return []*logTemplate{templateA, templateB},
		[]*logStream{streamA, streamB},
		chunks, resolutions, correlations
}

// compareNodes compares the two node sets keyed by id, field by field.
//
// THE CONTENT COMPARISON IS THE ONE DELIBERATE DIFFERENCE and it is decoded on
// both sides rather than skipped: the comparator writes the chunk payload as raw
// bytes and this module base64-encodes it, because this module's envelope is
// JSON and Go's encoder replaces invalid UTF-8 rather than refusing it. The
// BYTES must still be identical, which is what this compares.
func compareNodes(t *testing.T, want []goldenNode, got []framework.Node) {
	t.Helper()
	gotByID := make(map[string]framework.Node, len(got))
	for _, n := range got {
		gotByID[n.ID] = n
	}
	wantByID := make(map[string]goldenNode, len(want))
	for _, n := range want {
		wantByID[n.ID] = n
	}

	for id, w := range wantByID {
		g, ok := gotByID[id]
		if !ok {
			t.Errorf("the comparator emitted a %s node %q that this module did not", w.Type, id)
			continue
		}
		if g.Type != w.Type {
			t.Errorf("node %q type = %q, comparator %q", id, g.Type, w.Type)
		}
		if g.SymbolName != w.SymbolName {
			t.Errorf("node %q SymbolName = %q, comparator %q", id, g.SymbolName, w.SymbolName)
		}
		if g.Description != w.Description {
			t.Errorf("node %q Description = %q, comparator %q", id, g.Description, w.Description)
		}
		if g.Source != w.Source {
			t.Errorf("node %q Source = %q, comparator %q", id, g.Source, w.Source)
		}
		compareContent(t, id, g.Content, w.Content)
		compareMetadata(t, id, g.Metadata, w.Metadata)
	}
	for id, g := range gotByID {
		if _, ok := wantByID[id]; !ok {
			t.Errorf("this module emitted a %s node %q the comparator did not", g.Type, id)
		}
	}
}

// compareContent decodes both sides to bytes before comparing.
func compareContent(t *testing.T, id, got, want string) {
	t.Helper()
	wantBytes, err := base64.StdEncoding.DecodeString(want)
	if err != nil {
		t.Fatalf("the golden's content for %q is not base64: %v", id, err)
	}
	if len(wantBytes) == 0 && got == "" {
		return
	}
	gotBytes, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Errorf("node %q content is not base64: %v", id, err)
		return
	}
	if string(gotBytes) != string(wantBytes) {
		t.Errorf("node %q content bytes differ from the comparator's", id)
	}
}

// compareMetadata compares two metadata maps key by key, naming what differs.
//
// THE content_encoding KEY IS THIS MODULE'S OWN and is expected to be absent
// from the comparator's node, because the comparator writes the payload raw and
// has nothing to declare. It is the one admitted addition, and admitting it by
// name rather than by ignoring extra keys is what keeps every OTHER addition a
// failure.
func compareMetadata(t *testing.T, id string, got, want map[string]string) {
	t.Helper()
	for _, k := range sortedKeys(want) {
		if got[k] != want[k] {
			t.Errorf("node %q metadata %s = %q, comparator %q", id, k, got[k], want[k])
		}
	}
	for _, k := range sortedKeys(got) {
		if _, ok := want[k]; ok {
			continue
		}
		if k == "content_encoding" {
			continue
		}
		t.Errorf("node %q carries the metadata key %s=%q, which the comparator does not", id, k, got[k])
	}
}

// compareEdges compares the two edge sets keyed by (from, to, type).
func compareEdges(t *testing.T, want []goldenEdge, got []framework.Edge) {
	t.Helper()
	key := func(from, to, typ string) string { return from + "\x00" + to + "\x00" + typ }

	gotByKey := make(map[string]framework.Edge, len(got))
	for _, e := range got {
		gotByKey[key(e.FromID, e.ToID, e.Type)] = e
	}
	wantByKey := make(map[string]goldenEdge, len(want))
	for _, e := range want {
		wantByKey[key(e.FromID, e.ToID, e.Type)] = e
	}

	for k, w := range wantByKey {
		g, ok := gotByKey[k]
		if !ok {
			t.Errorf("the comparator emitted a %s edge %s->%s that this module did not",
				w.Type, w.FromID, w.ToID)
			continue
		}
		if g.Confidence != w.Confidence {
			t.Errorf("%s edge %s->%s confidence = %v, comparator %v",
				w.Type, w.FromID, w.ToID, g.Confidence, w.Confidence)
		}
		if g.Method != w.Method {
			t.Errorf("%s edge %s->%s method = %q, comparator %q", w.Type, w.FromID, w.ToID, g.Method, w.Method)
		}
		if g.Evidence != w.Evidence {
			t.Errorf("%s edge %s->%s evidence = %q, comparator %q",
				w.Type, w.FromID, w.ToID, g.Evidence, w.Evidence)
		}
	}
	for k, g := range gotByKey {
		if _, ok := wantByKey[k]; !ok {
			t.Errorf("this module emitted a %s edge %s->%s the comparator did not", g.Type, g.FromID, g.ToID)
		}
	}

	// The EMITTED_BY count is asserted separately, because a set comparison
	// keyed by endpoints would hide a duplicate.
	if gotCount, wantCount := countEdges(got), countGoldenEdges(want); gotCount[edgeTypeEmittedBy] != wantCount[edgeTypeEmittedBy] {
		t.Errorf("emitted %d EMITTED_BY edges, comparator %d",
			gotCount[edgeTypeEmittedBy], wantCount[edgeTypeEmittedBy])
	}
}

func countEdges(edges []framework.Edge) map[string]int {
	out := make(map[string]int, len(edges))
	for _, e := range edges {
		out[e.Type]++
	}
	return out
}

func countGoldenEdges(edges []goldenEdge) map[string]int {
	out := make(map[string]int, len(edges))
	for _, e := range edges {
		out[e.Type]++
	}
	return out
}

// loadGolden reads the checked-in comparator output. It lives under this
// module's OWN testdata, so the go test cache already keys on it: a read that
// crossed the module root would be invisible to the cache key and the whole
// package would be cacheable against a subject it never opened.
func loadGolden(t *testing.T) goldenGraph {
	t.Helper()
	body, err := os.ReadFile("testdata/parity/golden.json")
	if err != nil {
		t.Fatalf("reading the parity golden: %v", err)
	}
	var g goldenGraph
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatalf("decoding the parity golden: %v", err)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	return g
}
