// SPDX-License-Identifier: Apache-2.0

package gcpgraph

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// build.go — the ONE crossing from what a walk found to what the contract
// carries, and the two properties that live here because nowhere else can hold
// them.
//
// FIRST, THE VOCABULARY IS ENFORCED HERE. A converter that invents a resource or
// edge type — a typo, a copied literal, a type this collector has no business
// emitting — is refused at build time naming the value. Enforcing it here rather
// than trusting each converter means the parity floor is a gate and not a
// document: the emitted set is a subset of the floor by construction, and the
// walk's own superset row closes the other direction.
//
// SECOND, THE OUTPUT IS SORTED. The walk fans out concurrently, so the order
// results merge in is not stable between two runs of the same source. A
// re-collect of an unchanged project must produce a byte-identical result or the
// carry-forward diff reports a change the source did not make. Sorting is what
// makes "unchanged" observable, and it is cheap at this cardinality.

// nodeSource is stamped on every node this collector produces. It names the
// producer for a consumer reading a mixed graph.
const nodeSource = "gcp"

// relationMethod is stamped on an edge that carries derivation metadata but no
// method of its own. A derived edge names its own resolver instead.
const relationMethod = "gcp-collect"

// Resource is one thing a walk found, before it becomes a contract node. It is
// the intermediate the enumerations produce and the resolvers read, so a
// resolver can reach Content and Metadata without a JSON round trip through the
// wire shape.
type Resource struct {
	// ID is the provider's own identifier, used as the node id unmodified.
	ID string
	// Name is the human-readable display name.
	Name string
	// ResourceType classifies the resource and must be one of [ResourceTypes].
	ResourceType string
	// Region is the region or zone the resource lives in, when it has one.
	Region string
	// Summary overrides the generated one-line summary. Empty takes the default.
	Summary string
	// Content is the API's own JSON for this resource, preserved verbatim.
	Content []byte
	// Metadata is the classification a consumer queries on.
	Metadata map[string]string
}

// Relation is one directed relationship a walk found or a resolver derived.
type Relation struct {
	// From and To are node ids. An endpoint this walk did not enumerate is
	// legitimate and is passed through: resolution belongs to the write path.
	From string
	To   string
	// Type is the edge type and must be one of [EdgeTypes].
	Type string
	// Method names how the edge was derived. Empty takes [relationMethod] when
	// Metadata is present, and stays empty otherwise.
	Method string
	// Metadata describes how the relationship was established. When non-empty it
	// is serialized as the edge's evidence.
	Metadata map[string]string
}

// Result is what one enumeration or one resolver produced.
type Result struct {
	Resources []Resource
	Relations []Relation
}

// Add merges another result into this one.
func (r *Result) Add(other Result) {
	r.Resources = append(r.Resources, other.Resources...)
	r.Relations = append(r.Relations, other.Relations...)
}

// Build turns a walk's accumulated result into contract nodes and edges, in a
// deterministic order, refusing anything outside the parity vocabulary.
func Build(r Result) ([]framework.Node, []framework.Edge, error) {
	// THE RICHER EMISSION OF AN ID WINS, and it is NOT the first one.
	//
	// Two enumerations legitimately emit the same id: one reads the resource,
	// another merely REFERENCES it and emits a placeholder saying so. The id is
	// the identity, so the second is the same node rather than a conflict — but
	// "first wins" makes the surviving node depend on which enumeration finished
	// first, and the walk fans out concurrently. Two collects of an unchanged
	// project would then differ, and a diff keyed on node content would report a
	// change the source did not make.
	byID := make(map[string]framework.Node, len(r.Resources))
	for i, res := range r.Resources {
		node, err := buildNode(i, res)
		if err != nil {
			return nil, nil, err
		}
		if existing, dup := byID[node.ID]; dup && richer(existing, node) {
			continue
		}
		byID[node.ID] = node
	}
	nodes := make([]framework.Node, 0, len(byID))
	for _, node := range byID {
		nodes = append(nodes, node)
	}

	edges := make([]framework.Edge, 0, len(r.Relations))
	for i, rel := range r.Relations {
		edge, err := buildEdge(i, rel)
		if err != nil {
			return nil, nil, err
		}
		edges = append(edges, edge)
	}

	slices.SortStableFunc(nodes, func(a, b framework.Node) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortStableFunc(edges, compareEdges)
	return nodes, edges, nil
}

// MetadataCollected is the key a node carries when it was REFERENCED by another
// resource rather than read in its own right. A referenced placeholder has an id
// and little else; the enumeration that actually reads the resource emits the
// same id with everything.
const MetadataCollected = "collected"

// richer reports whether keeping `a` over `b` loses nothing.
//
// IT IS A TOTAL ORDER ON PURPOSE, not a preference. Any rule with a tie that
// falls back to arrival order reintroduces exactly the defect this exists to
// fix, so the comparison runs to a decision on every pair:
//
//  1. a node that was READ beats one that was merely REFERENCED;
//  2. then the one carrying more content, because content is the resource's own
//     configuration and the placeholder has none;
//  3. then the greater rendering of the whole node, which is arbitrary as a
//     PREFERENCE and is not arbitrary as a RULE: it depends on the two values
//     and on nothing else, so two collects that produced the same pair keep the
//     same one whatever order they arrived in.
func richer(a, b framework.Node) bool {
	aRead, bRead := !referencedOnly(a), !referencedOnly(b)
	if aRead != bRead {
		return aRead
	}
	if len(a.Content) != len(b.Content) {
		return len(a.Content) > len(b.Content)
	}
	return canonical(a) >= canonical(b)
}

// canonical renders a node into a stable string for the final tiebreak. Encoding
// a map sorts its keys, so the rendering depends on the node's contents and not
// on how the map was built.
func canonical(n framework.Node) string {
	raw, err := json.Marshal(n)
	if err != nil {
		// A node is a struct of strings and a string map, so this cannot fail.
		// Falling back to the id keeps the comparison total rather than
		// panicking on a case that does not arise.
		return n.ID
	}
	return string(raw)
}

// referencedOnly reports whether a node says it was referenced rather than read.
func referencedOnly(n framework.Node) bool {
	return n.Metadata[MetadataCollected] == "false"
}

// compareEdges orders edges on every field that distinguishes two of them. A
// firewall rule emits several edges between the same pair whose only difference
// is the evidence, so an order keyed on the endpoints alone would still vary.
func compareEdges(a, b framework.Edge) int {
	if c := cmp.Compare(a.FromID, b.FromID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ToID, b.ToID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Type, b.Type); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Method, b.Method); c != 0 {
		return c
	}
	return cmp.Compare(a.Evidence, b.Evidence)
}

func buildNode(i int, res Resource) (framework.Node, error) {
	if res.ID == "" {
		return framework.Node{}, fmt.Errorf(
			"gcp collector: resource[%d] of type %q has an empty id; the id is what every edge joins on",
			i, res.ResourceType)
	}
	if res.ResourceType == "" {
		return framework.Node{}, fmt.Errorf(
			"gcp collector: resource[%d] (id=%q) has an empty resource type", i, res.ID)
	}
	if !slices.Contains(resourceTypeFloor, res.ResourceType) {
		return framework.Node{}, fmt.Errorf(
			"gcp collector: resource[%d] (id=%q) carries resource type %q, which is not in the resource-type floor",
			i, res.ID, res.ResourceType)
	}

	meta := make(map[string]string, len(res.Metadata)+2)
	for k, v := range res.Metadata {
		meta[k] = v
	}
	// resource_type is written LAST of the two so a converter cannot shadow the
	// classification a consumer queries on with a metadata key of the same name.
	if res.Region != "" {
		meta["region"] = res.Region
	}
	meta["resource_type"] = res.ResourceType

	return framework.Node{
		ID:         res.ID,
		Type:       res.ResourceType,
		SymbolName: res.Name,
		Content:    string(res.Content),
		Summary:    summarize(res),
		Source:     nodeSource,
		Metadata:   meta,
	}, nil
}

// summarize builds the one-line summary a search index reads when a converter
// supplies none. It names the type and the resource, and the region when there
// is one, because a bucket name alone tells a reader nothing about what it is.
func summarize(res Resource) string {
	if res.Summary != "" {
		return res.Summary
	}
	name := res.Name
	if name == "" {
		name = res.ID
	}
	if res.Region != "" {
		return res.ResourceType + " " + name + " in " + res.Region
	}
	return res.ResourceType + " " + name
}

func buildEdge(i int, rel Relation) (framework.Edge, error) {
	switch {
	case rel.From == "":
		return framework.Edge{}, fmt.Errorf(
			"gcp collector: relation[%d] of type %q has an empty from_id", i, rel.Type)
	case rel.To == "":
		return framework.Edge{}, fmt.Errorf(
			"gcp collector: relation[%d] of type %q (from=%q) has an empty to_id", i, rel.Type, rel.From)
	case rel.Type == "":
		return framework.Edge{}, fmt.Errorf(
			"gcp collector: relation[%d] (from=%q to=%q) has an empty edge type", i, rel.From, rel.To)
	case !slices.Contains(edgeTypeFloor, rel.Type):
		return framework.Edge{}, fmt.Errorf(
			"gcp collector: relation[%d] (from=%q to=%q) carries edge type %q, which is not in the edge-type floor",
			i, rel.From, rel.To, rel.Type)
	}

	edge := framework.Edge{
		FromID: rel.From,
		ToID:   rel.To,
		Type:   rel.Type,
		Method: rel.Method,
	}
	if len(rel.Metadata) > 0 {
		raw, err := json.Marshal(rel.Metadata)
		if err != nil {
			// A map[string]string cannot fail to marshal, so reaching here means
			// something about the value is not what this signature says it is.
			return framework.Edge{}, fmt.Errorf(
				"gcp collector: relation[%d] (from=%q to=%q): encoding edge evidence: %w",
				i, rel.From, rel.To, err)
		}
		edge.Evidence = string(raw)
		if edge.Method == "" {
			edge.Method = relationMethod
		}
	}
	return edge, nil
}
