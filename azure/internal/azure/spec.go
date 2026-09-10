// SPDX-License-Identifier: Apache-2.0

// Package azure walks an Azure subscription and returns it as one collector
// contract result: every resource as a cloud-resource node keyed by its ARM
// resource id, and the relationships between them as edges.
//
// THE SHAPE OF A WALK. Each subcollector owns one Azure service, calls that
// service's ARM list pagers, and returns [resource] and [edge] values; the
// runner in walk.go fans them out at a bounded degree and merges the results.
// Nothing about MCP, JSON Schema or the contract envelope is visible below
// collector.go: that is the framework's, and this package only produces the
// walk.
//
// SDK TYPES NEVER ESCAPE A SUBCOLLECTOR. The boundary is [resource] and
// [edge]; a subcollector splits into a thin lister that pages, and pure
// conversion functions that take an SDK struct and return specs. The pure half
// is what the tests drive, with hand-built SDK structs.
package azure

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// nodeTypeCloudResource is the ONE node type this collector emits. The Azure
// resource type is not the node type: it rides metadata key resource_type, so a
// consumer filters on a metadata value rather than on a type vocabulary that
// would grow by one entry per Azure service.
const nodeTypeCloudResource = "cloud-resource"

// sourceCloud is the Node.Source value every node carries, naming the
// collection family rather than this collector, so a cloud node from any
// provider reads the same.
const sourceCloud = "cloud"

// metaResourceType, metaRegion and the proxy keys below are the metadata keys
// a consumer reads by name. They are an informal contract: a rename here is a
// silent break for every query that filters on them, which is why they are
// constants rather than literals at the emission sites.
const (
	metaResourceType = "resource_type"
	metaRegion       = "region"

	// metaCollected marks a PROXY node: one this collector references but does
	// not enumerate, because the reference does not carry enough identity to
	// reconstruct the real ARM id. It is the string "false" rather than a bool
	// because node metadata is a string map on the wire.
	metaCollected       = "collected"
	metaCollectedReason = "collected_reason"
	metaDiscoveredVia   = "discovered_via"
	metaSubscriptionID  = "subscription_id"
)

// resource is one node this walk produced, before it becomes a contract node.
//
// IT CARRIES WALK FACTS THE WIRE DOES NOT. The five in-walk resolvers need
// facts about a resource that no metadata key exposes — an NSG's parsed rules,
// a load balancer's frontend IPs, a registry's login server. The internal
// collector's post-collection resolvers recover those by re-parsing each node's
// Content JSON after the graph is written; in-walk that round trip is both
// unnecessary and fragile, because the walk still holds the SDK struct when it
// emits. So the resolver inputs are carried HERE, in typed fields that
// [resource.node] drops, and nothing depends on decoding Content back.
type resource struct {
	id           string
	name         string
	resourceType string
	region       string
	// content is the curated API projection stored on the node for summary and
	// embedding. It is never read back by this collector.
	content  []byte
	metadata map[string]string

	// walk facts, dropped on the wire. Each names the resolver that reads it.
	nsgRules       []nsgRule // resolver 1
	lbFrontendIPs  []string  // resolver 2
	acrLoginServer string    // resolver 3
	containerImage string    // resolver 3
	principalID    string    // resolver 5
	tenantID       string    // resolver 4
}

// edge is one relationship this walk produced, before it becomes a contract
// edge. Endpoints are ARM resource ids (or this collector's own synthetic proxy
// ids); an edge naming an endpoint the walk did not enumerate is emitted
// unchanged, because endpoint resolution belongs to the write path.
type edge struct {
	from     string
	to       string
	relation string
	// metadata becomes the contract edge's Evidence, JSON-encoded. It is how
	// principal_type, issuer and role_source reach the resolvers and the graph.
	metadata map[string]string
	// method names how the edge was derived. The subcollectors leave it at
	// methodCloudCollect; each resolver stamps its own.
	method string
}

// methodCloudCollect is the Edge.Method every subcollector-emitted edge
// carrying metadata is stamped with. The resolvers use their own values so a
// consumer can tell a walked relationship from a derived one.
const methodCloudCollect = "cloud-collect"

// node converts a walked resource into the contract node. The ARM resource id
// is the node id VERBATIM and is never rewritten: it is the identity Azure
// itself uses, so a node collected twice from two directions converges.
func (r resource) node() framework.Node {
	n := framework.Node{
		ID:         r.id,
		Type:       nodeTypeCloudResource,
		SymbolName: r.name,
		Content:    string(r.content),
		Source:     sourceCloud,
		Summary:    summarize(r),
		Metadata:   map[string]string{metaResourceType: r.resourceType},
	}
	if r.region != "" {
		n.Metadata[metaRegion] = r.region
	}
	maps.Copy(n.Metadata, r.metadata)
	return n
}

// contractEdge converts a walked edge into the contract edge. Metadata is
// JSON-encoded into Evidence with SORTED KEYS: Go map iteration is randomized,
// and an Evidence string whose key order changed between two identical collects
// would read downstream as a changed edge.
func (e edge) contractEdge() framework.Edge {
	out := framework.Edge{FromID: e.from, ToID: e.to, Type: e.relation, Method: e.method}
	if len(e.metadata) == 0 {
		return out
	}
	if out.Method == "" {
		out.Method = methodCloudCollect
	}
	out.Evidence = encodeEvidence(e.metadata)
	return out
}

// encodeEvidence renders edge metadata as a deterministic JSON object.
// encoding/json already sorts map keys, so this is a thin wrapper whose job is
// to make the determinism a stated property rather than an implementation
// detail a later refactor to a struct could lose.
func encodeEvidence(md map[string]string) string {
	keys := make([]string, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	obj := make(map[string]string, len(md))
	for _, k := range keys {
		obj[k] = md[k]
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		// A map[string]string cannot fail to marshal. Returning the error text
		// rather than dropping the evidence keeps the failure visible in the
		// graph if the impossible ever happens.
		return `{"evidence_encode_error":"` + err.Error() + `"}`
	}
	return string(raw)
}

// proxy builds a PROXY resource: a node for something this collector referenced
// but could not enumerate. Every proxy states why it was not collected and what
// referenced it, so a reader of the graph can tell a gap in coverage from a gap
// in the source data.
func proxy(id, name, resourceType, reason, via, subscriptionID string) resource {
	return resource{
		id:           id,
		name:         name,
		resourceType: resourceType,
		metadata: map[string]string{
			metaCollected:       "false",
			metaCollectedReason: reason,
			metaDiscoveredVia:   via,
			metaSubscriptionID:  subscriptionID,
		},
	}
}

// entityResource builds a resource for a CHILD entity: one whose whole
// identity is its id, its name and its parent's region, and whose own
// properties are carried in its content rather than promoted to metadata.
//
// A child entity has no location of its own in ARM, so the region is its
// parent's; a child rendered with no region would be the only kind of node in
// the graph a region filter could never find.
func entityResource(id, name, resourceType, region string, body any) (resource, error) {
	content, err := marshalContent(body)
	if err != nil {
		return resource{}, fmt.Errorf("projecting %s %s: %w", resourceType, id, err)
	}
	return resource{
		id:           id,
		name:         name,
		resourceType: resourceType,
		region:       region,
		content:      content,
		metadata:     map[string]string{},
	}, nil
}

// marshalContent renders a curated projection as the node's Content. A
// projection that cannot marshal is a programming error in the projection type,
// so it is returned as an error rather than silently emitted as an empty node
// body.
func marshalContent(v any) ([]byte, error) {
	return json.Marshal(v)
}
