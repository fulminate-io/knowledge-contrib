// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// declaration_flag_test.go — THE RESOLUTIONS REACH THE GRAPH WHATEVER THE
// CORRELATION FLAG SAYS, which is the property this module's declaration rests
// on and which nothing observed.
//
// WHY IT NEEDS ITS OWN ROW. declaration.go states it as a contract: only the
// correlation half is gated on cfg.CorrelationEnabled, so the declaration is
// load-bearing for the PROXY half even with correlation turned off. That
// sentence is true at source — computeStreamResolutions sits outside the `if` —
// and moving that one call inside the conditional left the entire module suite
// green, because no test in it sets the flag false at all and the default sets
// it true. Under that mutation an operator running with correlation disabled
// receives a declared foreign block, pays the read cost, and emits nothing from
// it: the same silent-empty-result class the declaration exists to end.
//
// BOTH ARMS RUN THE REAL runPipeline AND THE REAL buildResult, so what is
// asserted is the emitted graph rather than an intermediate the mutation could
// leave intact.

// flagFixtureCloud is a resolvable cloud context for both arms: two services,
// each naming a resource, and a declared dependency between them so the
// correlation half has something to confirm when it is enabled.
func flagFixtureCloud() *stubCloud {
	return &stubCloud{
		services: map[string]resolvedResource{
			"api":    {Account: "acct", ID: "res-api"},
			"worker": {Account: "acct", ID: "res-worker"},
		},
		dependent: map[string]bool{"res-api->res-worker": true},
	}
}

// flagArm runs the whole pipeline and result build under one flag setting and
// returns the emitted graph.
func flagArm(t *testing.T, correlationEnabled bool) ([]framework.Node, []framework.Edge) {
	t.Helper()
	cfg := defaultPipelineConfig()
	cfg.CorrelationEnabled = correlationEnabled
	out, err := runPipeline(crossServiceErrorEntries(), cfg, flagFixtureCloud())
	if err != nil {
		t.Fatalf("running the pipeline with correlation=%v: %v", correlationEnabled, err)
	}
	res, err := buildResult(out, false, 0)
	if err != nil {
		t.Fatalf("building the result with correlation=%v: %v", correlationEnabled, err)
	}
	return res.Nodes, res.Edges
}

func TestTheDeclaredResolutionsReachTheGraphWithCorrelationDisabled(t *testing.T) {
	nodes, edges := flagArm(t, false)

	if got := countNodeType(nodes, nodeTypeProxy); got == 0 {
		t.Error("with correlation DISABLED the walk emitted no proxy node. The resolutions are " +
			"computed outside the correlation conditional precisely so this arm keeps working, and " +
			"this module's declaration justifies its own read cost on that property")
	}
	if got := countEdgeType(edges, edgeTypeEmittedBy); got == 0 {
		t.Error("with correlation DISABLED the walk emitted no EMITTED_BY edge, so every resolution " +
			"the declared block paid for was dropped")
	}
	// THE HALF THAT IS SUPPOSED TO BE GATED, asserted here so the row above is a
	// statement about the resolutions rather than about the flag doing nothing.
	if got := countEdgeType(edges, edgeTypeCorrelatesWith); got != 0 {
		t.Errorf("with correlation DISABLED the walk emitted %d CORRELATES_WITH edge(s); that half "+
			"IS gated on the flag", got)
	}
}

// TestTheCorrelationHalfIsTheOnlyThingTheFlagGates is the same run's control. It
// is what makes the zero above a property of the flag rather than of a fixture
// that correlates nothing.
func TestTheCorrelationHalfIsTheOnlyThingTheFlagGates(t *testing.T) {
	offNodes, offEdges := flagArm(t, false)
	onNodes, onEdges := flagArm(t, true)

	if got := countEdgeType(onEdges, edgeTypeCorrelatesWith); got == 0 {
		t.Fatal("control: with correlation ENABLED this fixture must produce a CORRELATES_WITH edge, " +
			"or the disabled arm's zero says nothing about the flag")
	}
	if on, off := countNodeType(onNodes, nodeTypeProxy), countNodeType(offNodes, nodeTypeProxy); on != off {
		t.Errorf("the proxy count moved with the correlation flag: %d enabled against %d disabled. "+
			"The resolutions are not the flag's to gate", on, off)
	}
	if on, off := countEdgeType(onEdges, edgeTypeEmittedBy), countEdgeType(offEdges, edgeTypeEmittedBy); on != off {
		t.Errorf("the EMITTED_BY count moved with the correlation flag: %d enabled against %d disabled", on, off)
	}
}

func countNodeType(nodes []framework.Node, nodeType string) int {
	n := 0
	for _, node := range nodes {
		if node.Type == nodeType {
			n++
		}
	}
	return n
}

func countEdgeType(edges []framework.Edge, edgeType string) int {
	n := 0
	for _, e := range edges {
		if e.Type == edgeType {
			n++
		}
	}
	return n
}
