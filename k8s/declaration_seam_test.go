// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// declaration_seam_test.go — THE DECLARE SIDE AND THE READ SIDE, DRIVEN AS ONE.
//
// WHY IT DID NOT EXIST AND WHAT THAT COST. This module's linkage suite drives
// the four shapes against a HAND-WRITTEN foreign context whose azure node
// carries `Type: "cloud-resource"`. That block is what a correct declaration
// would have produced — and the shipped declaration asked for `azure-resource`,
// which the azure collector does not emit. So the read side was exercised
// against a block production never delivered: the fill selected zero nodes,
// cloudIdentitiesByClientID indexed an empty set, and every WORKLOAD_IDENTITY
// edge was lost with the whole suite green.
//
// THE FIXTURE HERE IS BUILT FROM THE DECLARATION, so a declared node type the
// producing collector does not emit yields a block with NO nodes, exactly as the
// client's exact-string filter leaves it.

// blockFromDeclaration builds the block a client WOULD fill for this module's
// entry, given what the AZURE collector actually emits.
//
// azureEmits is the node type the azure collector emits. It is a PARAMETER
// rather than a constant so the mutation row can drive the same builder with the
// producer unchanged and the DECLARATION wrong, which is the defect's own shape:
// the producer was always right.
func blockFromDeclaration(decl framework.ForeignContextDeclaration, azureEmits, clientID string) framework.ForeignContext {
	out := framework.ForeignContext{}
	for _, family := range decl.Families() {
		d := decl[family]
		graph := framework.ForeignGraph{GraphName: family + "-instance"}
		if family == familyAzure {
			// THE PRODUCER'S NODE. It enters the slice only when the declaration
			// names its type, which is the whole subject of this seam.
			for _, declared := range d.NodeTypes {
				if declared != azureEmits {
					continue
				}
				node := framework.ForeignNode{}
				for _, field := range d.NodeFields {
					if field == "id" {
						node.ID = "/subscriptions/sub-abc/resourceGroups/rg/providers/identity/api-identity"
					}
				}
				for _, key := range d.MetadataKeys {
					if node.Metadata == nil {
						node.Metadata = map[string]string{}
					}
					if key == "client_id" {
						node.Metadata[key] = clientID
						continue
					}
					node.Metadata[key] = "declared-but-unused"
				}
				graph.Nodes = append(graph.Nodes, node)
			}
		}
		out[family] = []framework.ForeignGraph{graph}
	}
	return out
}

// TestSeam_TheAzureDeclarationYieldsTheWorkloadIdentityEdge is the row the
// shipped declaration could not have satisfied.
func TestSeam_TheAzureDeclarationYieldsTheWorkloadIdentityEdge(t *testing.T) {
	const clientID = "11111111-2222-3333-4444-555555555555"
	nodes, _ := fullLinkageFixture()

	block := blockFromDeclaration(declaredForeignContext(), azureNodeTypeCloudResource, clientID)
	require.NotEmptyf(t, cloudIdentitiesByClientID(block),
		"the block filled from this module's OWN declaration indexed no azure identity — the "+
			"declaration and the read side disagree, which is the failure this seam exists to catch")

	assert.Equal(t, 1, countWorkloadIdentityEdges(buildLinkageEdges(nodes, block)),
		"the declared block must produce the WORKLOAD_IDENTITY edge the Azure shape exists for")

	// THE MUTATION, and it is the exact string that shipped: a declaration naming
	// a type the azure collector does not emit. The producer is unchanged; only
	// the declaration moves.
	wrong := framework.ForeignContextDeclaration{}
	for family, d := range declaredForeignContext() {
		if family == familyAzure {
			d.NodeTypes = []string{family + "-resource"}
		}
		wrong[family] = d
	}
	wrongBlock := blockFromDeclaration(wrong, azureNodeTypeCloudResource, clientID)
	assert.Empty(t, cloudIdentitiesByClientID(wrongBlock),
		"a declaration naming a type the azure collector does not emit selects nothing")
	assert.Zero(t, countWorkloadIdentityEdges(buildLinkageEdges(nodes, wrongBlock)),
		"and with nothing to resolve against, the WORKLOAD_IDENTITY edge is lost — silently, which "+
			"is what this collector shipped doing")
}

// TestSeam_TheIRSAAndGCPShapesAreUnaffectedByTheForeignBlock is the
// no-regression row. Two of the four shapes compose their targets from the
// service account's own metadata and read no foreign context, so they must emit
// identically under a full block and under none.
func TestSeam_TheIRSAAndGCPShapesAreUnaffectedByTheForeignBlock(t *testing.T) {
	nodes, _ := fullLinkageFixture()
	full := blockFromDeclaration(declaredForeignContext(), azureNodeTypeCloudResource,
		"11111111-2222-3333-4444-555555555555")

	withBlock := countEdgesByType(buildLinkageEdges(nodes, full))
	withNone := countEdgesByType(buildLinkageEdges(nodes, framework.ForeignContext{}))

	// THE THREE WORKLOAD-IDENTITY SHAPES SHARE ONE EDGE TYPE and differ by the
	// family they target, so the count is keyed on the PAIR. Counting by type
	// alone would let a lost Azure edge hide behind an IRSA one.
	for _, family := range []string{familyAWS, familyGCP} {
		key := edgeWorkloadIdentity + "->" + family
		assert.Equalf(t, withNone[key], withBlock[key],
			"the %s workload-identity shape reads no foreign context, so the block must not change "+
				"what it emits", family)
		assert.NotZerof(t, withBlock[key], "and the fixture must actually exercise the %s shape", family)
	}

	// THE CONTROL that makes the equalities above about those two shapes rather
	// than about a block that changes nothing: the Azure shape DOES move.
	azure := edgeWorkloadIdentity + "->" + familyAzure
	assert.NotEqual(t, withNone[azure], withBlock[azure],
		"control: the Azure workload-identity shape is the one the block feeds")
}

func countWorkloadIdentityEdges(edges []Edge) int {
	return countEdgesByType(edges)[edgeWorkloadIdentity+"->"+familyAzure]
}

// countEdgesByType keys on the edge type AND the family its foreign endpoint
// names, because three distinct shapes share the WORKLOAD_IDENTITY type.
func countEdgesByType(edges []Edge) map[string]int {
	out := map[string]int{}
	for _, e := range edges {
		family := e.TargetGraph
		if family == "" {
			family = e.SourceGraph
		}
		out[e.Type+"->"+family]++
	}
	return out
}
