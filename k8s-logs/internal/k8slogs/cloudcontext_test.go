// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"context"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// cloudcontext_test.go — the cloud-facing families, computed from a SUPPLIED
// slice, with the zero arm as their control.

// cloudFixtureFamily is the registered graph type these fixtures declare their
// provider slices under, and it is the same name the sibling fixtures already
// pass at the map's key position.
//
// A FIXTURE HAS TO PICK A FAMILY NOW. The framework block used to be a struct
// with a `Cloud` field, so a fixture said "cloud" by construction and could not
// be wrong about it. Under the map-keyed contract the KEY is the family, there
// is no built-in cloud type to key on, and a fixture picks one the way an
// operator would — the name a contrib collector registers itself under. This
// module's read side takes EVERY declared family but code, so the specific name
// is never something the production path checks for.
const cloudFixtureFamily = "aws"

func cloudFixture() CloudContext {
	return CloudContext{Cloud: []framework.ForeignGraph{{
		GraphName: "fulminate-services",
		Nodes: []framework.ForeignNode{
			{ID: "ns/dev", Type: "k8s_namespace", Metadata: map[string]string{
				"namespace": "dev", "cluster_name": "main", "resource_type": "k8s_namespace",
				"region": "us-central1", "provider": "gcp",
			}},
			{ID: "ns/other", Type: "k8s_namespace", Metadata: map[string]string{
				"namespace": "other", "cluster_name": "main",
			}},
		},
		Edges: []framework.ForeignEdge{{FromID: "ns/dev", ToID: "ns/other"}},
	}}}
}

func devStreams() []*logpipe.Stream {
	return []*logpipe.Stream{{Labels: StreamLabels("dev", "api-1", "api", "main", "fulminate-services")}}
}

// TestResolutionsMatchOnTheNamespace, with an unmatched cloud node in the same
// fixture as the control.
func TestResolutionsMatchOnTheNamespace(t *testing.T) {
	got := cloudFixture().Resolutions(devStreams())
	if len(got) != 1 {
		t.Fatalf("%d resolutions from a fixture holding one matching and one unmatched cloud node, want 1: %+v",
			len(got), got)
	}
	r := got[0]
	if r.LabelKey != LabelNamespace || r.LabelValue != "dev" {
		t.Errorf("the resolution names %s=%s, want %s=dev", r.LabelKey, r.LabelValue, LabelNamespace)
	}
	if r.Account != "fulminate-services" || r.ResourceID != "ns/dev" {
		t.Errorf("the resolution names %s/%s", r.Account, r.ResourceID)
	}
	for name, pair := range map[string][2]string{
		"resource type": {r.ResourceType, "k8s_namespace"},
		"region":        {r.Region, "us-central1"},
		"provider":      {r.Provider, "gcp"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("the resolution's %s is %q, want %q; a proxy carries the display metadata so a reader "+
				"renders it without resolving back into the cloud graph", name, pair[0], pair[1])
		}
	}
}

// TestAnEmptyContextResolvesNothing is the control arm.
func TestAnEmptyContextResolvesNothing(t *testing.T) {
	var empty CloudContext
	if !empty.IsEmpty() {
		t.Fatal("a context with no graphs did not report itself empty")
	}
	if got := empty.Resolutions(devStreams()); len(got) != 0 {
		t.Fatalf("%d resolutions from an empty cloud context", len(got))
	}
	if got := empty.Correlation(devStreams()); got != nil {
		t.Fatal("an empty cloud context produced a resolver; with none supplied nothing resolves and " +
			"nothing is confirmed")
	}
	nodesOnly := CloudContext{Cloud: []framework.ForeignGraph{{GraphName: "acct"}}}
	if !nodesOnly.IsEmpty() {
		t.Fatal("a graph carrying no nodes did not report itself empty")
	}
}

// TestTheClusterMustAgreeWhereBothSidesKnowIt — two clusters in one account can
// hold a namespace of the same name.
func TestTheClusterMustAgreeWhereBothSidesKnowIt(t *testing.T) {
	other := []*logpipe.Stream{{Labels: StreamLabels("dev", "api-1", "api", "staging-cluster", "fulminate-services")}}
	if got := cloudFixture().Resolutions(other); len(got) != 0 {
		t.Fatalf("%d resolutions when the stream's cluster and the cloud node's disagree: %+v", len(got), got)
	}

	// A cloud node carrying NO cluster matches on the namespace alone, which is
	// the right answer for a cloud graph that does not model clusters.
	unclustered := CloudContext{Cloud: []framework.ForeignGraph{{
		GraphName: "acct",
		Nodes:     []framework.ForeignNode{{ID: "r1", Metadata: map[string]string{"namespace": "dev"}}},
	}}}
	if got := unclustered.Resolutions(other); len(got) != 1 {
		t.Fatalf("%d resolutions against a cloud node that models no cluster, want 1", len(got))
	}
}

// TestANamespaceResolvingToSeveralResourcesIsDroppedFromCorrelation — picking
// one arbitrarily would make the edges a function of the slice's order.
func TestANamespaceResolvingToSeveralResourcesIsDroppedFromCorrelation(t *testing.T) {
	ambiguous := CloudContext{Cloud: []framework.ForeignGraph{{
		GraphName: "acct",
		Nodes: []framework.ForeignNode{
			{ID: "r1", Metadata: map[string]string{"namespace": "dev"}},
			{ID: "r2", Metadata: map[string]string{"namespace": "dev"}},
		},
	}}}
	if got := ambiguous.ResourcesByService(devStreams()); len(got) != 0 {
		t.Fatalf("an ambiguous namespace resolved to %+v; asserting a dependency on the wrong resource is "+
			"worse than asserting none", got)
	}
	if got := len(ambiguous.Resolutions(devStreams())); got != 2 {
		t.Fatalf("%d proxy resolutions for two matching resources, want 2; ambiguity drops the CORRELATION "+
			"input, not the proxies", got)
	}
}

// TestTheOracleAnswersFromTheSuppliedEdges, in both directions and with the
// account rule this module keeps rather than the detector.
func TestTheOracleAnswersFromTheSuppliedEdges(t *testing.T) {
	oracle := cloudFixture().Correlation(devStreams())
	if oracle == nil {
		t.Fatal("a fixture carrying one cloud edge produced no oracle")
	}
	dev := correlation.ResolvedResource{Account: "fulminate-services", ID: "ns/dev"}
	other := correlation.ResolvedResource{Account: "fulminate-services", ID: "ns/other"}

	if !oracle.HasDependency(dev, other) {
		t.Fatal("the supplied edge is not reported as a dependency")
	}
	if !oracle.HasDependency(other, dev) {
		t.Fatal("the supplied edge is directional; a log correlation says two services' failures are " +
			"related, which does not depend on which way the dependency points")
	}
	if oracle.HasDependency(dev, correlation.ResolvedResource{Account: "fulminate-services", ID: "ns/absent"}) {
		t.Fatal("an unsupplied pair was reported as a dependency")
	}

	// THE ACCOUNT RULE IS THIS MODULE'S, NOT THE DETECTOR'S. The common detector
	// hands both resources across whole and applies no account rule; this
	// module's declared edges join two ids INSIDE one graph, so a pair spanning
	// two accounts is one the declared slice cannot have asserted.
	crossAccount := correlation.ResolvedResource{Account: "another-account", ID: "ns/other"}
	if oracle.HasDependency(dev, crossAccount) {
		t.Fatal("a pair spanning two accounts was confirmed; the declared edges join two ids inside ONE " +
			"graph, so no declared edge can have asserted it")
	}
}

// TestAWalkWithACloudContextEmitsProxiesAndEmittedBy is the with-cloud arm,
// paired with the without-cloud control in the same test so the zero is a
// measurement rather than an absence.
func TestAWalkWithACloudContextEmitsProxiesAndEmittedBy(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "connection to database failed for alpha"))
	client := api.client(t)

	// The two arms differ in ONE input: the cloud context. Everything else,
	// including the kubecontext name that decides the cluster labels, is held
	// identical, or the node-count comparison below would measure the labels
	// rather than the proxy.
	params := devParams()
	params.Context = "gke_fulminate-services_us-central1_main"

	without := &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
	control, err := without.Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	if n := countType(control.Nodes, logpipe.NodeProxy); n != 0 {
		t.Fatalf("%d proxy nodes with NO cloud context supplied; the built-in pipeline emits none either, "+
			"so zero here is parity rather than a gap", n)
	}
	if n := countEdge(control.Edges, logpipe.EdgeEmittedBy); n != 0 {
		t.Fatalf("%d EMITTED_BY edges with no cloud context supplied", n)
	}

	with := &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
	res, err := with.Walk(context.Background(), "id", params, framework.ForeignContext{"aws": cloudFixture().Cloud})
	if err != nil {
		t.Fatal(err)
	}
	if n := countType(res.Nodes, logpipe.NodeProxy); n != 1 {
		t.Fatalf("%d proxy nodes with a cloud context supplied, want 1", n)
	}
	if n := countEdge(res.Edges, logpipe.EdgeEmittedBy); n != 1 {
		t.Fatalf("%d EMITTED_BY edges with a cloud context supplied, want 1", n)
	}
	if len(res.Nodes) != len(control.Nodes)+1 {
		t.Fatalf("the cloud context added %d nodes, want exactly the one proxy", len(res.Nodes)-len(control.Nodes))
	}
	for _, e := range res.Edges {
		if e.Type != logpipe.EdgeEmittedBy {
			continue
		}
		if e.FromID != "log-label:namespace=dev" {
			t.Errorf("EMITTED_BY runs from %q, want the LABEL node the walk emitted", e.FromID)
		}
		if e.ToID != "proxy:cloud:fulminate-services:ns/dev" {
			t.Errorf("EMITTED_BY runs to %q", e.ToID)
		}
	}
}

func countType(nodes []framework.Node, nodeType string) int {
	n := 0
	for _, node := range nodes {
		if node.Type == nodeType {
			n++
		}
	}
	return n
}

func countEdge(edges []framework.Edge, edgeType string) int {
	n := 0
	for _, e := range edges {
		if e.Type == edgeType {
			n++
		}
	}
	return n
}

// TestAWalkWithACloudContextEmitsAConfirmedCorrelation is the third family's
// end-to-end arm, with the unconfirmed run beside it as the control.
//
// The fixture needs two namespaces whose error templates overlap in time, each
// resolving to a cloud resource, and a supplied dependency joining the two
// resources. Remove the dependency and the pair is a candidate and no edge.
func TestAWalkWithACloudContextEmitsAConfirmedCorrelation(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addPod("data", "db-1", nil, nil, []string{"db"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "[ERROR] upstream call to the store timed out"))
	api.addLog("data", "db-1", "db", stamped(fixtureBase, "[ERROR] refused a connection past the pool limit"))
	client := api.client(t)

	params := devParams()
	params.Namespaces = []string{"dev", "data"}

	graph := func(edges []framework.ForeignEdge) CloudContext {
		return CloudContext{Cloud: []framework.ForeignGraph{{
			GraphName: "acct",
			Nodes: []framework.ForeignNode{
				{ID: "r-api", Metadata: map[string]string{"namespace": "dev"}},
				{ID: "r-db", Metadata: map[string]string{"namespace": "data"}},
			},
			Edges: edges,
		}}}
	}

	unconfirmed := &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
	control, err := unconfirmed.Walk(context.Background(), "id", params, framework.ForeignContext{"aws": graph(nil).Cloud})
	if err != nil {
		t.Fatal(err)
	}
	if n := countEdge(control.Edges, logpipe.EdgeCorrelatesWith); n != 0 {
		t.Fatalf("%d CORRELATES_WITH edges with no dependency supplied; an overlapping pair is a CANDIDATE, "+
			"and a temporal coincidence is not a fact the graph carries", n)
	}

	confirmed := &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
	res, err := confirmed.Walk(context.Background(), "id", params,
		framework.ForeignContext{"aws": graph([]framework.ForeignEdge{{FromID: "r-api", ToID: "r-db"}}).Cloud})
	if err != nil {
		t.Fatal(err)
	}
	if n := countEdge(res.Edges, logpipe.EdgeCorrelatesWith); n != 1 {
		t.Fatalf("%d CORRELATES_WITH edges with the dependency supplied, want 1", n)
	}
	for _, e := range res.Edges {
		if e.Type != logpipe.EdgeCorrelatesWith {
			continue
		}
		if e.Method != "temporal+cloud-dependency" {
			t.Errorf("the edge's method is %q", e.Method)
		}
		if !strings.Contains(e.Evidence, "services=") {
			t.Errorf("the edge's evidence does not name the services: %q", e.Evidence)
		}
	}
}

// TestAnEdgeDeclaredInOneGraphDoesNotConfirmAPairFromAnother — the account rule
// this module keeps has to hold on EVERY route to its own conclusion.
//
// The oracle's own comment says a pair the declared slice did not assert is
// refused here. Comparing the two resources' ACCOUNTS is not enough to make that
// true: with the edge set flattened across every declared graph, an edge
// declared by one account confirms a same-account pair resolved out of another,
// and the module emits a CORRELATES_WITH edge no declared graph carried. The
// graph that declared an edge has to be part of what the lookup asks for.
func TestAnEdgeDeclaredInOneGraphDoesNotConfirmAPairFromAnother(t *testing.T) {
	twoGraphs := CloudContext{Cloud: []framework.ForeignGraph{
		{
			GraphName: "acctA",
			Nodes: []framework.ForeignNode{
				{ID: "ns/dev", Metadata: map[string]string{"namespace": "dev"}},
				{ID: "ns/other", Metadata: map[string]string{"namespace": "other"}},
			},
			Edges: []framework.ForeignEdge{},
		},
		{
			GraphName: "acctB",
			Nodes:     []framework.ForeignNode{{ID: "unrelated", Metadata: map[string]string{"namespace": "zzz"}}},
			Edges:     []framework.ForeignEdge{{FromID: "ns/dev", ToID: "ns/other"}},
		},
	}}
	streams := []*logpipe.Stream{
		{Labels: StreamLabels("dev", "api-1", "api", "", "")},
		{Labels: StreamLabels("other", "api-2", "api", "", "")},
	}

	oracle := twoGraphs.Correlation(streams)
	if oracle == nil {
		t.Fatal("a fixture carrying an edge produced no oracle")
	}
	if oracle.HasDependency(
		correlation.ResolvedResource{Account: "acctA", ID: "ns/dev"},
		correlation.ResolvedResource{Account: "acctA", ID: "ns/other"},
	) {
		t.Fatal("a pair resolved out of acctA was confirmed by the ONLY declared edge, which graph acctB " +
			"declared. No graph of acctA asserted that dependency, so the module would emit a " +
			"CORRELATES_WITH edge its declared slice never carried")
	}

	// THE CONTROL, in the same run: move the same edge into acctA and the same
	// pair IS confirmed, so the refusal above is about which graph declared it
	// rather than about the fixture.
	sameGraph := CloudContext{Cloud: []framework.ForeignGraph{{
		GraphName: "acctA",
		Nodes: []framework.ForeignNode{
			{ID: "ns/dev", Metadata: map[string]string{"namespace": "dev"}},
			{ID: "ns/other", Metadata: map[string]string{"namespace": "other"}},
		},
		Edges: []framework.ForeignEdge{{FromID: "ns/dev", ToID: "ns/other"}},
	}}}
	if !sameGraph.Correlation(streams).HasDependency(
		correlation.ResolvedResource{Account: "acctA", ID: "ns/dev"},
		correlation.ResolvedResource{Account: "acctA", ID: "ns/other"},
	) {
		t.Fatal("the same pair is not confirmed when its own graph declares the edge")
	}
}

// TestTheOracleRefusesAnEmptyResourceID — a resource with no id names nothing,
// and an empty id would key a lookup every other empty id resolves against.
//
// It is unreachable from the walk, which skips a cloud node with no id before a
// resolution is built, so a direct call is the only venue that reaches it.
func TestTheOracleRefusesAnEmptyResourceID(t *testing.T) {
	oracle := cloudFixture().Correlation(devStreams())
	if oracle == nil {
		t.Fatal("the fixture produced no oracle")
	}
	real := correlation.ResolvedResource{Account: "fulminate-services", ID: "ns/dev"}
	for _, tc := range []struct {
		name string
		a, b correlation.ResolvedResource
	}{
		{"empty on the left", correlation.ResolvedResource{Account: "fulminate-services"}, real},
		{"empty on the right", real, correlation.ResolvedResource{Account: "fulminate-services"}},
		{"empty on both", correlation.ResolvedResource{}, correlation.ResolvedResource{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if oracle.HasDependency(tc.a, tc.b) {
				t.Fatal("a resource with no id was reported as a dependency endpoint")
			}
		})
	}
}

// TestACloudSliceThatResolvesNothingLeavesTheDetectorUnreached — the untyped-nil
// guard, driven through the walk it protects.
//
// WHAT IT GUARDS. Correlation returns a TYPED nil pointer when the declared
// slice answers neither question. Assigning that into the two interfaces makes
// them non-nil, so the detector calls through them and dereferences a nil
// receiver. The guard assigns only a non-nil resolver, which leaves the
// interfaces genuinely nil and the detector leaving every candidate unconfirmed.
//
// REACHING IT NEEDS BOTH HALVES: a cloud slice whose nodes match no stream's
// namespace and carry no edges, AND error templates in two services for the
// detector to have a candidate to resolve. With either missing the detector
// never calls the resolver and the guard is never exercised.
func TestACloudSliceThatResolvesNothingLeavesTheDetectorUnreached(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addPod("data", "db-1", nil, nil, []string{"db"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "[ERROR] upstream call to the store timed out"))
	api.addLog("data", "db-1", "db", stamped(fixtureBase, "[ERROR] refused a connection past the pool limit"))
	client := api.client(t)

	// A DECLARED SLICE THAT ANSWERS NEITHER QUESTION: its one node's namespace
	// matches no stream, and it carries no edges.
	unmatched := framework.ForeignContext{cloudFixtureFamily: []framework.ForeignGraph{{
		GraphName: "acct",
		Nodes:     []framework.ForeignNode{{ID: "r-zzz", Metadata: map[string]string{"namespace": "zzz"}}},
		Edges:     []framework.ForeignEdge{},
	}}}

	c := &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
	params := devParams()
	params.Namespaces = []string{"dev", "data"}

	res, err := c.Walk(context.Background(), "id", params, unmatched)
	if err != nil {
		t.Fatalf("a collect whose declared cloud slice resolves nothing failed: %v. A typed nil resolver "+
			"assigned into the detector's interfaces is not nil, so the detector calls through it", err)
	}
	if n := countEdge(res.Edges, logpipe.EdgeCorrelatesWith); n != 0 {
		t.Fatalf("%d CORRELATES_WITH edges from a slice that resolved nothing", n)
	}
	if n := countType(res.Nodes, logpipe.NodeProxy); n != 0 {
		t.Fatalf("%d proxy nodes from a slice whose nodes match no stream", n)
	}

	// THE CONTROL: the same two services, the same walk, with a slice that DOES
	// resolve both and declares the edge. If the fixture could not produce a
	// candidate at all, the arm above would pass whatever the guard did.
	matched := framework.ForeignContext{cloudFixtureFamily: []framework.ForeignGraph{{
		GraphName: "acct",
		Nodes: []framework.ForeignNode{
			{ID: "r-api", Metadata: map[string]string{"namespace": "dev"}},
			{ID: "r-db", Metadata: map[string]string{"namespace": "data"}},
		},
		Edges: []framework.ForeignEdge{{FromID: "r-api", ToID: "r-db"}},
	}}}
	confirmed, err := c.Walk(context.Background(), "id", params, matched)
	if err != nil {
		t.Fatal(err)
	}
	if n := countEdge(confirmed.Edges, logpipe.EdgeCorrelatesWith); n != 1 {
		t.Fatalf("%d CORRELATES_WITH edges from the control, want 1; the fixture produces no candidate and "+
			"the arm above proves nothing", n)
	}
}
