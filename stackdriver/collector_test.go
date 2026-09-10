// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector_test.go — the walk end to end against recorded entries: the result
// it returns, the completeness it asserts, and the failures it refuses to
// swallow.

// TestWalkProducesAWellFormedCompleteResult is the ordinary path.
func TestWalkProducesAWellFormedCompleteResult(t *testing.T) {
	c := &Collector{read: readerReturning(drainResult{Entries: recordedEntries()}), config: defaultPipelineConfig()}
	got, err := c.Walk(context.Background(), "collect-1", params{Project: "fulminate-services"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	if !got.Complete.IsAsserted() || !got.Complete.IsComplete() {
		t.Errorf("completeness = asserted %v complete %v, want an asserted complete walk",
			got.Complete.IsAsserted(), got.Complete.IsComplete())
	}
	if len(got.Nodes) == 0 || len(got.Edges) == 0 {
		t.Fatalf("the walk produced %d nodes and %d edges", len(got.Nodes), len(got.Edges))
	}
	for i, n := range got.Nodes {
		if n.ID == "" || n.Type == "" {
			t.Errorf("node %d has id %q and type %q; both are required by the contract", i, n.ID, n.Type)
		}
	}
}

// TestWalkAssertsAnIncompleteWalkWhenTheReadWasTruncated is the arm that decides
// whether the server may treat rows this collect did not carry as gone. A
// provider that always asserts completeness replaces the graph with its own
// truncated view.
func TestWalkAssertsAnIncompleteWalkWhenTheReadWasTruncated(t *testing.T) {
	c := &Collector{
		read:   readerReturning(drainResult{Entries: recordedEntries(), Truncated: true}),
		config: defaultPipelineConfig(),
	}
	got, err := c.Walk(context.Background(), "collect-1", params{Project: "p", MaxEntries: new(3)}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	if got.Complete.IsComplete() {
		t.Fatalf("a truncated read asserted a complete walk")
	}
	if !strings.Contains(got.Complete.Reason(), "3") {
		t.Errorf("the incompleteness reason does not name the bound: %q", got.Complete.Reason())
	}
	// The rows it DID see are still emitted: an incomplete walk is a partial
	// graph honestly labeled, not an empty one.
	if len(got.Nodes) == 0 {
		t.Errorf("a truncated walk emitted no nodes")
	}
}

// TestWalkOverNoEntriesIsACompleteEmptyResult is the zero-entry input class. An
// empty region or a filter that matched nothing is the first real run of a new
// collector, so this is the cell it hits.
func TestWalkOverNoEntriesIsACompleteEmptyResult(t *testing.T) {
	c := &Collector{read: readerReturning(drainResult{}), config: defaultPipelineConfig()}
	got, err := c.Walk(context.Background(), "collect-1", params{Project: "p"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("an empty walk failed: %v", err)
	}
	if !got.Complete.IsComplete() {
		t.Errorf("an exhausted empty read asserted an incomplete walk")
	}
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Errorf("an empty walk emitted %d nodes and %d edges", len(got.Nodes), len(got.Edges))
	}
}

// TestWalkRefusesBadParamsBeforeReading pins the order: a refused parameter must
// not reach the provider, or a mistyped timestamp becomes a Cloud Logging call.
func TestWalkRefusesBadParamsBeforeReading(t *testing.T) {
	reached := false
	c := &Collector{
		read: func(context.Context, string, logQuery) (drainResult, error) {
			reached = true
			return drainResult{}, nil
		},
		config: defaultPipelineConfig(),
	}
	if _, err := c.Walk(context.Background(), "collect-1", params{}, framework.ForeignContext{}); err == nil {
		t.Fatalf("a walk with no project succeeded")
	}
	if reached {
		t.Errorf("the read ran despite the params being refused")
	}
}

// TestWalkSurfacesAReadFailureRatherThanAnEmptyResult is the failure posture at
// the walk level: an error becomes a tool error the client treats as a refused
// collect, never a successful walk that found nothing.
func TestWalkSurfacesAReadFailureRatherThanAnEmptyResult(t *testing.T) {
	boom := errors.New("the read broke")
	c := &Collector{read: readerFailing(boom), config: defaultPipelineConfig()}
	got, err := c.Walk(context.Background(), "collect-1", params{Project: "p"}, framework.ForeignContext{})
	if err == nil {
		t.Fatalf("a failed read returned a result with %d nodes and no error", len(got.Nodes))
	}
	if !errors.Is(err, boom) {
		t.Errorf("the cause was not wrapped: %v", err)
	}
}

// TestTwoWalksOverTheSameEntriesProduceIdenticalGraphs is the carry-forward
// property, asserted at the walk. It runs THIRTY times rather than twice: the
// mutation this row exists to catch is replacing the settled remap rule with a
// map walk, and on a fixture with three survivors two runs agree by luck more
// often than not, so a small repetition count turns that mutation into a flake
// rather than a red.
func TestTwoWalksOverTheSameEntriesProduceIdenticalGraphs(t *testing.T) {
	entries := append(recordedEntries(), goPanicEntries()...)
	c := &Collector{read: readerReturning(drainResult{Entries: entries}), config: defaultPipelineConfig()}

	first := walkFingerprint(t, c)
	for i := range 30 {
		if got := walkFingerprint(t, c); got != first {
			t.Fatalf("run %d produced a different graph from run 0", i+1)
		}
	}

	// SAME-RUN CONTROL: a collect with NO consolidated template is stable too,
	// so the stability above is not an artifact of the fixture being trivial.
	plain := &Collector{read: readerReturning(drainResult{Entries: recordedEntries()}), config: defaultPipelineConfig()}
	plainFirst := walkFingerprint(t, plain)
	for range 30 {
		if got := walkFingerprint(t, plain); got != plainFirst {
			t.Fatalf("the no-consolidation control produced a different graph")
		}
	}
}

// TestTheToolIsNamedAndDescribed covers the advertised identity an operator
// reads in a tool listing.
func TestTheToolIsNamedAndDescribed(t *testing.T) {
	spec := New().Tool()
	if spec.Name != toolName {
		t.Errorf("tool name = %q, want %q", spec.Name, toolName)
	}
	if !strings.Contains(spec.Description, "Application Default Credentials") {
		t.Errorf("the description does not say where credentials come from: %q", spec.Description)
	}
}

// TestNewInstallsTheRealReader is the one assertion that the shipped binary is
// not wired to a fixture.
func TestNewInstallsTheRealReader(t *testing.T) {
	if New().read == nil {
		t.Fatalf("New built a collector with no reader")
	}
}

// walkFingerprint runs one walk and renders its whole result.
func walkFingerprint(t *testing.T, c *Collector) string {
	t.Helper()
	got, err := c.Walk(context.Background(), "collect-1", params{Project: "p"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	return renderResult(t, got)
}

// renderResult is a whole framework.Result as one comparable string.
func renderResult(t *testing.T, r framework.Result) string {
	t.Helper()
	var b strings.Builder
	for _, n := range r.Nodes {
		b.WriteString(n.ID + "|" + n.Type + "|" + n.SymbolName + "|" + n.Content + "|")
		for _, k := range sortedKeys(n.Metadata) {
			b.WriteString(k + "=" + n.Metadata[k] + ",")
		}
		b.WriteString("\n")
	}
	for _, e := range r.Edges {
		b.WriteString(e.FromID + "->" + e.ToID + "|" + e.Type + "|" + e.Method + "|" + e.Evidence + "\n")
	}
	return b.String()
}
