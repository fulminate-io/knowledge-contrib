// SPDX-License-Identifier: Apache-2.0

package ghgraph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// build.go — the walk's intermediate resources and relations, and the ONE place
// they become contract nodes and edges.
//
// WHY AN INTERMEDIATE SHAPE AT ALL. Every converter in this collector is a pure
// function from one API value to some of these, so a converter's whole behavior —
// the id, the kind, the metadata, the edges and their endpoints — is assertable
// with no network, no client and no framework. [Build] is the only code that
// knows the contract's field names.

// Resource is one thing this collector found.
type Resource struct {
	// ID is the graph id, built by one of the helpers in ids.go.
	ID string
	// Name is the display name.
	Name string
	// ResourceType is the kind, and it must be in the declared vocabulary:
	// [Build] refuses one that is not.
	ResourceType string
	// Content is the marshaled per-kind detail struct, carried verbatim.
	Content string
	// Metadata is the free-form domain data. `resource_type` and `provider` are
	// added by [Build] and must not be set here.
	Metadata map[string]string
}

// Relation is one relationship this collector found. Endpoints are ids.
type Relation struct {
	FromID string
	ToID   string
	Type   string
	// Evidence is the marshaled edge metadata, where an edge carries any. Only
	// one relationship in this graph does.
	Evidence string
}

// Result is what a subcollector found: everything it enumerated, in whatever
// order it enumerated it.
type Result struct {
	Resources []Resource
	Relations []Relation
}

// Add merges another result into this one. Order is NOT preserved and must not
// be relied on: the walk fans out concurrently, so the merge order is whatever
// the goroutines finished in, and [Build] sorts.
func (r *Result) Add(other Result) {
	r.Resources = append(r.Resources, other.Resources...)
	r.Relations = append(r.Relations, other.Relations...)
}

// edgeMethod is the `method` field on every edge this collector emits that
// carries evidence, naming how the edge was derived.
const edgeMethod = "cicd-collect"

// Build turns the walk's result into contract nodes and edges.
//
// IT SORTS AND DEDUPLICATES, AND BOTH ARE LOAD-BEARING RATHER THAN TIDY.
//
// SORTING is what makes two collects of an unchanged organization identical. The
// walk fans out over subcollectors concurrently, so an unsorted result carries
// whatever order the goroutines finished in and a diff between two collects of a
// source that did not change would show every node moving.
//
// DEDUPLICATION is what makes the three materialized node classes work at all. A
// label node is minted by every runner that carries the label, and a user node by
// every environment, run and deployment that names the person, so the same node is
// produced many times by construction; one node per distinct id is the intended
// result. Edges are deduplicated on the same terms, since a repeated edge says
// nothing a single one does not.
//
// AND THE SORT KEY IS THE WHOLE RESOURCE, NOT THE ID, WHICH IS WHAT MAKES THE
// DEDUPLICATION DETERMINISTIC. Sorting on the id alone leaves copies sharing an
// id in their INPUT order, and the input order is whatever the fan-out's
// goroutines finished in — so the surviving copy was decided by a race. That was
// invisible while every multiply-minted node was byte-identical to its twin, and
// it stopped being so when one person began arriving from a protection rule and
// from a run's actor: the two provider objects do not carry the same fields.
// Ordering on the rest of the resource makes the winner a function of what the
// provider said, and the copy carrying more of the provider's fields is the one
// that survives, because the shorter detail document is a prefix of the longer
// one and the comma that continues it sorts before the brace that ends it.
//
// EVERY FIELD, NOT MOST OF THEM. An ordering over a proper SUBSET of what
// distinguishes two copies is the same defect one field further in: a pair
// agreeing on the ordered fields compares equal and falls back to arrival order
// exactly as before. So [Resource]'s metadata is in the key too, rendered
// canonically, and a census asserts the field count the comparison was written
// against — the edge path has never had the gap, because [Relation] is compared
// on all four of its fields and deduplicated on the whole struct.
//
// IT REFUSES A RESOURCE TYPE OUTSIDE THE DECLARED VOCABULARY rather than passing
// it through. The declared list is a promise to a consumer; a type nothing
// declared would be a node no query in the world is looking for, and refusing it
// here is what turns the coverage assertion from a one-way superset check into an
// equality.
func Build(r Result) ([]framework.Node, []framework.Edge, error) {
	nodes, err := buildNodes(r.Resources)
	if err != nil {
		return nil, nil, err
	}
	edges, err := buildEdges(r.Relations)
	if err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

// orderedResource is a resource beside the canonical rendering of its metadata.
//
// THE RENDERING IS COMPUTED ONCE PER RESOURCE RATHER THAN ONCE PER COMPARISON.
// cmp.Or is an ordinary variadic call, so every argument is evaluated on every
// comparison the sort makes; rendering a map there would sort each resource's
// keys a logarithmic number of times instead of once.
type orderedResource struct {
	res      Resource
	metadata string
}

func buildNodes(resources []Resource) ([]framework.Node, error) {
	sorted := make([]orderedResource, 0, len(resources))
	for _, res := range resources {
		sorted = append(sorted, orderedResource{res: res, metadata: metadataOrder(res.Metadata)})
	}
	slices.SortStableFunc(sorted, func(a, b orderedResource) int {
		return cmp.Or(
			cmp.Compare(a.res.ID, b.res.ID),
			cmp.Compare(a.res.Content, b.res.Content),
			cmp.Compare(a.res.Name, b.res.Name),
			cmp.Compare(a.res.ResourceType, b.res.ResourceType),
			cmp.Compare(a.metadata, b.metadata),
		)
	})

	nodes := make([]framework.Node, 0, len(sorted))
	seen := make(map[string]bool, len(sorted))
	for _, entry := range sorted {
		res := entry.res
		if res.ID == "" {
			return nil, fmt.Errorf("github-actions: a %q resource named %q carries no id",
				res.ResourceType, res.Name)
		}
		if !IsDeclaredResourceType(res.ResourceType) {
			return nil, fmt.Errorf(
				"github-actions: resource %q carries the type %q, which is not in this collector's "+
					"declared vocabulary %v; a type nothing declared is a node no consumer queries for",
				res.ID, res.ResourceType, ResourceTypes())
		}
		if seen[res.ID] {
			continue
		}
		seen[res.ID] = true
		nodes = append(nodes, node(res))
	}
	return nodes, nil
}

// metadataOrder renders a resource's metadata as one comparable string: the keys
// sorted, each followed by its value.
//
// THE SEPARATOR IS A BYTE NO KEY OR VALUE CAN CONTAIN. Joining with a printable
// character would let two different maps render identically — a key `a` with the
// value `b=c` against a key `a=b` with the value `c` — which would put the pair
// back on arrival order for exactly the reason this rendering exists to remove.
func metadataOrder(metadata map[string]string) string {
	if len(metadata) == 0 {
		return ""
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var out strings.Builder
	for _, key := range keys {
		out.WriteString(key)
		out.WriteByte(0)
		out.WriteString(metadata[key])
		out.WriteByte(0)
	}
	return out.String()
}

func node(res Resource) framework.Node {
	meta := make(map[string]string, len(res.Metadata)+2)
	for k, v := range res.Metadata {
		meta[k] = v
	}
	meta["resource_type"] = res.ResourceType
	meta["provider"] = Provider
	return framework.Node{
		ID:         res.ID,
		Type:       NodeType,
		SymbolName: res.Name,
		Content:    res.Content,
		Summary:    Summarize(res),
		Source:     SourceTag,
		Metadata:   meta,
	}
}

func buildEdges(relations []Relation) ([]framework.Edge, error) {
	sorted := slices.Clone(relations)
	slices.SortStableFunc(sorted, func(a, b Relation) int {
		return cmp.Or(
			cmp.Compare(a.FromID, b.FromID),
			cmp.Compare(a.ToID, b.ToID),
			cmp.Compare(a.Type, b.Type),
			cmp.Compare(a.Evidence, b.Evidence),
		)
	})

	edges := make([]framework.Edge, 0, len(sorted))
	seen := make(map[Relation]bool, len(sorted))
	for _, rel := range sorted {
		if rel.FromID == "" || rel.ToID == "" {
			return nil, fmt.Errorf("github-actions: a %q relation names an empty endpoint (from=%q to=%q)",
				rel.Type, rel.FromID, rel.ToID)
		}
		if !IsDeclaredEdgeType(rel.Type) {
			return nil, fmt.Errorf(
				"github-actions: the relation %s -> %s carries the type %q, which is not in this "+
					"collector's declared vocabulary %v",
				rel.FromID, rel.ToID, rel.Type, EdgeTypes())
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		edge := framework.Edge{FromID: rel.FromID, ToID: rel.ToID, Type: rel.Type}
		if rel.Evidence != "" {
			edge.Evidence = rel.Evidence
			edge.Method = edgeMethod
		}
		// NEITHER SourceGraph NOR TargetGraph IS EVER SET. Every relationship
		// this collector emits joins two nodes of its own graph; a cross-graph
		// edge would be a new assertion about the operator's other graphs rather
		// than a reproduction of what the source provider already states, and
		// this collector makes none.
		edges = append(edges, edge)
	}
	return edges, nil
}
