// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// correlate_test.go — the seam between this module and the common correlation
// detector: the vocabulary both sides read, and the adapter that carries this
// module's objects across.

// TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo is the seam pin
// between two copies of one vocabulary.
//
// THE FILTER LIVES ON THE OTHER SIDE AND THE SPELLING LIVES HERE. The common
// detector keeps only templates at ERROR or above, and it reads the severity
// string this module writes into a Template. Nothing in the compiler relates the
// two vocabularies, so a rename or a re-ordering on either side would silently
// stop correlating this collector's errors while every other test stayed green.
// This arm relates them: same six names, same rank at every pair, same verdict
// at the one comparison the filter actually makes.
func TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo(t *testing.T) {
	levels := []string{SeverityTrace, SeverityDebug, SeverityInfo, SeverityWarn, SeverityError, SeverityCritical}
	common := []string{
		correlation.SeverityTrace, correlation.SeverityDebug, correlation.SeverityInfo,
		correlation.SeverityWarn, correlation.SeverityError, correlation.SeverityCritical,
	}
	for i, name := range levels {
		if name != common[i] {
			t.Errorf("severity %d is %q here and %q in the common detector", i, name, common[i])
		}
	}
	for _, level := range levels {
		for _, minimum := range levels {
			if got, want := correlation.SeverityAtLeast(level, minimum), SeverityAtLeast(level, minimum); got != want {
				t.Errorf("SeverityAtLeast(%q, %q) = %v in the common detector and %v here", level, minimum, got, want)
			}
		}
	}
	// THE ONE COMPARISON THE FILTER MAKES, stated on its own so a reader sees
	// which way it must go.
	if !correlation.SeverityAtLeast(SeverityError, correlation.SeverityError) {
		t.Error("this module's ERROR does not satisfy the common detector's ERROR minimum")
	}
	if correlation.SeverityAtLeast(SeverityWarn, correlation.SeverityError) {
		t.Error("this module's WARN satisfies the common detector's ERROR minimum")
	}
}

// TestTheServiceIdentifyingKeysAreTheCommonDetectorsToo is the second half of
// the same seam. The detector names the owning service by reading these keys off
// a Stream in a fixed precedence; this module reads the same keys to decide
// which labels are worth resolving. Two lists that drifted would resolve a proxy
// for one label and correlate on another.
func TestTheServiceIdentifyingKeysAreTheCommonDetectorsToo(t *testing.T) {
	for _, key := range []string{
		correlation.FieldService, correlation.FieldNamespace,
		correlation.FieldDeployment, correlation.FieldApp,
	} {
		if !serviceIdentifyingKeys[key] {
			t.Errorf("the common detector reads the label %q and this module does not resolve it", key)
		}
	}
	if len(serviceIdentifyingKeys) != 4 {
		t.Errorf("this module resolves %d label keys and the common detector reads 4", len(serviceIdentifyingKeys))
	}
}

// correlationFixture is two services logging errors in the same minute, with a
// declared cloud graph naming both resources and an edge between them.
func correlationFixture(t *testing.T) (*Graph, []framework.ForeignGraph) {
	t.Helper()
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: base, Severity: SeverityError, Message: "checkout upstream refused",
			Labels: map[string]string{"service": "checkout"}},
		{Timestamp: base.Add(2 * time.Second), Severity: SeverityError, Message: "payments gateway timed out",
			Labels: map[string]string{"service": "payments"}},
	}
	graph, err := Build(entries, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	cloud := []framework.ForeignGraph{{
		GraphName: "acct-1",
		Nodes: []framework.ForeignNode{
			{ID: "i-checkout", Type: "ec2:instance", SymbolName: "checkout"},
			{ID: "i-payments", Type: "ec2:instance", SymbolName: "payments"},
		},
		Edges: []framework.ForeignEdge{{FromID: "i-checkout", ToID: "i-payments"}},
	}}
	return graph, cloud
}

// TestAConfirmedPairBecomesAnEdgeAndAnUnconfirmedOneDoesNot drives the whole
// adapter: this module's templates, chunks and streams into the detector, its
// cloud context answering both questions, and the common materializer rendering
// the edge.
func TestAConfirmedPairBecomesAnEdgeAndAnUnconfirmedOneDoesNot(t *testing.T) {
	graph, cloud := correlationFixture(t)

	t.Run("a declared dependency confirms the pair", func(t *testing.T) {
		ctx := NewCloudContext(cloud)
		resolutions := ResolutionsFromContext(graph.Streams, ctx)
		results, err := FindCorrelations(graph, resolutions, ctx)
		if err != nil {
			t.Fatalf("FindCorrelations: %v", err)
		}
		confirmed := 0
		for _, r := range results {
			if r.StructurallyConfirmed {
				confirmed++
			}
		}
		if confirmed == 0 {
			t.Fatalf("no candidate was confirmed; results = %+v", results)
		}

		_, edges, err := Emit(graph, resolutions, results)
		if err != nil {
			t.Fatalf("Emit: %v", err)
		}
		n := 0
		for _, e := range edges {
			if e.Type != EdgeCorrelatesWith {
				continue
			}
			n++
			if e.Method != correlation.CorrelationMethod {
				t.Fatalf("method = %q, want %q", e.Method, correlation.CorrelationMethod)
			}
			if e.Confidence <= 0 {
				t.Fatalf("confidence = %v, want the cooccurrence score", e.Confidence)
			}
			// THE RESOURCES SIDE COMES FROM THIS MODULE'S OWN RESOLUTION SET,
			// which is what the proxy map carries, so an edge names the same
			// resources the proxies do.
			if !contains(e.Evidence, "acct-1:i-checkout") || !contains(e.Evidence, "acct-1:i-payments") {
				t.Fatalf("evidence = %q, want both declared resources", e.Evidence)
			}
		}
		if n != confirmed {
			t.Fatalf("%d CORRELATES_WITH edges for %d confirmed pairs", n, confirmed)
		}
	})

	t.Run("the same fixture with no declared edge confirms nothing", func(t *testing.T) {
		// THE CONTROL, and it varies ONE thing: the same two services, the same
		// two resources, the same overlap, and no dependency between them.
		noEdge := []framework.ForeignGraph{{
			GraphName: cloud[0].GraphName,
			Nodes:     cloud[0].Nodes,
		}}
		ctx := NewCloudContext(noEdge)
		resolutions := ResolutionsFromContext(graph.Streams, ctx)
		results, err := FindCorrelations(graph, resolutions, ctx)
		if err != nil {
			t.Fatalf("FindCorrelations: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("the control produced no candidates at all, so it does not discriminate on confirmation")
		}
		for _, r := range results {
			if r.StructurallyConfirmed {
				t.Fatalf("a pair was confirmed with no declared dependency: %+v", r)
			}
		}
		_, edges, err := Emit(graph, resolutions, results)
		if err != nil {
			t.Fatalf("Emit: %v", err)
		}
		for _, e := range edges {
			if e.Type == EdgeCorrelatesWith {
				t.Fatalf("an unconfirmed pair emitted an edge: %+v", e)
			}
		}
	})

	t.Run("no declared cloud family confirms nothing", func(t *testing.T) {
		results, err := FindCorrelations(graph, nil, NewCloudContext(nil))
		if err != nil {
			t.Fatalf("FindCorrelations: %v", err)
		}
		for _, r := range results {
			if r.StructurallyConfirmed {
				t.Fatalf("a pair was confirmed with no cloud context: %+v", r)
			}
		}
	})
}

// TestTheAdapterPassesANilContextAsANilInterface is the nil-safety cell the
// common module's contract turns on: an interface holding a typed nil pointer is
// not nil, so the detector would call through it and panic rather than leave
// every candidate unconfirmed.
func TestTheAdapterPassesANilContextAsANilInterface(t *testing.T) {
	graph, _ := correlationFixture(t)
	// NewCloudContext returns an untyped nil for an empty slice, and
	// FindCorrelations must pass that through without wrapping it.
	if ctx := NewCloudContext(nil); ctx != nil {
		t.Fatalf("NewCloudContext(nil) returned %T, want an untyped nil", ctx)
	}
	if _, err := FindCorrelations(graph, nil, NewCloudContext(nil)); err != nil {
		t.Fatalf("FindCorrelations with no context: %v", err)
	}
}

// TestMalformedPipelineOutputFailsTheWalk covers the detector's refusals reaching
// this module: they report this collector's own pipeline contradicting itself,
// so they are errors rather than input to tolerate.
//
// ALL THREE PASS-THROUGH GUARDS ARE DRIVEN, one arm each. The adapter appends a
// nil element rather than dropping it, in every one of its three projections, so
// the detector refuses it by name and index; a projection that dropped the nil
// would hand the detector a shorter slice than the pipeline produced and swallow
// the refusal, which is a silent degrade in the one place this collector exists
// to be loud. Two of the three were unobserved until this arm covered them.
func TestMalformedPipelineOutputFailsTheWalk(t *testing.T) {
	graph, cloud := correlationFixture(t)
	ctx := NewCloudContext(cloud)

	t.Run("a nil template", func(t *testing.T) {
		g := &Graph{Templates: append([]*Template{nil}, graph.Templates...), Chunks: graph.Chunks, Streams: graph.Streams}
		if _, err := FindCorrelations(g, nil, ctx); err == nil {
			t.Fatal("a nil template was accepted")
		}
	})
	t.Run("a template with no id", func(t *testing.T) {
		g := &Graph{
			Templates: append([]*Template{{Severity: SeverityError}}, graph.Templates...),
			Chunks:    graph.Chunks, Streams: graph.Streams,
		}
		if _, err := FindCorrelations(g, nil, ctx); err == nil {
			t.Fatal("a template with no id was accepted")
		}
	})
	t.Run("a nil chunk", func(t *testing.T) {
		g := &Graph{Templates: graph.Templates, Chunks: append([]*Chunk{nil}, graph.Chunks...), Streams: graph.Streams}
		if _, err := FindCorrelations(g, nil, ctx); err == nil {
			t.Fatal("a nil chunk was accepted")
		}
	})
	t.Run("a nil stream", func(t *testing.T) {
		g := &Graph{Templates: graph.Templates, Chunks: graph.Chunks, Streams: append([]*Stream{nil}, graph.Streams...)}
		if _, err := FindCorrelations(g, nil, ctx); err == nil {
			t.Fatal("a nil stream was accepted")
		}
	})
	t.Run("a nil graph yields nothing rather than an error", func(t *testing.T) {
		results, err := FindCorrelations(nil, nil, ctx)
		if err != nil || results != nil {
			t.Fatalf("FindCorrelations(nil) = %v, %v", results, err)
		}
	})
}

// TestADeclaredEdgeAnswersInBothDirections is the symmetry cell, and it is a
// direct one because the detector chooses which of a pair is A: a fixture that
// happened to ask in the declared direction would leave the reverse key
// unobserved, and the next fixture would ask the other way and fail.
//
// "These two resources are connected" is what upgrades a coincidence to a
// correlation, and connection is symmetric even where the declared edge is not.
func TestADeclaredEdgeAnswersInBothDirections(t *testing.T) {
	ctx := NewCloudContext([]framework.ForeignGraph{{
		GraphName: "acct-1",
		Nodes: []framework.ForeignNode{
			{ID: "i-a", SymbolName: "checkout"},
			{ID: "i-b", SymbolName: "payments"},
			{ID: "i-c", SymbolName: "search"},
		},
		Edges: []framework.ForeignEdge{{FromID: "i-a", ToID: "i-b"}},
	}})
	a := Resource{Account: "acct-1", ID: "i-a"}
	b := Resource{Account: "acct-1", ID: "i-b"}
	c := Resource{Account: "acct-1", ID: "i-c"}

	if !ctx.HasDependency(a, b) {
		t.Error("the declared direction does not answer")
	}
	if !ctx.HasDependency(b, a) {
		t.Error("the REVERSE of the declared direction does not answer; connection is symmetric")
	}
	// The control: a resource with no declared edge is connected to neither.
	if ctx.HasDependency(a, c) || ctx.HasDependency(c, a) {
		t.Error("a resource with no declared edge answered as connected")
	}
}

// TestADeclaredEdgeAnswersOnlyForTheGraphThatDeclaredIt is the oracle's SCOPE,
// and it is a different mechanism from the account rule below: that rule refuses
// a pair whose two resources sit in different accounts, and it fires before the
// lookup, so it says nothing about which graph's edges the lookup reads. THE
// UNCOVERED CASE IS A SAME-ACCOUNT PAIR, and the fixture is two declared graphs
// carrying THE SAME TWO IDS where only the second declares the edge between
// them. An oracle keyed on the two ids alone would let the second graph's edge
// confirm the first graph's pair and emit a CORRELATES_WITH edge no declared
// graph carries.
func TestADeclaredEdgeAnswersOnlyForTheGraphThatDeclaredIt(t *testing.T) {
	ctx := NewCloudContext([]framework.ForeignGraph{
		{
			GraphName: "acct-1",
			Nodes: []framework.ForeignNode{
				{ID: "i-1", SymbolName: "checkout"}, {ID: "i-2", SymbolName: "payments"},
			},
		},
		{
			GraphName: "acct-2",
			Nodes: []framework.ForeignNode{
				{ID: "i-1", SymbolName: "checkout"}, {ID: "i-2", SymbolName: "payments"},
			},
			Edges: []framework.ForeignEdge{{FromID: "i-1", ToID: "i-2"}},
		},
	})
	first := Resource{Account: "acct-1", ID: "i-1"}
	second := Resource{Account: "acct-1", ID: "i-2"}
	if ctx.HasDependency(first, second) {
		t.Error("a pair in acct-1 was confirmed by an edge only acct-2 declared")
	}
	if ctx.HasDependency(second, first) {
		t.Error("the same leak in the reverse direction")
	}
	// THE CONTROL, same run: the graph that DID declare the edge answers for
	// its own pair, so the two zeros above are the scope rather than an oracle
	// that answers nothing.
	if !ctx.HasDependency(
		Resource{Account: "acct-2", ID: "i-1"}, Resource{Account: "acct-2", ID: "i-2"}) {
		t.Error("the declaring graph does not answer for its own pair")
	}
}

// TestTwoAccountsNeverDependOnEachOther is this module's own account rule,
// stated where its reason is. The common detector applies none and hands both
// resolved resources across whole; the declared edges are within-graph by the
// block's own shape, so a cross-account pair has no edge that could confirm it.
func TestTwoAccountsNeverDependOnEachOther(t *testing.T) {
	ctx := NewCloudContext([]framework.ForeignGraph{
		{
			GraphName: "acct-1",
			Nodes:     []framework.ForeignNode{{ID: "i-a", SymbolName: "checkout"}},
			Edges:     []framework.ForeignEdge{{FromID: "i-a", ToID: "i-a"}},
		},
		{
			GraphName: "acct-2",
			Nodes:     []framework.ForeignNode{{ID: "i-a", SymbolName: "payments"}},
		},
	})
	if ctx.HasDependency(
		Resource{Account: "acct-1", ID: "i-a"},
		Resource{Account: "acct-2", ID: "i-a"},
	) {
		t.Error("two same-named resources in different accounts answered as connected")
	}
	// The control, same ids, one account: the within-graph edge does answer.
	if !ctx.HasDependency(
		Resource{Account: "acct-1", ID: "i-a"},
		Resource{Account: "acct-1", ID: "i-a"},
	) {
		t.Error("the within-graph declared edge does not answer")
	}
}
