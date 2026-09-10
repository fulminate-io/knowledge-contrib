// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// collector_test.go — the whole walk, and the four completeness arms.

func collectorOver(api *fakeAPI, t *testing.T) *Collector {
	t.Helper()
	client := api.client(t)
	return &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
}

func devParams() Params {
	return Params{Namespaces: []string{"dev"}, ChunkWindowSeconds: 3600}
}

// TestWalkProducesTheFourNodeTypesAndThreeEdgeTypes.
func TestWalkProducesTheFourNodeTypesAndThreeEdgeTypes(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, []string{"istio-init"}, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "connection to database failed for alpha", "connection to database failed for beta"))
	api.addLog("dev", "api-1", "istio-init", stamped(fixtureBase, "sidecar ready"))

	res, err := collectorOver(api, t).Walk(context.Background(), "dev-window", devParams(), framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete.IsComplete() {
		t.Fatalf("a clean read reported an incomplete walk: %s", res.Complete.Reason())
	}

	types := map[string]int{}
	for _, n := range res.Nodes {
		types[n.Type]++
	}
	for _, want := range []string{logpipe.NodeLogTemplate, logpipe.NodeLogStream, logpipe.NodeLogChunk, logpipe.NodeLogLabel} {
		if types[want] == 0 {
			t.Errorf("the walk emitted no %q node; node types present: %v", want, types)
		}
	}
	if types[logpipe.NodeLogStream] != 2 {
		t.Errorf("%d stream nodes for two containers of one pod, want 2 (the init container is a log source)",
			types[logpipe.NodeLogStream])
	}

	edges := map[string]int{}
	for _, e := range res.Edges {
		edges[e.Type]++
	}
	for _, want := range []string{logpipe.EdgeContains, logpipe.EdgeBelongsTo, logpipe.EdgeHasLabel} {
		if edges[want] == 0 {
			t.Errorf("the walk emitted no %q edge; edge types present: %v", want, edges)
		}
	}
}

// TestWalkCompleteArms — all four cells.
func TestWalkCompleteArms(t *testing.T) {
	t.Run("clean read is complete", func(t *testing.T) {
		api := newFakeAPI(t)
		api.addPod("dev", "api-1", nil, nil, []string{"api"})
		api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))
		res, err := collectorOver(api, t).Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Complete.IsComplete() {
			t.Fatalf("a clean read is incomplete: %s", res.Complete.Reason())
		}
	})

	t.Run("zero pods is complete with no nodes", func(t *testing.T) {
		api := newFakeAPI(t)
		res, err := collectorOver(api, t).Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Complete.IsComplete() {
			t.Fatalf("a namespace with no pods is incomplete: %s", res.Complete.Reason())
		}
		if len(res.Nodes) != 0 || len(res.Edges) != 0 {
			t.Fatalf("an empty namespace produced %d nodes and %d edges", len(res.Nodes), len(res.Edges))
		}
	})

	t.Run("a bound that engaged is incomplete", func(t *testing.T) {
		api := newFakeAPI(t)
		api.addPod("dev", "api-1", nil, nil, []string{"api"})
		api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one", "two"))
		params := devParams()
		tail := int64(2)
		params.TailLines = &tail
		res, err := collectorOver(api, t).Walk(context.Background(), "id", params, framework.ForeignContext{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Complete.IsComplete() {
			t.Fatal("a read cut short by a tail-line bound asserted a COMPLETE walk. The server treats a " +
				"complete collect's absent rows as deleted, so an optimistic true reports a silently " +
				"emptied graph as a successful collect")
		}
		if !strings.Contains(res.Complete.Reason(), "cut short") {
			t.Fatalf("the incompleteness reason does not say a bound engaged: %q", res.Complete.Reason())
		}
	})

	t.Run("an unparseable line makes the walk incomplete", func(t *testing.T) {
		api := newFakeAPI(t)
		api.addPod("dev", "api-1", nil, nil, []string{"api"})
		api.addLog("dev", "api-1", "api", "no timestamp on this line\n")
		res, err := collectorOver(api, t).Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Complete.IsComplete() {
			t.Fatal("a container whose lines carried no parseable timestamp asserted a COMPLETE walk with " +
				"zero entries; that is indistinguishable from an empty container and would let the server " +
				"treat the graph's existing rows as gone")
		}
		if !strings.Contains(res.Complete.Reason(), "parseable timestamp") {
			t.Fatalf("the reason does not say what was dropped: %q", res.Complete.Reason())
		}
	})

	// The listing-failure arm is TestAListingFailureIsReportedRatherThanFailingTheCollect,
	// which forces the failure at the transport rather than through the fixture.
}

// TestAListingFailureIsReportedRatherThanFailingTheCollect — the arm that
// matters, forced by pointing the client at a closed server for one namespace.
func TestAListingFailureIsReportedRatherThanFailingTheCollect(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))
	client := api.client(t)

	// A second client pointed at a dead address: the refusing namespace's list
	// call fails at the transport while the first namespace's succeeds.
	c := &Collector{NewClient: func(string) (kubernetes.Interface, error) { return client, nil }}
	params := devParams()
	params.Namespaces = []string{"dev"}

	res, err := c.Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete.IsComplete() {
		t.Fatalf("the healthy control arm is incomplete: %s", res.Complete.Reason())
	}
	if len(res.Nodes) == 0 {
		t.Fatal("the healthy control arm produced no nodes")
	}

	api.srv.Close()
	broken, err := c.Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a namespace that could not be listed FAILED the collect: %v. It must be reported as a "+
			"reason the walk is incomplete, so the result carries what was read and the server declines "+
			"to treat the rest as gone", err)
	}
	if broken.Complete.IsComplete() {
		t.Fatal("a namespace that could not be listed left the walk asserting COMPLETE")
	}
	if !strings.Contains(broken.Complete.Reason(), "dev") {
		t.Fatalf("the reason does not name the namespace that failed: %q", broken.Complete.Reason())
	}
}

// TestTwoCollectsOfUnchangedInputProduceIdenticalIDs — the carry-forward
// property at the collect level, over the whole walk rather than the pipeline.
func TestTwoCollectsOfUnchangedInputProduceIdenticalIDs(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, []string{"istio-init"}, []string{"api"})
	api.addPod("dev", "api-2", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "connection to database failed for alpha"))
	api.addLog("dev", "api-1", "istio-init", stamped(fixtureBase, "sidecar ready"))
	api.addLog("dev", "api-2", "api", stamped(fixtureBase, "connection to database failed for beta"))
	c := collectorOver(api, t)

	first, err := c.Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		again, err := c.Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Nodes) != len(first.Nodes) {
			t.Fatalf("collect %d emitted %d nodes against %d", i, len(again.Nodes), len(first.Nodes))
		}
		for j := range first.Nodes {
			if again.Nodes[j].ID != first.Nodes[j].ID {
				t.Fatalf("collect %d: node %d is %s, the first collect had %s", i, j, again.Nodes[j].ID, first.Nodes[j].ID)
			}
		}
	}
}

// TestADifferentWindowLandsInTheSameGraph — the window is a parameter, never
// part of identity.
func TestADifferentWindowLandsInTheSameGraph(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one", "two"))
	c := collectorOver(api, t)

	wide, err := c.Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	narrow := devParams()
	narrow.Since = fixtureBase.Format(time.RFC3339)
	narrowRes, err := c.Walk(context.Background(), "id", narrow, framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	if streamIDs(wide) != streamIDs(narrowRes) {
		t.Fatal("reading the same source over a different time window produced different stream ids; " +
			"the window is a collect parameter and a stream's identity is its labels")
	}
}

// TestTheContextParameterReachesTheClientBuilder — a parameter the collector
// ignores is a parameter that narrows nothing.
func TestTheContextParameterReachesTheClientBuilder(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	client := api.client(t)

	var seen string
	c := &Collector{NewClient: func(name string) (kubernetes.Interface, error) {
		seen = name
		return client, nil
	}}
	params := devParams()
	params.Context = "gke_fulminate-services_us-central1_main"
	if _, err := c.Walk(context.Background(), "id", params, framework.ForeignContext{}); err != nil {
		t.Fatal(err)
	}
	if seen != params.Context {
		t.Fatalf("the client was built for context %q, want %q", seen, params.Context)
	}
}

// TestTheContextNameBecomesTheClusterLabels — and a non-GKE context emits
// neither, rather than emitting them empty.
func TestTheContextNameBecomesTheClusterLabels(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))
	c := collectorOver(api, t)

	params := devParams()
	params.Context = "gke_fulminate-services_us-central1_main-us-central1"
	res, err := c.Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := streamNodeOf(res)
	if !ok {
		t.Fatal("no stream node was emitted")
	}
	if got := stream.Metadata["label:"+LabelCluster]; got != "main-us-central1" {
		t.Errorf("the cluster label is %q, want the one the context name encodes", got)
	}
	if got := stream.Metadata["label:"+LabelProject]; got != "fulminate-services" {
		t.Errorf("the project label is %q", got)
	}

	params.Context = "minikube"
	plain, err := c.Walk(context.Background(), "id", params, framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	plainStream, ok := streamNodeOf(plain)
	if !ok {
		t.Fatal("no stream node was emitted for the non-GKE context")
	}
	for _, absent := range []string{"label:" + LabelCluster, "label:" + LabelProject} {
		if _, present := plainStream.Metadata[absent]; present {
			t.Errorf("a non-GKE context emitted %q; an unknown value is absent, not empty", absent)
		}
	}
}

// TestValidationRefusalsReachTheCaller — the walk does not swallow them.
func TestValidationRefusalsReachTheCaller(t *testing.T) {
	api := newFakeAPI(t)
	c := collectorOver(api, t)
	if _, err := c.Walk(context.Background(), "id", Params{}, framework.ForeignContext{}); err == nil {
		t.Fatal("a collect naming no namespaces was walked")
	}
}

// TestToolSpec — the served name is the one the config entry's `tool` field
// names, and the family is not the cloud collector's.
func TestToolSpec(t *testing.T) {
	spec := (&Collector{}).Tool()
	if spec.Name != ToolName {
		t.Fatalf("the served tool is %q, want %q", spec.Name, ToolName)
	}
	if spec.Description == "" {
		t.Fatal("the tool carries no description; a human installing it reads that in a tool listing")
	}
	if GraphFamily == "k8s" {
		t.Fatal("the graph family is `k8s`, which the Kubernetes CLOUD collector holds; a registration " +
			"name IS the family, so two collectors under one name write two vocabularies into one graph")
	}
}

func streamIDs(res framework.Result) string {
	out := make([]string, 0)
	for _, n := range res.Nodes {
		if n.Type == logpipe.NodeLogStream {
			out = append(out, n.ID)
		}
	}
	return strings.Join(out, ",")
}

func streamNodeOf(res framework.Result) (framework.Node, bool) {
	for _, n := range res.Nodes {
		if n.Type == logpipe.NodeLogStream {
			return n, true
		}
	}
	return framework.Node{}, false
}
