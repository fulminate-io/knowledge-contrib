// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// resolve_test.go — the rules that turn a declared cloud slice into
// resolutions, which are the input the proxy emitter consumes.

func streamsWithLabels(t *testing.T, sets ...map[string]string) []*Stream {
	t.Helper()
	entries := make([]Entry, 0, len(sets))
	for i, labels := range sets {
		entries = append(entries, Entry{
			Timestamp: at(time.Duration(i) * time.Second),
			Severity:  SeverityInfo,
			Message:   "disk pressure detected",
			Labels:    labels,
		})
	}
	streams, _ := buildStreams(entries, DefaultCardinalityThreshold)
	return streams
}

// resolutionsFor is the path the WALK takes: build the cloud context once, then
// resolve against it. The tests call it rather than a wrapper of their own,
// because a wrapper with no production caller is a surface the walk does not
// use and a suite that drives one is testing something nobody runs.
func resolutionsFor(streams []*Stream, cloud []framework.ForeignGraph) []Resolution {
	return ResolutionsFromContext(streams, NewCloudContext(cloud))
}

func cloudGraph(name string, nodes ...framework.ForeignNode) framework.ForeignGraph {
	return framework.ForeignGraph{GraphName: name, Nodes: nodes}
}

func resource(id, symbol string) framework.ForeignNode {
	return framework.ForeignNode{ID: id, Type: "ec2:instance", SymbolName: symbol}
}

// TestOnlyServiceIdentifyingLabelsResolve covers rule 2: a stream carries many
// labels and most of them name an instance, a container or a file. Only the four
// that name a SERVICE are looked up, because a cloud graph holds services.
func TestOnlyServiceIdentifyingLabelsResolve(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1",
		resource("i-service", "checkout"),
		resource("i-pod", "checkout-7b6f"),
		resource("i-container", "app"),
	)}

	for _, key := range []string{"service", "namespace", "deployment", "app"} {
		t.Run("resolves on "+key, func(t *testing.T) {
			got := resolutionsFor(streamsWithLabels(t, map[string]string{key: "checkout"}), cloud)
			if len(got) != 1 {
				t.Fatalf("resolutions = %v, want one on %q", got, key)
			}
			if got[0].LabelKey != key || got[0].Account != "acct-1" || got[0].ResourceID != "i-service" {
				t.Fatalf("resolution = %+v", got[0])
			}
		})
	}

	// A label that names a POD resolves nothing, even though a cloud node in the
	// slice carries that very SymbolName. This is the cell that separates "the
	// key is in the set" from "the value matched something".
	got := resolutionsFor(streamsWithLabels(t, map[string]string{"pod": "checkout-7b6f"}), cloud)
	if len(got) != 0 {
		t.Fatalf("a pod label resolved to %+v; only service-identifying keys are looked up", got)
	}
	// And so does a container label, whose value also matches a node.
	got = resolutionsFor(streamsWithLabels(t, map[string]string{"container": "app"}), cloud)
	if len(got) != 0 {
		t.Fatalf("a container label resolved to %+v", got)
	}
}

// TestAHighCardinalityLabelIsNotResolved covers rule 1. The label set below
// carries `app` on every stream, but with enough distinct values to cross the
// threshold it stops being a shared label and stops being resolved.
func TestAHighCardinalityLabelIsNotResolved(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1", resource("i-1", "svc-0"))}

	low := streamsWithLabels(t, map[string]string{"app": "svc-0"}, map[string]string{"app": "svc-0"})
	if got := resolutionsFor(low, cloud); len(got) != 1 {
		t.Fatalf("a low-cardinality app label resolved %d times, want 1", len(got))
	}

	// Three distinct values against a threshold of 2 makes `app` high
	// cardinality, so it is inline on the stream rather than shared, and no
	// proxy is built for it.
	entries := []Entry{}
	for i := range 3 {
		entries = append(entries, Entry{
			Timestamp: at(time.Duration(i) * time.Second),
			Severity:  SeverityInfo,
			Message:   "disk pressure detected",
			Labels:    map[string]string{"app": "svc-" + string(rune('0'+i))},
		})
	}
	high, _ := buildStreams(entries, 2)
	if got := resolutionsFor(high, cloud); len(got) != 0 {
		t.Fatalf("a high-cardinality app label resolved to %+v", got)
	}
}

// TestTwoStreamsNamingOneServiceResolveOnce covers rule 3, which is what makes
// one proxy node stand for one resource however many streams point at it.
func TestTwoStreamsNamingOneServiceResolveOnce(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1", resource("i-1", "checkout"))}
	got := resolutionsFor(streamsWithLabels(t,
		map[string]string{"app": "checkout", "zone": "a"},
		map[string]string{"app": "checkout", "zone": "b"},
	), cloud)
	if len(got) != 1 {
		t.Fatalf("resolutions = %v, want one for two streams naming one service", got)
	}
}

// TestTheFirstDeclaredGraphCarryingTheNodeWins covers rule 4's precedence, and
// with it the account a proxy is stamped with.
//
// THE FIXTURE'S DECLARATION ORDER AND ITS ALPHABETICAL ORDER DISAGREE, which is
// the whole point: a fixture whose two orders coincide passes under either rule
// and discriminates nothing. Here the first-declared graph sorts LAST, so a
// resolver that ordered its candidates by name would pick the other one.
func TestTheFirstDeclaredGraphCarryingTheNodeWins(t *testing.T) {
	cloud := []framework.ForeignGraph{
		cloudGraph("z-acct-declared-first", resource("i-first", "checkout")),
		cloudGraph("a-acct-declared-second", resource("i-second", "checkout")),
	}
	got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "checkout"}), cloud)
	if len(got) != 1 {
		t.Fatalf("resolutions = %v, want one", got)
	}
	if got[0].Account != "z-acct-declared-first" || got[0].ResourceID != "i-first" {
		t.Fatalf("resolution = %+v, want the FIRST DECLARED graph (%q), not the first by name",
			got[0], "z-acct-declared-first")
	}

	// THE CONTROL, the same two graphs declared the other way round: the winner
	// follows the DECLARATION and not the names, so it changes.
	swapped := []framework.ForeignGraph{cloud[1], cloud[0]}
	got = resolutionsFor(streamsWithLabels(t, map[string]string{"app": "checkout"}), swapped)
	if len(got) != 1 || got[0].Account != "a-acct-declared-second" {
		t.Fatalf("resolutions = %+v, want the newly-first-declared graph; the winner does not follow the declaration", got)
	}
}

// TestTheFirstMatchingNodeWithinAGraphWins is the same precedence one level
// down: two nodes of ONE declared graph carrying the name resolve to the one
// declared first, again with a fixture whose two orders disagree.
func TestTheFirstMatchingNodeWithinAGraphWins(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1",
		resource("i-z-declared-first", "checkout"),
		resource("i-a-declared-second", "checkout"),
	)}
	got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "checkout"}), cloud)
	if len(got) != 1 {
		t.Fatalf("resolutions = %v, want one", got)
	}
	if got[0].ResourceID != "i-z-declared-first" {
		t.Fatalf("resolution = %+v, want the first DECLARED node, not the first by id", got[0])
	}
}

// TestANodeMatchesOnItsSymbolNameOrEitherMetadataName covers rule 4's match
// sources. A block declaring `metadata` and not `symbol_name` is a legitimate
// declaration — the entry decides which node fields the client fetches — and a
// symbol-only rule would resolve nothing at all against it.
func TestANodeMatchesOnItsSymbolNameOrEitherMetadataName(t *testing.T) {
	cases := []struct {
		name string
		node framework.ForeignNode
	}{
		{"symbol name", framework.ForeignNode{ID: "i-1", SymbolName: "checkout"}},
		{"metadata name", framework.ForeignNode{ID: "i-1", Metadata: map[string]string{"name": "checkout"}}},
		{"metadata service", framework.ForeignNode{ID: "i-1", Metadata: map[string]string{"service": "checkout"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolutionsFor(
				streamsWithLabels(t, map[string]string{"app": "checkout"}),
				[]framework.ForeignGraph{cloudGraph("acct-1", tc.node)})
			if len(got) != 1 {
				t.Fatalf("a node matching on its %s resolved %d times, want 1", tc.name, len(got))
			}
			if got[0].ResourceID != "i-1" {
				t.Fatalf("resolution = %+v", got[0])
			}
		})
	}

	// THE SAME-RUN CONTROL: a node carrying NONE of the three under that name
	// does not resolve, so the three cells above are about the match rather
	// than about every declared node resolving.
	got := resolutionsFor(
		streamsWithLabels(t, map[string]string{"app": "checkout"}),
		[]framework.ForeignGraph{cloudGraph("acct-1", framework.ForeignNode{
			ID: "i-1", SymbolName: "payments", Metadata: map[string]string{"name": "payments", "service": "payments"},
		})})
	if len(got) != 0 {
		t.Fatalf("a node matching on none of the three resolved to %+v", got)
	}

	// AND THE MATCH IS EXACT, never a prefix or a substring: a resource named
	// checkout-service is not the service named checkout.
	got = resolutionsFor(
		streamsWithLabels(t, map[string]string{"app": "checkout"}),
		[]framework.ForeignGraph{cloudGraph("acct-1", resource("i-1", "checkout-service"))})
	if len(got) != 0 {
		t.Fatalf("a node whose name merely contains the label resolved to %+v", got)
	}
}

// TestTheMatchSourcesArePreferredInOrder pins rule 4's precedence among the
// three, on ONE node carrying a different name in each: the symbol wins, then
// metadata name, then metadata service.
func TestTheMatchSourcesArePreferredInOrder(t *testing.T) {
	node := framework.ForeignNode{
		ID: "i-1", SymbolName: "by-symbol",
		Metadata: map[string]string{"name": "by-name", "service": "by-service"},
	}
	cloud := []framework.ForeignGraph{cloudGraph("acct-1", node)}
	for _, value := range []string{"by-symbol", "by-name", "by-service"} {
		got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": value}), cloud)
		if len(got) != 1 {
			t.Fatalf("the label %q resolved %d times, want 1; all three names reach one node", value, len(got))
		}
	}
	// A node matching on more than one source is ONE candidate, not several, so
	// two graphs each matching once still resolve to the first declared.
	two := []framework.ForeignGraph{
		cloudGraph("z-first", node),
		cloudGraph("a-second", framework.ForeignNode{ID: "i-2", SymbolName: "by-symbol"}),
	}
	got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "by-symbol"}), two)
	if len(got) != 1 || got[0].Account != "z-first" {
		t.Fatalf("resolutions = %+v, want one against the first declared graph", got)
	}
}

// TestALabelMatchingNothingIsSkipped covers rule 5, and the empty-slice arm
// beside it: neither is an error, and both leave the base graph untouched.
func TestALabelMatchingNothingIsSkipped(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1", resource("i-1", "payments"))}
	if got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "checkout"}), cloud); len(got) != 0 {
		t.Fatalf("an unmatched label resolved to %+v", got)
	}
	// THE CONTROL: the same slice with a matching label does resolve, so the
	// zero above is a property of the value rather than of the fixture.
	if got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "payments"}), cloud); len(got) != 1 {
		t.Fatalf("the matching control resolved %d times, want 1", len(got))
	}

	// No cloud family declared at all, which is what a collector whose entry
	// declares nothing receives.
	if got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "payments"}), nil); got != nil {
		t.Fatalf("an empty declared slice resolved to %+v", got)
	}
}

// TestANodeWithNoIDIsSkipped covers the narrow-declaration arm: an entry that
// declared symbol_name and not id yields nodes no proxy can be built from, and a
// proxy id with an empty resource is a node nothing can join to.
func TestANodeWithNoIDIsSkipped(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1",
		framework.ForeignNode{SymbolName: "checkout"},
		resource("i-real", "payments"),
	)}
	if got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "checkout"}), cloud); len(got) != 0 {
		t.Fatalf("a node with no id resolved to %+v", got)
	}
	if got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "payments"}), cloud); len(got) != 1 {
		t.Fatalf("the control node with an id resolved %d times, want 1", len(got))
	}
}

// TestAGraphWithNoNameIsSkipped covers the other half of the same narrowing: the
// account is the graph's name, so a slice arriving without one cannot stamp a
// proxy.
func TestAGraphWithNoNameIsSkipped(t *testing.T) {
	cloud := []framework.ForeignGraph{
		{Nodes: []framework.ForeignNode{resource("i-1", "checkout")}},
		cloudGraph("acct-1", resource("i-2", "checkout")),
	}
	got := resolutionsFor(streamsWithLabels(t, map[string]string{"app": "checkout"}), cloud)
	if len(got) != 1 || got[0].Account != "acct-1" || got[0].ResourceID != "i-2" {
		t.Fatalf("resolutions = %+v, want the named graph's node", got)
	}
}

// TestResolutionOrderIsDeterministic is the divergence from the built-in path
// stated as an assertion. Its equivalent walks each stream's label MAP, whose
// iteration Go randomizes, so the same input yields the resolutions — and the
// EMITTED_BY edges built from them — in a different order on different runs.
func TestResolutionOrderIsDeterministic(t *testing.T) {
	cloud := []framework.ForeignGraph{cloudGraph("acct-1",
		resource("i-app", "checkout"),
		resource("i-ns", "prod"),
		resource("i-dep", "web"),
		resource("i-svc", "api"),
	)}
	// One stream carrying all four service-identifying keys, so the order the
	// keys are read in is the only thing that can vary.
	streams := streamsWithLabels(t, map[string]string{
		"app": "checkout", "namespace": "prod", "deployment": "web", "service": "api",
	})

	first := resolutionsFor(streams, cloud)
	if len(first) != 4 {
		t.Fatalf("resolutions = %v, want all four keys", first)
	}
	for run := range 40 {
		got := resolutionsFor(streams, cloud)
		if len(got) != len(first) {
			t.Fatalf("run %d resolved %d, want %d", run, len(got), len(first))
		}
		for i := range first {
			if got[i] != first[i] {
				t.Fatalf("run %d resolution %d = %+v, want %+v", run, i, got[i], first[i])
			}
		}
	}
	// Sorted by key, which is what the determinism rests on.
	want := []string{"app", "deployment", "namespace", "service"}
	for i, key := range want {
		if first[i].LabelKey != key {
			t.Fatalf("resolution %d is on %q, want %q; the keys are read in sorted order", i, first[i].LabelKey, key)
		}
	}
}

// TestResolutionsReachTheProxyEmitter joins the two halves in one package: the
// resolutions this file derives are the input proxy.go emits from.
func TestResolutionsReachTheProxyEmitter(t *testing.T) {
	entries := []Entry{{
		Timestamp: at(0), Severity: SeverityInfo, Message: "disk pressure detected",
		Labels: map[string]string{"app": "checkout"},
	}}
	graph, err := Build(entries, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	cloud := []framework.ForeignGraph{cloudGraph("acct-1", resource("i-0abc123", "checkout"))}

	nodes, edges, err := Emit(graph, resolutionsFor(graph.Streams, cloud), nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if n := countByType(nodes, NodeProxy); n != 1 {
		t.Fatalf("proxy nodes = %d, want 1", n)
	}
	for _, n := range nodes {
		if n.Type != NodeProxy {
			continue
		}
		if n.ID != "proxy:cloud:acct-1:i-0abc123" {
			t.Fatalf("proxy id = %q", n.ID)
		}
	}
	emitted := 0
	for _, e := range edges {
		if e.Type != EdgeEmittedBy {
			continue
		}
		emitted++
		if e.FromID != LabelNodeID("app", "checkout") {
			t.Fatalf("EMITTED_BY from %q", e.FromID)
		}
		// THE EDGE IS OWN-GRAPH: both endpoints are ids in this collect's own
		// result, so it carries no target graph. A cross-graph endpoint is a
		// different shape and this family is not one.
		if e.TargetGraph != "" {
			t.Fatalf("EMITTED_BY carries TargetGraph %q; the proxy is a node of this graph", e.TargetGraph)
		}
	}
	if emitted != 1 {
		t.Fatalf("EMITTED_BY edges = %d, want 1", emitted)
	}
}
