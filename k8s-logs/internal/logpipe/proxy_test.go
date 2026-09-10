// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"
	"time"
)

// proxy_test.go — the cloud-facing families, and the ZERO ARM that is their
// control.
//
// THE ZERO ARM IS PARITY, NOT A GAP. The built-in pipeline emits zero proxies,
// zero EMITTED_BY and zero CORRELATES_WITH when no cloud graph is attached, so
// a run with no supplied resolutions asserting zero is asserting the same
// behavior. The with-resolutions arm beside it is the known positive that makes
// that zero a measurement rather than an absence.

func labelSet(ns, pod string) map[string]string {
	return map[string]string{"container": "api", "container_pod": pod, "namespace": ns}
}

// TestNoCloudContextEmitsNoneOfTheThreeFamilies is the control arm.
func TestNoCloudContextEmitsNoneOfTheThreeFamilies(t *testing.T) {
	res := buildFixtureGraph(t)
	for _, n := range res.Nodes {
		if n.Type == NodeProxy {
			t.Fatalf("a proxy node (%s) was emitted with no cloud resolutions supplied", n.ID)
		}
	}
	for _, e := range res.Edges {
		if e.Type == EdgeEmittedBy || e.Type == EdgeCorrelatesWith {
			t.Fatalf("a %s edge (%s -> %s) was emitted with no cloud context supplied", e.Type, e.FromID, e.ToID)
		}
	}
	if len(res.Correlations) != 0 {
		t.Fatalf("%d correlations were produced with no cloud context supplied", len(res.Correlations))
	}
}

// TestSuppliedResolutionsEmitProxiesAndEmittedBy is the known positive through
// the same instrument.
func TestSuppliedResolutionsEmitProxiesAndEmittedBy(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: base, Message: "request served in good time", Labels: labelSet("dev", "api-1")},
	}
	res, err := Build(entries, Options{
		ChunkWindow: time.Hour,
		Resolutions: []Resolution{{
			LabelKey: "namespace", LabelValue: "dev",
			Account: "fulminate-services", ResourceID: "ns/dev",
			ResourceType: "k8s_namespace", Region: "us-central1", Provider: "gcp",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var proxy *struct{ ID, Source, Symbol string }
	for _, n := range res.Nodes {
		if n.Type != NodeProxy {
			continue
		}
		proxy = &struct{ ID, Source, Symbol string }{n.ID, n.Source, n.SymbolName}
		if want := "proxy:cloud:fulminate-services:ns/dev"; n.ID != want {
			t.Errorf("proxy id is %q, want %q; the id convention is what makes this the SAME node the client would build", n.ID, want)
		}
		if want := "proxy:cloud:fulminate-services"; n.Source != want {
			t.Errorf("proxy source is %q, want %q", n.Source, want)
		}
		for k, want := range map[string]string{
			"foreign_graph": "cloud",
			"foreign_id":    "ns/dev",
			"account":       "fulminate-services",
			"resource_type": "k8s_namespace",
			"region":        "us-central1",
			"provider":      "gcp",
		} {
			if got := n.Metadata[k]; got != want {
				t.Errorf("proxy metadata %q is %q, want %q", k, got, want)
			}
		}
	}
	if proxy == nil {
		t.Fatal("no proxy node was emitted from a supplied resolution")
	}

	found := false
	for _, e := range res.Edges {
		if e.Type != EdgeEmittedBy {
			continue
		}
		found = true
		if e.FromID != "log-label:namespace=dev" {
			t.Errorf("EMITTED_BY runs from %q; it runs from the LABEL node", e.FromID)
		}
		if e.ToID != proxy.ID {
			t.Errorf("EMITTED_BY runs to %q, want the proxy %q", e.ToID, proxy.ID)
		}
	}
	if !found {
		t.Fatal("no EMITTED_BY edge was emitted from a supplied resolution")
	}
}

// TestProxyIsDeduplicatedAcrossResolutions — two labels resolving to one
// resource are one proxy and two edges.
func TestProxyIsDeduplicatedAcrossResolutions(t *testing.T) {
	nodes, edges, err := MaterializeProxies([]Resolution{
		{LabelKey: "namespace", LabelValue: "dev", Account: "acct", ResourceID: "r1"},
		{LabelKey: "namespace", LabelValue: "prod", Account: "acct", ResourceID: "r1"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("%d proxy nodes for one resource; two labels resolving to one resource make one proxy", len(nodes))
	}
	if len(edges) != 2 {
		t.Fatalf("%d EMITTED_BY edges for two resolutions; each label gets its own", len(edges))
	}
}

// TestResolutionNamingAnUnknownLabelIsRefused — bad input errors rather than
// producing a dangling edge.
func TestResolutionNamingAnUnknownLabelIsRefused(t *testing.T) {
	known := map[string]struct{}{"log-label:namespace=dev": {}}
	_, _, err := MaterializeProxies([]Resolution{
		{LabelKey: "namespace", LabelValue: "staging", Account: "acct", ResourceID: "r1"},
	}, known)
	if err == nil {
		t.Fatal("a resolution naming a label the walk never emitted was accepted; the EMITTED_BY edge would dangle")
	}
	if !strings.Contains(err.Error(), "log-label:namespace=staging") {
		t.Fatalf("the refusal does not name the label it is about: %v", err)
	}
}

// TestResolutionWithNoAccountIsRefused — the other bad-input arm, and it names
// which half is missing.
func TestResolutionWithNoAccountIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  Resolution
		want string
	}{
		{"no account", Resolution{LabelKey: "namespace", LabelValue: "dev", ResourceID: "r1"}, "cloud account"},
		{"no resource", Resolution{LabelKey: "namespace", LabelValue: "dev", Account: "acct"}, "cloud resource id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := MaterializeProxies([]Resolution{tc.res}, nil)
			if err == nil {
				t.Fatal("an incomplete resolution was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the refusal does not name %q: %v", tc.want, err)
			}
		})
	}
}

// TestProxyEmissionFollowsAStatedOrder. Stability across runs is NOT enough to
// observe this: a Go slice iterates in its own order every time, so a walk that
// simply followed the caller's slice would be stable and would still emit a
// different order for the same resolution SET arriving in a different order.
// The assertion is therefore the stated order itself.
func TestProxyEmissionFollowsAStatedOrder(t *testing.T) {
	// Supplied deliberately out of order, and again in a third order, so a
	// pass-through of the caller's slice cannot satisfy both.
	shuffled := [][]Resolution{
		{
			{LabelKey: "namespace", LabelValue: "z", Account: "acct", ResourceID: "r3"},
			{LabelKey: "namespace", LabelValue: "a", Account: "acct", ResourceID: "r1"},
			{LabelKey: "namespace", LabelValue: "m", Account: "acct", ResourceID: "r2"},
		},
		{
			{LabelKey: "namespace", LabelValue: "m", Account: "acct", ResourceID: "r2"},
			{LabelKey: "namespace", LabelValue: "z", Account: "acct", ResourceID: "r3"},
			{LabelKey: "namespace", LabelValue: "a", Account: "acct", ResourceID: "r1"},
		},
	}
	want := []string{
		"proxy:cloud:acct:r1", // namespace=a
		"proxy:cloud:acct:r2", // namespace=m
		"proxy:cloud:acct:r3", // namespace=z
	}
	wantEdges := []string{
		"log-label:namespace=a", "log-label:namespace=m", "log-label:namespace=z",
	}

	for i, resolutions := range shuffled {
		nodes, edges, err := MaterializeProxies(resolutions, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != len(want) || len(edges) != len(wantEdges) {
			t.Fatalf("input %d emitted %d nodes and %d edges, want %d and %d",
				i, len(nodes), len(edges), len(want), len(wantEdges))
		}
		for j := range want {
			if nodes[j].ID != want[j] {
				t.Fatalf("input %d: proxy %d is %s, want %s. The emitted order must be a function of the "+
					"resolution SET, not of the order the caller happened to supply it in", i, j, nodes[j].ID, want[j])
			}
			if edges[j].FromID != wantEdges[j] {
				t.Fatalf("input %d: EMITTED_BY %d runs from %s, want %s", i, j, edges[j].FromID, wantEdges[j])
			}
		}
	}
}
