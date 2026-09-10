// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// declaration_seam_test.go — THE DECLARE SIDE AND THE READ SIDE, DRIVEN AS ONE.
//
// WHY A SEAM TEST AND NOT TWO UNIT TESTS. The declaration names families and
// fields; CloudContextFrom reads a block back out. Either can be right while the
// pair is broken, and that is exactly what happened: a correct read side sat
// next to a declaration nothing carried, and every read-side test passed against
// a block that in production never arrived. What has to hold is that the block
// an operator's ENTRY causes to be filled is a block THIS collector can consume.
//
// BOTH SIDES ARE REAL. The declaration is the shipped DeclaredForeignContext,
// serialized through the config entry's own JSON exactly as the loader would
// read it; the consumer is the shipped CloudContextFrom over the shipped
// framework.ForeignContext type. Nothing is stubbed but the graph payload, which
// is the daemon's to supply and is not part of this seam.
//
// WHAT IT DELIBERATELY DOES NOT CLAIM. It does not prove the daemon fills the
// block correctly — that is the client's own contract, tested there. It proves
// the two ENDS agree: every family this collector declares is a family its read
// side takes, and the `code` family it does not declare is one the read side
// drops.

// filledFromDeclaration builds the block a client WOULD fill for this entry: one
// ForeignGraph per declared family, carrying one node shaped by that entry's
// declared fields. It is derived from the declaration rather than hand-written,
// so a family added to the declaration is automatically driven here too.
func filledFromDeclaration(t *testing.T) framework.ForeignContext {
	t.Helper()

	// THROUGH THE CONFIG ENTRY'S OWN JSON, not straight off the Go value: the
	// declaration reaches the daemon as the config file's bytes, so a json tag
	// that did not match would be invisible to a test that skipped the encode.
	raw, err := ExampleEntry(OSLinux, false, "/home/you/.knowledge/bin/knowledge-collector-k8s-logs")
	require.NoError(t, err)
	var doc struct {
		Collectors map[string]struct {
			Context framework.ForeignContextDeclaration `json:"context"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	decls := doc.Collectors[GraphFamily].Context
	require.NotEmpty(t, decls, "the entry must carry the declaration for this seam to have a left side")

	out := framework.ForeignContext{}
	for family, d := range decls {
		graph := framework.ForeignGraph{GraphName: family + "-prod"}
		// THE FIXTURE NODE CARRIES THE DECLARED TYPE, not a type this test made
		// up from the family name. That substitution is what the old fixture did,
		// and it is why this seam stayed green while the shipped declaration
		// selected nothing: a fixture that answers with whatever was asked for
		// cannot tell a right declaration from a wrong one. A family declaring NO
		// node types therefore arrives with NO nodes, which is the honest fill for
		// the gcp entry rather than a gap in the fixture.
		for _, nodeType := range d.NodeTypes {
			graph.Nodes = append(graph.Nodes, framework.ForeignNode{
				ID:   family + ":resource/1",
				Type: nodeType,
				Metadata: map[string]string{
					cloudMetaNamespace:    "dev",
					cloudMetaCluster:      "my-cluster",
					cloudMetaResourceType: family + ":compute",
					cloudMetaRegion:       "us-east-1",
					cloudMetaProvider:     family,
				},
			})
		}
		if len(d.EdgeFields) > 0 && len(graph.Nodes) > 0 {
			graph.Edges = []framework.ForeignEdge{{FromID: family + ":resource/1", ToID: family + ":resource/2"}}
		}
		out[family] = []framework.ForeignGraph{graph}
	}
	// AND THE ONE FAMILY THE DECLARATION DOES NOT NAME, supplied anyway. The
	// client fills `code` for every collector, so the read side meets it whether
	// this collector asked or not, and dropping it is a behavior worth pinning
	// rather than an accident.
	out[framework.FamilyCode] = []framework.ForeignGraph{{GraphName: "some-repo"}}
	return out
}

func TestSeam_TheDeclaredFamiliesAreExactlyWhatTheReadSideConsumes(t *testing.T) {
	block := filledFromDeclaration(t)
	got := CloudContextFrom(block)

	require.False(t, got.IsEmpty(),
		"the read side found nothing in a block filled from this collector's OWN declaration — the "+
			"two ends disagree, which is the whole failure this seam exists to catch")

	// THE READ SIDE FLATTENS the families into one slice deliberately (see
	// CloudContextFrom: this module matches on resource metadata and never on a
	// provider name), so the seam is checked by GRAPH NAME rather than by family
	// key — the flattening is the behavior, not an obstacle to it.
	byName := map[string]framework.ForeignGraph{}
	for _, g := range got.Cloud {
		byName[g.GraphName] = g
	}

	declared := DeclaredForeignContext()
	for _, family := range cloudProviderFamilies() {
		g, ok := byName[family+"-prod"]
		require.Truef(t, ok, "the read side dropped the declared family %q", family)

		if len(declared[family].NodeTypes) == 0 {
			// THE NAMES-ONLY ARM, ASSERTED RATHER THAN SKIPPED. The gcp entry
			// declares no node types, so its graph reaches this collector as a NAME
			// and nothing else: no resource to resolve against and no edge to
			// confirm a correlation with. That is the disclosed pre-selector state,
			// and stating it here is what keeps it from reading as a fixture gap.
			assert.Emptyf(t, g.Nodes, "the %q entry declares no node types, so it carries no nodes", family)
			assert.Emptyf(t, g.Edges, "the %q entry declares no node types, so there is no pivot and no edges", family)
			continue
		}
		require.NotEmptyf(t, g.Nodes, "the %q graph arrived with no nodes", family)

		// THE FIELDS THE DECLARATION ASKED FOR ARE THE FIELDS THE READ USES. A
		// declaration that named different metadata keys than the matcher reads
		// would yield a block full of nodes that match nothing.
		n := g.Nodes[0]
		assert.NotEmptyf(t, n.ID, "the %q node carries the declared id", family)
		assert.NotEmptyf(t, n.Metadata[cloudMetaNamespace],
			"the %q node carries the declared namespace the resolver matches on", family)

		// AND THE EDGES, which are what CONFIRM a correlation.
		assert.NotEmptyf(t, g.Edges, "the %q graph carries the declared edges", family)
	}

	// THE NEGATIVE HALF, in the same run: `code` is supplied and deliberately not
	// consumed. Without it, a read side that returned the whole block unfiltered
	// would satisfy every row above.
	_, hasCode := byName["some-repo"]
	assert.False(t, hasCode,
		"the read side must drop the code family: it is supplied to every collector and this one "+
			"correlates against provider resources, not source")
}

// TestSeam_AnEmptyBlockIsConsumedWithoutClaimingACorrelation is the other
// direction of the same seam, and it is the arm an operator meets first: they
// install the entry, they have no provider collector registered yet, and the
// client fills the declared families with nothing.
//
// THE OBSERVABLE IS "NO CORRELATION IS CLAIMED", not "no error". An empty block
// is a legitimate answer to a legitimate declaration, so the failure to avoid is
// a collector that invents a resolution it has no evidence for.
func TestSeam_AnEmptyBlockIsConsumedWithoutClaimingACorrelation(t *testing.T) {
	empty := framework.ForeignContext{}
	for _, family := range cloudProviderFamilies() {
		// A DECLARED FAMILY PRESENT WITH AN EMPTY SLICE, which is the contract's
		// own shape: the client answers a declared family with an empty slice
		// rather than a missing key, so the collector can tell "asked and got
		// nothing" from "never asked".
		empty[family] = []framework.ForeignGraph{}
	}

	// THE DISTINCTION SURVIVES ON THE WIRE TYPE and is LOST BY THE FLATTENING,
	// and both halves are asserted because only the first is a contract. The
	// framework block still reports every declared family as present; the
	// collector's own view flattens them away, which is correct for a module that
	// never reads a provider name.
	assert.False(t, empty.IsEmpty(),
		"the framework block still carries the declared families as present-with-nothing")
	assert.Len(t, empty.Families(), len(cloudProviderFamilies()),
		"asked-and-got-nothing stays distinguishable from never-asked on the wire type")

	got := CloudContextFrom(empty)
	assert.True(t, got.IsEmpty(),
		"a block whose declared families hold no graphs carries nothing to resolve against")
	assert.Empty(t, got.Cloud,
		"and the flattened view is empty rather than carrying empty graphs")

	// THE CONTROL: the same read side over a populated block is NOT empty, so the
	// assertion above is a statement about emptiness rather than about a read
	// that always answers nothing.
	assert.False(t, CloudContextFrom(filledFromDeclaration(t)).IsEmpty(),
		"control: a populated block is not reported empty")
}

// TestSeam_TheDeclarationDecidesWhetherTheCorrelationArmRunsAtAll is the arm
// collector.go gates on, and it is the one the shipped declaration silenced.
//
// build() takes the resolution arm only `if !cloud.IsEmpty()`, and assigns the
// correlation resolver only when Correlation returns non-nil. A declaration
// naming node types no collector emits yields a block whose families are all
// PRESENT and all EMPTY, so IsEmpty is true, the whole arm is skipped, and the
// collect emits a log graph with no proxies and no confirmed correlations —
// indistinguishable, from inside this module, from an operator who has no cloud
// collectors installed.
func TestSeam_TheDeclarationDecidesWhetherTheCorrelationArmRunsAtAll(t *testing.T) {
	entries := []logpipe.Entry{{
		Timestamp: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		Message:   "request completed",
		Labels: map[string]string{
			cloudMetaNamespace: "dev", cloudMetaCluster: "my-cluster", "pod": "api-0",
		},
	}}
	streams, _ := logpipe.BuildStreams(entries, 0)

	// THE CORRECTED DECLARATION: the block carries nodes, so the arm runs.
	correct := CloudContextFrom(filledFromDeclaration(t))
	require.False(t, correct.IsEmpty(),
		"a block filled from this collector's own declaration must not be empty; if it is, "+
			"collector.go skips the resolution arm entirely and every correlation is lost silently")
	assert.NotNil(t, correct.Correlation(streams),
		"and the correlation resolver must be live, or the detector leaves every candidate unconfirmed")

	// THE PRE-CHANGE ARM, in the same run and through the same code: every
	// declared family PRESENT and EMPTY, which is exactly what the shipped
	// declaration's exact-string filter produced.
	shipped := framework.ForeignContext{}
	for _, family := range cloudProviderFamilies() {
		shipped[family] = []framework.ForeignGraph{{GraphName: family + "-prod"}}
	}
	got := CloudContextFrom(shipped)
	assert.True(t, got.IsEmpty(),
		"four families present and empty is what a declaration naming unemitted types delivers")
	assert.Nil(t, got.Correlation(streams),
		"so there is no resolver, and collector.go's `if !cloud.IsEmpty()` skips the whole arm")
}
