// SPDX-License-Identifier: Apache-2.0

//go:build ignore

// THE CONSTRAINT ABOVE IS WHY THIS FILE STILL COMPILES NOTHING AND BREAKS
// NOTHING. The two client packages it imports were deleted with the built-in
// log collectors, so this file cannot build anywhere. `go test ./...` never
// saw it — the go tool excludes testdata from that pattern — but a tool that
// resolves a package by its DIRECTORY does see it, and the pre-commit gate
// that tests the packages containing staged files is exactly such a tool: it
// failed here the first time this file's comment was edited. `ignore` is Go's
// own spelling for a program kept in the tree and built by nothing, and it
// states in the source what the note below states in prose.

// Command generate produces the parity golden this module's suite compares its
// own emission against.
//
// WHY IT LIVES HERE AND RUNS THERE. The comparator is
// cmd/knowledge/internal/collector/logs.MaterializeLogGraph, which is
// client-internal: the collector module may not import it, and the toolchain
// would refuse if it tried. So the golden is generated ONCE, from inside the
// client module, and checked in beside the test that reads it. It sits under
// testdata, which the go tool excludes from every ./... pattern, so no ./...
// build ever saw it. It carries the ignore constraint above as well, because
// ./... is not the only way a tool names a package.
//
// HOW TO RUN IT, AND WHY IT IS TWO STEPS. Go's internal-package rule keys on the
// IMPORTING FILE'S DIRECTORY, not on the module it is compiled into, so this
// file cannot import the comparator from where it sits however it is invoked.
// Regenerating the golden therefore means copying it under the client module's
// own internal tree, running it, and removing the copy — which leaves the client
// module unchanged, as this ticket requires:
//
//	mkdir -p cmd/knowledge/internal/paritygen
//	cp cmd/collectors/stackdriver/testdata/parity/generate.go \
//	   cmd/knowledge/internal/paritygen/main.go
//	(cd cmd/knowledge && go run ./internal/paritygen "$(git rev-parse HEAD)") \
//	   > cmd/collectors/stackdriver/testdata/parity/golden.json
//	rm -rf cmd/knowledge/internal/paritygen
//
// The tree sha is passed IN rather than read: this program is a build tool, and
// a tool that shells out to git is one more thing to reason about when it is
// copied somewhere unexpected.
//
// WHAT THE GOLDEN COVERS AND WHAT IT DOES NOT. It cross-checks everything the
// comparator DERIVES from the fixture: the stream fingerprints, the label node
// ids, the stream and template aliases, every node's type and metadata, the
// proxy ids and their metadata, and the whole edge set with its directions and
// the correlation edge's three carried fields. It does NOT cross-check the
// template id or the chunk id, because both derivations are unexported on the
// comparator's side and cannot be called from here; the fixture supplies those
// two ids directly, identically to both sides, and their derivation is pinned by
// this module's own tests instead.
//
// THE INTERNAL LOG PIPELINE HAS SINCE BEEN DELETED, so this golden IS NOW THE
// SPECIFICATION rather than a snapshot of a live comparator. That is the
// intended end state and not a defect in the test.
// THE RECIPE ABOVE CAN NO LONGER RUN: the packages it imports,
// cmd/knowledge/internal/collector/logs and
// cmd/knowledge/internal/logwire were removed from the client, and the two
// imports below name packages that no longer exist. The file is kept, unbuilt
// under testdata, because it is the provenance of the checked-in golden and is
// what makes the parity claim auditable — it was last runnable at
// a9171723f400f6f4d58fbe6ca27626fb4874c076.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fulminate-io/knowledge/cmd/knowledge/internal/collector/logs"
	wirelogs "github.com/fulminate-io/knowledge/cmd/knowledge/internal/logwire"
)

// baseTime must match the collector module's own fixture instant.
var baseTime = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// The two template ids and the two chunk ids are FIXED here and in the
// collector's fixture, because their derivations are unexported on this side.
const (
	templateAID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	templateBID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	chunkAID    = "log-chunk:cccccccccccccccccccccccccccccccc"
	chunkBID    = "log-chunk:dddddddddddddddddddddddddddddddd"
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

type golden struct {
	GeneratedBy string       `json:"generated_by"`
	Command     string       `json:"command"`
	TreeSHA     string       `json:"tree_sha"`
	Date        string       `json:"date"`
	Note        string       `json:"note"`
	Nodes       []goldenNode `json:"nodes"`
	Edges       []goldenEdge `json:"edges"`
}

func main() {
	templates, streams, chunks, correlations, resolutions := fixture()

	nodes, edges, err := logs.MaterializeLogGraph(
		"parity-fixture", templates, streams, chunks, correlations, resolutions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "materializing: %v\n", err)
		os.Exit(1)
	}

	out := golden{
		GeneratedBy: "cmd/knowledge/internal/collector/logs.MaterializeLogGraph",
		Command: "cp cmd/collectors/stackdriver/testdata/parity/generate.go " +
			"cmd/knowledge/internal/paritygen/main.go && (cd cmd/knowledge && " +
			"go run ./internal/paritygen \"$(git rev-parse HEAD)\") " +
			"> cmd/collectors/stackdriver/testdata/parity/golden.json",
		TreeSHA: treeSHA(),
		Date:    time.Now().UTC().Format(time.RFC3339),
		Note: "Content is base64 of the node's raw Content bytes. The comparator writes the " +
			"chunk payload raw; the collector base64-encodes it, because the collector's own " +
			"envelope is JSON and Go's encoder replaces invalid UTF-8 rather than refusing it.",
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, goldenNode{
			ID:          n.GetId(),
			Type:        n.GetType(),
			SymbolName:  n.GetSymbolName(),
			Content:     base64.StdEncoding.EncodeToString([]byte(n.GetContent())),
			Description: n.GetDescription(),
			Source:      n.GetSource(),
			Metadata:    n.GetMetadata(),
		})
	}
	for _, e := range edges {
		out.Edges = append(out.Edges, goldenEdge{
			FromID:     e.FromID,
			ToID:       e.ToID,
			Type:       string(e.Type),
			Confidence: e.Confidence,
			Method:     e.Method,
			Evidence:   e.Evidence,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encoding: %v\n", err)
		os.Exit(1)
	}
}

// fixture is the five inputs, and it is duplicated field for field in the
// collector module's parity_test.go.
func fixture() ([]*wirelogs.LogTemplate, []*wirelogs.LogStream, []*wirelogs.LogChunk,
	[]wirelogs.CorrelationResult, []wirelogs.ResolvedProxyEntry) {

	templateA := &wirelogs.LogTemplate{
		ID:        templateAID,
		Pattern:   "connect failed to <*>",
		Severity:  wirelogs.SeverityError,
		Count:     3,
		FirstSeen: baseTime,
		LastSeen:  baseTime.Add(time.Minute),
	}
	templateA.Alias = logs.TemplateAliasFor(templateA)

	templateB := &wirelogs.LogTemplate{
		ID:        templateBID,
		Pattern:   "queue drain failed",
		Severity:  wirelogs.SeverityError,
		Count:     2,
		FirstSeen: baseTime,
		LastSeen:  baseTime.Add(30 * time.Second),
	}
	templateB.Alias = logs.TemplateAliasFor(templateB)

	// The tracker sees every value, so both keys classify low-cardinality and
	// every label becomes a shared node.
	tracker := logs.NewCardinalityTracker(0)
	apiLabels := map[string]string{"service": "api", "namespace_name": "prod"}
	workerLabels := map[string]string{"service": "worker", "namespace_name": "prod"}
	for _, set := range []map[string]string{apiLabels, workerLabels} {
		for k, v := range set {
			tracker.Observe(k, v)
		}
	}
	streamA := logs.NewLogStream(apiLabels, tracker)
	streamB := logs.NewLogStream(workerLabels, tracker)

	chunks := []*wirelogs.LogChunk{
		{
			ID:             chunkAID,
			StreamID:       streamA.ID,
			TemplateID:     templateA.ID,
			StartTime:      baseTime,
			EndTime:        baseTime.Add(time.Minute),
			CompressedData: []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x01, 0x02, 0xff},
			EntryCount:     3,
		},
		{
			ID:             chunkBID,
			StreamID:       streamB.ID,
			TemplateID:     templateB.ID,
			StartTime:      baseTime,
			EndTime:        baseTime.Add(30 * time.Second),
			CompressedData: []byte{0x28, 0xb5, 0x2f, 0xfd, 0xfe, 0xfd, 0xfc, 0x00},
			EntryCount:     2,
		},
	}

	correlations := []wirelogs.CorrelationResult{
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

	resolutions := []wirelogs.ResolvedProxyEntry{
		{LabelKey: "service", LabelValue: "api", Account: "acct", ResourceID: "res-api"},
		{LabelKey: "namespace", LabelValue: "prod", Account: "acct", ResourceID: "res-api"},
		{LabelKey: "service", LabelValue: "worker", Account: "acct", ResourceID: "res-worker"},
	}

	return []*wirelogs.LogTemplate{templateA, templateB},
		[]*wirelogs.LogStream{streamA, streamB},
		chunks, correlations, resolutions
}

// treeSHA is the tree the golden was generated at, taken from the one command
// argument so a reader can tell what the comparator looked like when it ran.
func treeSHA() string {
	if len(os.Args) > 1 {
		return os.Args[1]
	}
	return "unrecorded"
}
