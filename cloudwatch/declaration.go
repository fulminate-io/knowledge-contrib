// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/fulminate-io/knowledge-contrib/framework"

// declaration.go — THE FOREIGN-GRAPH CONTEXT THIS COLLECTOR DECLARES.
//
// WHY IT DID NOT EXIST, AND WHAT THAT COST. This collector READS a foreign
// context on every collect — cloudContextFrom (resolve.go) turns it into the
// candidate set resolveStreams matches log-stream service labels against, and
// those resolutions become PROXY NODES, EMITTED_BY edges and, through
// findCorrelations, the CORRELATES_WITH edges the pipeline appends. It declared
// nothing, and a collector receives only what its entry declared, so the read
// was of an empty block on every collect an operator has ever run: the whole
// cross-graph half of this collector was dead by construction rather than by a
// wrong string.
//
// THE FIELD SET IS THIS MODULE'S OWN AND IS NOT INTERCHANGEABLE WITH ANOTHER
// COLLECTOR'S. cloudContextFrom reads `node.ID`, `node.SymbolName` and the
// `resource_type` METADATA KEY, and indexes both endpoints of every foreign
// edge. A declaration copied from the k8s-logs collector would carry that
// module's metadata keys — namespace, cluster_name, region, provider — and omit
// `resource_type`, so every candidate would arrive with an empty resource type,
// rank against nothing and match nothing: an empty result from a declaration
// that reads as thorough, which is the same defect class this file exists to
// close.
//
// WHICH FAMILIES, AND WHY GCP IS NOT ONE OF THEM. A declared family whose
// resources this collector cannot resolve costs the operator's privacy and the
// collect's size for nothing, and a family the operator has no collector
// registered for is REFUSED BY NAME at collect time. The service and namespace
// prefix lists (resolve.go) name ECS, Lambda, EC2 and account types, the
// Kubernetes kinds, and the Cloud Run, GCE and App Engine types — so aws and k8s
// are families whose resources a label here can actually resolve to. Azure types
// appear in neither list, so declaring azure would be admitted and would resolve
// nothing; widening those lists is a behaviour change to this collector's
// resolution rather than a declaration fix.
//
// GCP IS THE OTHER ABSENCE AND IT HAS A DIFFERENT REASON. The prefix lists DO
// name gcp types, but the gcp collector emits its resource type AS the node type
// — forty-nine `gcp:<service>:<kind>` values plus the `gcp-cidr-block` sentinel
// — and a declaration selects a type only by naming it exactly, at one node read
// per type per graph per collect. The only other admissible spelling today is
// the family with NO node types, which by the documented semantics yields the
// graph names and no nodes: zero candidates, exactly as declaring nothing does,
// plus a refusal for every operator who runs no gcp collector. So gcp is left
// out until the declaration contract gains an explicit family-level selector,
// and the README says so rather than leaving the absence to be read as an
// oversight.

// awsNodeTypeCloudResource and k8sNodeTypeCloudResource are the node types the
// AWS and Kubernetes inventory collectors emit, spelled here because those
// modules are separate Go modules this one cannot import.
//
// THEY ARE TWO CONSTANTS AND NOT ONE, even though the strings are equal today:
// they are facts about two different collectors, and sharing one would make a
// rename on either side invisible on the other. The cross-module census in the
// root module holds both against those modules' own constants and reds BY NAME
// on a declared type its owning module does not emit.
const (
	awsNodeTypeCloudResource = "cloud-resource"
	k8sNodeTypeCloudResource = "cloud-resource"
)

// The families this collector declares. Each is the name a provider collector is
// registered under, which is also the graph family its nodes land in.
const (
	familyAWS = "aws"
	familyK8s = "k8s"
)

// metaResourceType is the metadata key a cloud collector writes a resource's
// kind into, and the one this collector RANKS ON. It is declared rather than
// assumed: a node arriving without it ranks against nothing.
const metaResourceType = "resource_type"

// declaredForeignContext is what this collector declares it needs from other
// graphs, keyed by graph family in the shape the client's config loader decodes.
//
// THE EDGES ARE DECLARED AS WELL AS THE NODES, and that is what makes a
// CORRELATES_WITH edge possible at all. The nodes answer "which cloud resource
// is this service label", which the proxy half needs; the edges answer "do these
// two resources depend on each other", which is the half that upgrades a
// temporal coincidence between two templates into a correlation.
func declaredForeignContext() framework.ForeignContextDeclaration {
	return framework.ForeignContextDeclaration{
		familyAWS: {
			NodeTypes:    []string{awsNodeTypeCloudResource},
			NodeFields:   []string{"id", "symbol_name"},
			MetadataKeys: []string{metaResourceType},
			EdgeFields:   []string{"from_id", "to_id"},
			Reason: "EMITTED_BY and confirmed CORRELATES_WITH: a log group's service label is " +
				"resolved to an AWS resource by NAME and ranked by its resource_type, and the " +
				"edges are what CONFIRM a temporal correlation between two templates.",
		},
		familyK8s: {
			NodeTypes:    []string{k8sNodeTypeCloudResource},
			NodeFields:   []string{"id", "symbol_name"},
			MetadataKeys: []string{metaResourceType},
			EdgeFields:   []string{"from_id", "to_id"},
			Reason: "EMITTED_BY and confirmed CORRELATES_WITH: the same resolution against " +
				"Kubernetes workloads and namespaces, which the service and namespace prefix " +
				"lists rank ahead of the compute types.",
		},
	}
}
