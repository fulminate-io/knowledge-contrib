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

// Package cloudwatch — THE GENERATOR FOR THIS MODULE'S PARITY ANSWER KEY.
//
// WHY IT LIVES HERE AND RUNS THERE. The key is the BUILT-IN path's output, and
// that path is client-internal: this collector module may not import it and the
// toolchain would refuse if it tried. So the key is generated from inside the
// client module and checked in beside the test that reads it. This file sits
// under testdata, which the go tool excludes from every ./... pattern, so no
// ./... build ever saw it. It carries the ignore constraint above as well,
// because ./... is not the only way a tool names a package.
//
// WHY IT IS A TEST RATHER THAN A COMMAND, which is where it differs from the
// sibling generators. Two of the three built-in functions it must drive —
// the CloudWatch adapter's own pagination and its own event normalization — are
// UNEXPORTED in package cloudwatch under the client's internal tree. A program
// cannot reach them from anywhere. A file compiled INTO that package can, and
// the cheapest way to compile a file into a package and run it is a test.
// Normalizing the fixture with anything else would defeat the whole point: the
// key would then carry this module's reading of the events rather than the
// built-in's.
//
// HOW TO REGENERATE THE KEY, from the repository root. The copy is removed
// afterwards, so the client module is left exactly as it was:
//
//	root=$(pwd)
//	cp cmd/collectors/cloudwatch/testdata/parity/generate.go \
//	   cmd/knowledge/internal/collector/logs/cloudwatch/zz_parity_gen_test.go
//	(cd cmd/knowledge && \
//	  KN_CLOUDWATCH_PARITY_FIXTURE="$root/cmd/collectors/cloudwatch/testdata/events_parity.json" \
//	  KN_CLOUDWATCH_PARITY_GOLDEN="$root/cmd/collectors/cloudwatch/testdata/parity/golden.json" \
//	  go test ./internal/collector/logs/cloudwatch/ -run TestGenerateCloudwatchParityGolden -v)
//	rm cmd/knowledge/internal/collector/logs/cloudwatch/zz_parity_gen_test.go
//
// THE TWO PATHS ARE PARAMETERS, NOT LITERALS, which is what lets the same file
// regenerate a key for a fixture that has moved and is why the generator needs
// to know nothing about where this module lives.
//
// THEY ARE ABSOLUTE, AND THAT IS NOT STYLE. `go test` runs a test binary with
// the PACKAGE directory as its working directory, not the module directory the
// command was issued from, so a path relative to either one resolves against
// neither. A relative path here fails with a missing file rather than silently
// writing the key somewhere unexpected, but it fails every time.
//
// IT SKIPS WHEN THEY ARE UNSET, so a copy left behind by accident is inert
// rather than rewriting a checked-in key on somebody else's test run.
//
// WHAT THE KEY COVERS AND WHAT IT DOES NOT. It carries every node the built-in
// materializer produced and every edge, with the fields the comparison reads:
// ids, types, symbol names, sources, descriptions and whole metadata maps, plus
// each edge's direction, confidence, method and evidence. Chunk CONTENT is
// deliberately omitted and marked, because this module emits the entry text
// where the built-in emits a compressed block — a documented divergence, checked
// separately by the module's own test.
//
// THE BUILT-IN PATH HAS SINCE BEEN DELETED, so the checked-in key IS NOW THE
// SPECIFICATION rather than a snapshot of a live comparator, and THE RECIPE
// ABOVE CAN NO LONGER RUN: cmd/knowledge/internal/collector/logs and
// cmd/knowledge/internal/logwire were removed from the client, and the two
// imports below name packages that no longer exist. The file is kept, unbuilt
// under testdata, because it is the provenance of the checked-in key and is
// what makes the parity claim auditable — it was last runnable at
// a9171723f400f6f4d58fbe6ca27626fb4874c076.
package cloudwatch

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/fulminate-io/knowledge/cmd/knowledge/internal/collector/logs"
	"github.com/fulminate-io/knowledge/cmd/knowledge/internal/kgwire"
	logwire "github.com/fulminate-io/knowledge/cmd/knowledge/internal/logwire"
	knowledgev1 "github.com/fulminate-io/knowledge/gen/knowledge/v1"
)

// The two path parameters, and the switch that makes an unset one a skip.
const (
	parityFixtureEnv = "KN_CLOUDWATCH_PARITY_FIXTURE"
	parityGoldenEnv  = "KN_CLOUDWATCH_PARITY_GOLDEN"
)

type parityEvent struct {
	EventID       string `json:"event_id"`
	Timestamp     *int64 `json:"timestamp"`
	LogStreamName string `json:"log_stream_name"`
	Message       string `json:"message"`
}

type parityNodeDecl struct {
	ID         string            `json:"id"`
	SymbolName string            `json:"symbol_name"`
	Metadata   map[string]string `json:"metadata"`
}

type parityEdgeDecl struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
}

type parityFixture struct {
	Groups []struct {
		LogGroup string `json:"log_group"`
		Pages    []struct {
			NextToken string        `json:"next_token"`
			Events    []parityEvent `json:"events"`
		} `json:"pages"`
	} `json:"groups"`
	CloudContext struct {
		Cloud []struct {
			GraphName string           `json:"graph_name"`
			Nodes     []parityNodeDecl `json:"nodes"`
			Edges     []parityEdgeDecl `json:"edges"`
		} `json:"cloud"`
	} `json:"cloud_context"`
}

// parityFake serves the fixture's recorded pages to the built-in adapter.
type parityFake struct {
	pages  map[string][]*cloudwatchlogs.FilterLogEventsOutput
	cursor map[string]int
}

func (g *parityFake) FilterLogEvents(
	_ context.Context, in *cloudwatchlogs.FilterLogEventsInput, _ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	name := aws.ToString(in.LogGroupName)
	idx := g.cursor[name]
	if idx >= len(g.pages[name]) {
		return &cloudwatchlogs.FilterLogEventsOutput{}, nil
	}
	g.cursor[name]++
	return g.pages[name][idx], nil
}

// parityCloud answers the built-in pipeline's two cloud questions from the
// fixture's declared block — the same block the collector module answers them
// from, so the key exercises the built-in's correlation stage rather than
// leaving it dark.
type parityCloud struct {
	byName    map[string]logs.ResolvedResource
	dependsOn map[string]struct{}
}

func (c *parityCloud) ResolveService(
	_ context.Context, _ *logwire.LogStream, name string,
) (logs.ResolvedResource, bool) {
	r, ok := c.byName[name]
	return r, ok
}

func (c *parityCloud) ResolveNamespace(
	_ context.Context, _ *logwire.LogStream, name string,
) (logs.ResolvedResource, bool) {
	r, ok := c.byName[name]
	return r, ok
}

func (c *parityCloud) HasDependency(_ context.Context, a, b logs.ResolvedResource) bool {
	if a.Account != b.Account {
		return false
	}
	_, ok := c.dependsOn[a.Account+"\x00"+a.ID+"\x00"+b.ID]
	return ok
}

type parityNode struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	SymbolName  string            `json:"symbol_name,omitempty"`
	Source      string            `json:"source,omitempty"`
	Description string            `json:"description,omitempty"`
	ContentOmit bool              `json:"content_omitted,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type parityEdge struct {
	FromID     string  `json:"from_id"`
	ToID       string  `json:"to_id"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence,omitempty"`
	Method     string  `json:"method,omitempty"`
	Evidence   string  `json:"evidence,omitempty"`
}

// TestGenerateCloudwatchParityGolden writes the key. It is a generator rather
// than an assertion: it fails only when it cannot produce one.
func TestGenerateCloudwatchParityGolden(t *testing.T) {
	fixturePath, goldenPath := os.Getenv(parityFixtureEnv), os.Getenv(parityGoldenEnv)
	if fixturePath == "" || goldenPath == "" {
		t.Skipf("set %s and %s to regenerate the cloudwatch collector's parity key",
			parityFixtureEnv, parityGoldenEnv)
	}

	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var f parityFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	if len(f.Groups) == 0 {
		t.Fatalf("the fixture names no log group, so the key would be empty")
	}

	entries := parityEntries(t, f)
	result := parityCollect(t, entries, f)
	nodes, edges, err := logs.MaterializeLogGraph(
		"t8-parity", result.Templates, result.Streams, result.Chunks, result.Correlations, result.Resolutions)
	if err != nil {
		t.Fatalf("materializing: %v", err)
	}

	body, err := json.MarshalIndent(
		map[string]any{"nodes": parityNodes(nodes), "edges": parityEdges(edges)}, "", "  ")
	if err != nil {
		t.Fatalf("marshaling the key: %v", err)
	}
	if err := os.WriteFile(goldenPath, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("writing the key: %v", err)
	}
	t.Logf("wrote %d nodes and %d edges to %s (%d correlations, %d confirmed, %d resolutions)",
		len(nodes), len(edges), goldenPath,
		len(result.Correlations), result.CorrelationsFound, len(result.Resolutions))
}

// parityEntries drives the BUILT-IN adapter's own pagination and normalization
// over the recorded pages, which is the whole reason this file must compile into
// that package.
func parityEntries(t *testing.T, f parityFixture) []logwire.LogEntry {
	t.Helper()
	fake := &parityFake{pages: map[string][]*cloudwatchlogs.FilterLogEventsOutput{}, cursor: map[string]int{}}
	for _, g := range f.Groups {
		for _, p := range g.Pages {
			events := make([]cwtypes.FilteredLogEvent, 0, len(p.Events))
			for _, e := range p.Events {
				ev := cwtypes.FilteredLogEvent{
					EventId:   aws.String(e.EventID),
					Timestamp: e.Timestamp,
					Message:   aws.String(e.Message),
				}
				if e.LogStreamName != "" {
					ev.LogStreamName = aws.String(e.LogStreamName)
				}
				events = append(events, ev)
			}
			out := &cloudwatchlogs.FilterLogEventsOutput{Events: events}
			if p.NextToken != "" {
				out.NextToken = aws.String(p.NextToken)
			}
			fake.pages[g.LogGroup] = append(fake.pages[g.LogGroup], out)
		}
	}

	var entries []logwire.LogEntry
	emit := func(batch []logwire.LogEntry) error {
		entries = append(entries, batch...)
		return nil
	}
	for _, g := range f.Groups {
		in := &cloudwatchlogs.FilterLogEventsInput{LogGroupName: aws.String(g.LogGroup), Limit: aws.Int32(10000)}
		if err := paginate(context.Background(), fake, in, logwire.Query{}, emit); err != nil {
			t.Fatalf("paginating %s: %v", g.LogGroup, err)
		}
	}
	return logs.ReclassifySeverity(entries)
}

// parityCollect runs the built-in pipeline with the fixture's declared block
// wired as its resolver and its dependency checker.
func parityCollect(t *testing.T, entries []logwire.LogEntry, f parityFixture) *logs.CollectResult {
	t.Helper()
	cloud := &parityCloud{
		byName:    map[string]logs.ResolvedResource{},
		dependsOn: map[string]struct{}{},
	}
	for _, graph := range f.CloudContext.Cloud {
		for _, node := range graph.Nodes {
			if node.SymbolName == "" {
				continue
			}
			cloud.byName[node.SymbolName] = logs.ResolvedResource{Account: graph.GraphName, ID: node.ID}
		}
		for _, edge := range graph.Edges {
			if edge.FromID == "" || edge.ToID == "" {
				continue
			}
			cloud.dependsOn[graph.GraphName+"\x00"+edge.FromID+"\x00"+edge.ToID] = struct{}{}
			cloud.dependsOn[graph.GraphName+"\x00"+edge.ToID+"\x00"+edge.FromID] = struct{}{}
		}
	}

	result, err := logs.NewPipeline(nil, "t8-parity",
		logs.WithCloudResolver(cloud), logs.WithDependencyChecker(cloud)).
		CollectFromEntries(context.Background(), entries, logwire.Query{})
	if err != nil {
		t.Fatalf("collecting: %v", err)
	}
	return result
}

// parityNodes and parityEdges project and sort what the materializer produced.
// The sort is what makes two runs over one fixture byte-identical.
func parityNodes(nodes []*knowledgev1.Node) []parityNode {
	out := make([]parityNode, 0, len(nodes))
	for _, n := range nodes {
		pn := parityNode{
			ID: n.GetId(), Type: n.GetType(), SymbolName: n.GetSymbolName(),
			Source: n.GetSource(), Description: n.GetDescription(), Metadata: n.GetMetadata(),
		}
		if n.GetType() == "log-chunk" {
			pn.ContentOmit = true
		}
		out = append(out, pn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func parityEdges(edges []kgwire.BatchEdge) []parityEdge {
	out := make([]parityEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, parityEdge{
			FromID: e.FromID, ToID: e.ToID, Type: string(e.Type),
			Confidence: e.Confidence, Method: e.Method, Evidence: e.Evidence,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromID != out[j].FromID {
			return out[i].FromID < out[j].FromID
		}
		if out[i].ToID != out[j].ToID {
			return out[i].ToID < out[j].ToID
		}
		return out[i].Type < out[j].Type
	})
	return out
}
