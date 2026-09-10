// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"maps"
	"slices"
)

// context.go — the DECLARED FOREIGN-GRAPH CONTEXT a collect may carry, as the
// Go shape a walk receives it in.
//
// IT IS A COPY OF THE CLIENT'S SHAPE AND THAT IS THE DESIGN, not an oversight.
// The client's own types live in a client-internal package this module cannot
// import; the contract between the two sides is the checked-in JSON schema pair
// this module embeds a copy of, exactly as the Node and Edge shapes on the
// output side are. The json tags are the contract's spellings and they are what
// keep the two halves the same object.
//
// A COLLECTOR NEVER ASKS FOR THIS BLOCK AT RUN TIME. It is DECLARED, once, in
// the operator's config entry — which graph families the collector needs, which
// node types, which fields, which metadata keys — and the client fills it from
// its own graphs before the call. A collector whose entry declares nothing
// receives the zero value, which is the shape every collector written before
// this property existed sees and the reason adding it broke none of them.
//
// WHAT A COLLECTOR CAN CONCLUDE FROM AN EMPTY BLOCK: nothing about the operator's
// graphs unless its own entry declared the family. The block carries what was
// declared and only what was declared, so an absent field means "not declared",
// never "not present in the graph".
//
// IT IS KEYED BY GRAPH-TYPE NAME, NOT BY A FIXED SET OF FIELDS. It carried a
// `cloud` field and a `code` field until the built-in cloud collectors were
// removed; the families a client can supply are now `code` plus whatever graph
// types the operator has REGISTERED, which is not a set any struct in this
// module could enumerate. A collector reads the arm its own entry declared, by
// the name it declared it under.

// FamilyCode is the one family name this module can name as a constant: it is
// the only supplyable family that is not an operator-registered graph type.
const FamilyCode = "code"

// ForeignContext is the collect input's context block: the foreign-graph slices
// this collector's registration declared it needs, keyed by graph-type name.
//
// A DECLARED FAMILY IS ALWAYS PRESENT, even when the operator's store held no
// graph of that type: its value is an empty slice rather than a missing key. A
// missing key means the entry never asked, which is a different fact and one a
// collector is entitled to distinguish.
type ForeignContext map[string][]ForeignGraph

// ForeignGraph is one graph's declared slice.
//
// GraphName is always carried, even where the declaration asks for no nodes: a
// predicate that is a membership test over graph names needs the names and
// nothing else, and that is a legitimate declaration rather than an empty one.
// The two arrays carry no omitempty, matching the client's own shape: a corpus
// check guards those wire names against it on the OUTPUT side, and a struct on
// either side of the wire reads the same to a name-based gate.
type ForeignGraph struct {
	GraphName string        `json:"graph_name"`
	Nodes     []ForeignNode `json:"nodes"`
	Edges     []ForeignEdge `json:"edges"`
}

// ForeignNode is one node of a declared slice, carrying the declared fields and
// no others. A field the entry did not declare arrives as its zero value,
// because it was never sent.
type ForeignNode struct {
	ID         string            `json:"id,omitempty"`
	Type       string            `json:"type,omitempty"`
	SymbolName string            `json:"symbol_name,omitempty"`
	FilePath   string            `json:"file_path,omitempty"`
	Content    string            `json:"content,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ForeignEdge is one edge of a declared slice. Endpoints are node ids in the
// same graph.
type ForeignEdge struct {
	FromID string `json:"from_id,omitempty"`
	ToID   string `json:"to_id,omitempty"`
}

// IsEmpty reports whether this block carries no family at all, which is what a
// collector whose entry declares nothing receives.
func (f ForeignContext) IsEmpty() bool { return len(f) == 0 }

// Graphs returns the slice declared under one family name, or nil when the entry
// did not declare it.
//
// A NIL RETURN AND AN EMPTY SLICE MEAN DIFFERENT THINGS, and a caller that needs
// to tell them apart indexes the map directly with the two-value form. Graphs is
// for the common case, where a collector iterates whatever it declared and an
// undeclared family and an empty one both iterate zero times.
func (f ForeignContext) Graphs(family string) []ForeignGraph { return f[family] }

// Families returns every declared family name, sorted.
//
// SORTED because a collector that walks families and emits edges from them would
// otherwise emit them in a map's randomized order, which makes one unchanged
// input produce a different result each run.
func (f ForeignContext) Families() []string { return slices.Sorted(maps.Keys(f)) }

// Except returns every declared family's graphs EXCEPT those named, flattened,
// in sorted family order.
//
// IT EXISTS FOR THE COLLECTORS THAT CORRELATE ACROSS PROVIDERS. A log collector
// matches its streams against resource metadata (namespace, cluster, region) and
// does not care which provider's graph a resource came from — it declares every
// provider family it might correlate with, and wants them as one set. Naming the
// families to EXCLUDE rather than to include is what keeps that working when an
// operator registers a provider the collector's author never heard of.
func (f ForeignContext) Except(families ...string) []ForeignGraph {
	var out []ForeignGraph
	for _, name := range f.Families() {
		if slices.Contains(families, name) {
			continue
		}
		out = append(out, f[name]...)
	}
	return out
}
