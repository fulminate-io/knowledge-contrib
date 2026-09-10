// SPDX-License-Identifier: Apache-2.0

package bbgraph

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// build.go — the walk's intermediate resources and relations, and the ONE place
// they become contract nodes and edges.
//
// WHY AN INTERMEDIATE SHAPE AT ALL. Every converter in this collector is a pure
// function from one API value to some of these, so a converter's whole
// behavior — the id, the kind, the metadata, the edges and their endpoints — is
// assertable with no network, no client and no framework. [Build] is the only
// code that knows the contract's field names.

// Resource is one thing this collector found.
type Resource struct {
	// ID is the graph id, built by one of the helpers in ids.go.
	ID string
	// Name is the display name.
	Name string
	// ResourceType is the kind, and it must be in the declared vocabulary:
	// [Build] refuses one that is not.
	ResourceType string
	// Content is the marshaled per-kind detail struct, carried verbatim. Two
	// resource types carry none: a label and an approval gate are a name and a
	// scope, and the source provider stored no content for either.
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
}

// Result is what a subcollector found: everything it enumerated, in whatever
// order it enumerated it.
type Result struct {
	Resources []Resource
	Relations []Relation
	// SecretRefs are the variable references a pipeline definition's steps name,
	// carried UNRESOLVED out of the enumeration that read them. They become
	// USES_SECRET relations only after the whole walk has finished, because the
	// variables that could satisfy them are read by a different enumeration
	// running in parallel with this one. See [ResolveSecretRefs].
	SecretRefs []SecretRef
	// StepTargets are the environments and runner labels a pipeline definition's
	// steps NAME, carried UNRESOLVED for the same reason: a step's `deployment`
	// and its `runs-on` come out of the repository's YAML, while the environments
	// and the runners come from two other enumerations running in parallel with
	// the one that read them. See [ResolveStepTargets].
	StepTargets []StepTarget
}

// Add merges another result into this one. Order is NOT preserved and must not
// be relied on: the walk fans out concurrently, so the merge order is whatever
// the goroutines finished in, and [Build] sorts.
func (r *Result) Add(other Result) {
	r.Resources = append(r.Resources, other.Resources...)
	r.Relations = append(r.Relations, other.Relations...)
	r.SecretRefs = append(r.SecretRefs, other.SecretRefs...)
	r.StepTargets = append(r.StepTargets, other.StepTargets...)
}

// Build turns the walk's result into contract nodes and edges.
//
// IT SORTS AND DEDUPLICATES, AND BOTH ARE LOAD-BEARING RATHER THAN TIDY.
//
// SORTING is what makes two collects of an unchanged workspace identical. The
// walk fans out over subcollectors concurrently and the YAML parse ranges a Go
// map, so an unsorted result carries an order that differs between two runs over
// byte-identical input, and a diff between two collects of a source that did not
// change would show every node moving.
//
// DEDUPLICATION is what makes the label node class work at all. A label node is
// minted by every runner that carries the label, so the same node is produced
// many times by construction; one node per distinct id is the intended result,
// and deduplicating after the sort makes which copy survives deterministic too.
// Edges are deduplicated on the same terms, since a repeated edge says nothing a
// single one does not.
//
// IT REFUSES A RESOURCE TYPE OUTSIDE THE DECLARED VOCABULARY rather than passing
// it through. The declared list is a promise to a consumer; a type nothing
// declared would be a node no query in the world is looking for, and refusing it
// here is what turns the coverage assertion from a one-way superset check into
// an equality.
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

func buildNodes(resources []Resource) ([]framework.Node, error) {
	sorted := slices.Clone(resources)
	slices.SortStableFunc(sorted, func(a, b Resource) int { return cmp.Compare(a.ID, b.ID) })

	nodes := make([]framework.Node, 0, len(sorted))
	seen := make(map[string]bool, len(sorted))
	for _, res := range sorted {
		if res.ID == "" {
			return nil, fmt.Errorf("bitbucket-pipelines: a %q resource named %q carries no id",
				res.ResourceType, res.Name)
		}
		if !IsDeclaredResourceType(res.ResourceType) {
			return nil, fmt.Errorf(
				"bitbucket-pipelines: resource %q carries the type %q, which is not in this "+
					"collector's declared vocabulary %v; a type nothing declared is a node no "+
					"consumer queries for",
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
		)
	})

	edges := make([]framework.Edge, 0, len(sorted))
	seen := make(map[Relation]bool, len(sorted))
	for _, rel := range sorted {
		if rel.FromID == "" || rel.ToID == "" {
			return nil, fmt.Errorf(
				"bitbucket-pipelines: a %q relation names an empty endpoint (from=%q to=%q)",
				rel.Type, rel.FromID, rel.ToID)
		}
		if !IsDeclaredEdgeType(rel.Type) {
			return nil, fmt.Errorf(
				"bitbucket-pipelines: the relation %s -> %s carries the type %q, which is not in "+
					"this collector's declared vocabulary %v",
				rel.FromID, rel.ToID, rel.Type, EdgeTypes())
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		// NEITHER SourceGraph NOR TargetGraph IS EVER SET. Every relationship this
		// collector emits joins two nodes of its own graph; a cross-graph edge
		// would be a new assertion about the operator's other graphs rather than a
		// reproduction of what the source provider already states, and this
		// collector makes none.
		edges = append(edges, framework.Edge{FromID: rel.FromID, ToID: rel.ToID, Type: rel.Type})
	}
	return edges, nil
}
