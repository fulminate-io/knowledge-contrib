// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// crossgraph_test.go — THE CROSS-GRAPH EDGE SET, which for this collector is
// EMPTY, and the two controls that make an empty set a measurement rather than a
// blind instrument.
//
// WHY THERE IS A ROW AT ALL. The client that spawns this collector used to run a
// post-collect pass that wrote edges into a linkage graph, and that pass is now
// gated off for a registered collector: whatever it produced for this provider
// family is this module's to emit. What it produced for this family is NOTHING,
// because every one of that pass's source predicates matches a Kubernetes-shaped
// node and none matches anything this collector emits.
//
// ONE SHAPE COMES CLOSE and is recorded rather than carried. That pass also
// matched a cloud resource by two Helm labels — a chart name and a chart
// reference — and this collector stamps arbitrary provider labels into metadata
// under a label/ prefix. A resource literally labeled with one of those two
// keys would therefore have satisfied the predicate. The edge's SOURCE would
// still have been a chart in a code graph rather than anything here, so this
// collector is a resolved target and never a source; and no fixture carries
// either label, which is what the second control below asserts.

// The two label keys the retired pass read off a cloud-family node. They are
// written out here rather than referenced, because the point of the control is
// to be an EXTERNAL expectation.
const (
	helmChartNameLabel = "label/app.kubernetes.io/name"
	helmChartRefLabel  = "label/helm.sh/chart"
)

// carriesHelmLabel is the instrument: it reproduces the predicate that pass used
// to select a cloud resource. It is deliberately this test's own code rather
// than an import, because the module it lived in is not importable from here and
// the predicate is two map lookups.
func carriesHelmLabel(node map[string]string) bool {
	_, byName := node[helmChartNameLabel]
	_, byRef := node[helmChartRefLabel]
	return byName || byRef
}

// TestTheWalkEmitsNoCrossGraphEdge is the amendment's own observable: over this
// module's parity fixtures the cross-graph edge set is empty.
func TestTheWalkEmitsNoCrossGraphEdge(t *testing.T) {
	got := walkTheFixtureProject(t)

	// CONTROL ONE, THE INSTRUMENT CONTROL. Without it, an instrument that could
	// never see an edge and a set that is genuinely empty read identically. A
	// synthetic node carrying the chart-name label MUST be selected by the
	// predicate, in this same run.
	if !carriesHelmLabel(map[string]string{helmChartNameLabel: "api"}) {
		t.Fatal("the predicate does not select a node carrying the chart-name label; " +
			"the zero below would prove nothing")
	}
	if !carriesHelmLabel(map[string]string{helmChartRefLabel: "api-1.2.3"}) {
		t.Fatal("the predicate does not select a node carrying the chart-reference label")
	}
	if carriesHelmLabel(map[string]string{"label/env": "prod"}) {
		t.Fatal("the predicate selects a node carrying an unrelated label; it is not the predicate")
	}

	// CONTROL TWO, THE FIXTURE CONTROL. It is a REQUIREMENT ON THE FIXTURE SET
	// rather than an observation of it: the empty result below must follow from
	// the inputs, so no fixture may carry either label. Adding one to a fixture
	// reds this and names the fixture, which is exactly the tell that the set has
	// grown the one shape a cross-graph carrier would have to express.
	for _, res := range got.Resources {
		if carriesHelmLabel(res.Metadata) {
			t.Errorf("parity fixture %q carries a chart label; the empty set below would then be "+
				"a fact about the walk rather than about the inputs", res.ID)
		}
	}

	// THE OBSERVATION. No emitted resource is selected by the predicate, so the
	// set of cross-graph edges this walk would owe is empty.
	var selected []string
	for _, res := range got.Resources {
		if carriesHelmLabel(res.Metadata) {
			selected = append(selected, res.ID)
		}
	}
	if len(selected) != 0 {
		t.Errorf("the walk emitted %d resources the retired cross-graph pass would have matched: %s",
			len(selected), strings.Join(selected, ", "))
	}
}

// TestNoEmittedEdgeNamesAForeignGraph is the other half of the same claim, at
// the shape level: nothing this walk emits addresses another graph.
//
// The contract's edge CAN name another graph — source_graph or target_graph —
// but this walk names neither, so a foreign id here would be accepted verbatim
// and land dangling inside this collector's own graph rather than being refused.
// That is what this asserts against: every edge endpoint is either a node this
// walk carries or an id in this provider's own address space.
func TestNoEmittedEdgeNamesAForeignGraph(t *testing.T) {
	// THE CONTROL FIRST, because an allow-list that admitted everything would
	// make the loop below vacuous. Each of these is an id from ANOTHER graph
	// family — a linkage proxy, a code symbol, a Kubernetes object — and the
	// classifier must refuse all three.
	for _, foreign := range []string{
		"proxy:knowledge:abcd",
		"cmd/knowledge/internal/tools/collect.go:runCollect",
		"default/Deployment/api",
	} {
		if classifyEndpoint(foreign) != endpointForeign {
			t.Fatalf("the classifier admits %q, which is an id from another graph; "+
				"the assertion below would then check nothing", foreign)
		}
	}

	got := walkTheFixtureProject(t)
	present := map[string]bool{}
	for _, res := range got.Resources {
		present[res.ID] = true
	}
	for _, rel := range got.Relations {
		for _, endpoint := range []string{rel.From, rel.To} {
			if present[endpoint] || classifyEndpoint(endpoint) != endpointForeign {
				continue
			}
			t.Errorf("edge %s names the endpoint %q, which is neither a node this walk carries "+
				"nor an address any of this collector's own address spaces recognizes",
				rel.Type, endpoint)
		}
	}
}

// endpointKind classifies an edge endpoint by the address space it belongs to.
type endpointKind int

const (
	// endpointForeign is an id this collector has no address space for. It is
	// the one this collector must never emit.
	endpointForeign endpointKind = iota
	// endpointProvider is a resource in this cloud provider's own space.
	endpointProvider
	// endpointPrincipal is an identity in the DIRECTORY's space rather than the
	// provider's: a person, a group, a domain. A group membership legitimately
	// names one, and there is no resource in this project to point at instead.
	endpointPrincipal
	// endpointExternal is an endpoint outside the provider entirely — the URL a
	// scheduled job calls. Recording it is the whole content of "this job calls
	// that": it will never resolve to a node, and turning it into no edge at all
	// would drop the fact rather than represent it honestly.
	endpointExternal
)

func classifyEndpoint(id string) endpointKind {
	switch {
	case strings.HasPrefix(id, "https://www.googleapis.com/compute/"),
		strings.HasPrefix(id, "projects/"),
		strings.HasPrefix(id, "groups/"),
		strings.HasPrefix(id, "gs://"),
		strings.HasPrefix(id, "storage.googleapis.com/"),
		strings.HasPrefix(id, "gcp:cidr:"),
		strings.HasPrefix(id, "gcp:bgp-peer:"),
		strings.HasPrefix(id, "gcp:ar-remote:"),
		strings.HasPrefix(id, "roles/"):
		return endpointProvider
	case strings.HasPrefix(id, "group:"), strings.HasPrefix(id, "user:"),
		strings.HasPrefix(id, "serviceAccount:"), strings.HasPrefix(id, "domain:"):
		return endpointPrincipal
	case strings.Contains(id, "@") && !strings.Contains(id, "/"):
		// A bare email: a directory member, which is what a membership edge
		// names.
		return endpointPrincipal
	case strings.HasPrefix(id, "https://"), strings.HasPrefix(id, "http://"):
		return endpointExternal
	default:
		return endpointForeign
	}
}

// TestTheSyntheticSentinelsAreMarkedUncollected keeps the three synthetic node
// kinds honest. Each stands for something outside this project that no
// enumeration can read, and a reader must be able to tell that from a resource
// this walk failed to describe.
func TestTheSyntheticSentinelsAreMarkedUncollected(t *testing.T) {
	got := walkTheFixtureProject(t)
	seen := map[string]bool{}
	for _, res := range got.Resources {
		switch res.ResourceType {
		case gcpgraph.ResourceTypeBGPPeer, gcpgraph.ResourceTypeARRemote:
			seen[res.ResourceType] = true
			if res.Metadata["collected"] != "false" {
				t.Errorf("%s node %q does not say it was referenced rather than read: %v",
					res.ResourceType, res.ID, res.Metadata)
			}
			if res.Metadata["collected_reason"] == "" {
				t.Errorf("%s node %q says it was not collected without saying why",
					res.ResourceType, res.ID)
			}
		case gcpgraph.ResourceTypeCIDRBlock:
			seen[res.ResourceType] = true
			// The CIDR sentinel is a DERIVED node rather than an uncollected
			// reference: it stands for an address range, which is not a resource
			// anywhere. It carries the range instead.
			if res.Metadata["cidr"] == "" {
				t.Errorf("the cidr sentinel %q does not carry the range it stands for", res.ID)
			}
		}
	}
	for _, want := range []string{
		gcpgraph.ResourceTypeBGPPeer,
		gcpgraph.ResourceTypeARRemote,
		gcpgraph.ResourceTypeCIDRBlock,
	} {
		if !seen[want] {
			t.Errorf("the fixture project produced no %s node, so this assertion checked nothing", want)
		}
	}
}
