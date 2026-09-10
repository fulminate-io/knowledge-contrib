// SPDX-License-Identifier: Apache-2.0

// Package framework is the common MCP serving and contract layer every custom
// knowledge collector is built on. A collector author writes a WALK and a
// params type; this package serves that walk as an MCP tool over stdio and over
// streamable HTTP, advertises the collector contract's input and output
// schemas, validates the call's params before the walk runs, and encodes the
// walk's result as the contract envelope.
//
// THE DIVISION OF LABOR IS THE POINT. Nothing about MCP, JSON Schema or the
// envelope is visible to a collector: it implements [Collector] and calls one
// of the two entry points, [ServeStdio] or [ServeHTTP]. A collector module that
// carries MCP server code or envelope-encoding code of its own has re-derived
// something this package already settled, and the eight in-tree collectors
// disagreed on three of those settlements before it existed.
//
// THIS PACKAGE IS NOT THE CLIENT'S. The knowledge client's own consumer side
// (dial, verify, call, convert) lives under cmd/knowledge/internal and is
// unimportable from here by construction; the contract between the two is the
// checked-in JSON schema pair this package embeds a copy of, never a shared Go
// package. See schema.go for what that costs and how the copy is kept honest.
package framework

import "context"

// ToolSpec names the single MCP tool a collector serves. The name is the
// collector's, not a constant: the config-file entry that registers a collector
// carries a `tool` field naming the one tool the daemon calls, and an operator
// is free to serve `collect_logs` beside another provider's `collect`.
type ToolSpec struct {
	// Name is the served tool's name. Empty means [DefaultToolName].
	Name string
	// Description is the tool's human-readable description, shown in an MCP
	// tool listing. Empty is allowed; a collector that means to be installed by
	// a human should write one.
	Description string
}

// DefaultToolName is the tool name a collector serves when its [ToolSpec]
// names none. It matches the `tool` value in the config-file entry's own
// worked example.
const DefaultToolName = "collect"

// DescribeToolName is the REQUIRED second tool every collector serves, and it
// is a constant rather than a [ToolSpec] field for a reason that is mechanical:
// the client must know a tool's name before it can call any tool on a provider,
// so the one thing a declaration cannot supply is the name of the tool that
// supplies it. The entry's `tool` field names the COLLECT tool only.
const DescribeToolName = "describe"

// Collector is the whole surface a collector author implements. P is the
// collector's params type: the framework infers its JSON Schema, advertises it
// inside the contract's input schema, and hands the walk an ALREADY-VALIDATED
// value of it — a call whose params do not satisfy the advertised schema is
// refused before Walk is reached.
//
// The `id` Walk receives is the collect id, which names the graph INSTANCE the
// result lands in. It is NOT the graph family: the family is the registration
// name, which the client derives on its own side and never sends, so a
// collector cannot choose the graph type it writes into.
type Collector[P any] interface {
	// Tool names the MCP tool this collector serves.
	Tool() ToolSpec
	// Describe returns what this collector IS: its suggested behavior, the node
	// and edge types it emits, the environment variables it reads with their
	// class, and the foreign-graph context it needs. The framework serves it on
	// the required [DescribeToolName] tool, `knowledge collector add` writes the
	// registration entry from it, and the installer derives its per-collector
	// tables from it.
	//
	// IT IS A METHOD RATHER THAN A FIELD ON [ToolSpec] for the same reason Walk's
	// foreign block is a parameter: a collector must be unable to serve without
	// answering. A field on a struct is omittable by writing nothing, and a
	// collector that omitted it would advertise a describe tool returning the
	// zero declaration — an empty vocabulary, which refuses every node it then
	// emits, discovered at the operator's first collect rather than at the
	// author's first build. [NewServer] refuses an invalid declaration instead.
	//
	// THE TWO LLM AXES ARE A SUGGESTION. Declare what the collector is worth;
	// the operator's flags decide what is paid for.
	Describe() Declaration
	// Walk enumerates the source and returns what it found, together with the
	// completeness assertion for this walk. An error return becomes a tool-call
	// error naming the collector and the cause; it never becomes an empty
	// successful result.
	//
	// foreign is the DECLARED FOREIGN-GRAPH CONTEXT: the cloud resources or
	// code-graph nodes this collector's registration entry declared it needs, read
	// out of the operator's own graphs by the client and sent with the call. It is
	// the ZERO VALUE for a collector whose entry declares nothing, which is what
	// every collector written before this parameter existed sees.
	//
	// IT IS A FOURTH PARAMETER RATHER THAN A FIELD ON A REQUEST STRUCT, and the
	// choice is worth stating because it is the one this seam had. A request
	// struct would let the framework add inputs later without moving this
	// signature; a parameter makes the block impossible to receive by accident
	// and impossible to ignore by omission, and every collector this module
	// exists for needs it. A collector with no use for the block names it `_`.
	Walk(ctx context.Context, id string, params P, foreign ForeignContext) (Result, error)
}

// Result is one walk's output. Complete has no usable zero value: see
// [Completeness].
type Result struct {
	Nodes    []Node
	Edges    []Edge
	Complete Completeness
}

// Node is one node a walk produced. The field set and the json tags are the Go
// counterpart of the contract output schema's node shape, which is the artifact
// a collector author in another language reads.
//
// Server-owned bookkeeping fields (created/updated/tombstoned stamps, the
// collect epoch) are deliberately absent: the collect-write path stamps them,
// so a collector cannot set them. Anything beyond the typed fields rides in
// Metadata, exactly as the built-in collectors do.
type Node struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	SymbolName  string            `json:"symbol_name,omitempty"`
	FilePath    string            `json:"file_path,omitempty"`
	Language    string            `json:"language,omitempty"`
	StartLine   int               `json:"start_line,omitempty"`
	EndLine     int               `json:"end_line,omitempty"`
	Content     string            `json:"content,omitempty"`
	Signature   string            `json:"signature,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Source      string            `json:"source,omitempty"`
	Status      string            `json:"status,omitempty"`
	Keywords    string            `json:"keywords,omitempty"`
	IsExported  bool              `json:"is_exported,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Edge is one edge a walk produced. Endpoints are referenced by node id.
//
// AN IN-GRAPH EDGE IS PASSED THROUGH UNTOUCHED, including one naming an
// endpoint this result does not carry. That is not an oversight and a collector
// must not "fix" it: the client converts a dangling edge deliberately, and
// THE WRITE PATH RESOLVES NO ENDPOINT — the server stores from_id and to_id
// verbatim, with no lookup and no proxy materialized on its side.
//
// AN EDGE INTO ANOTHER GRAPH IS THE ONE THING THAT IS NOT PASSED THROUGH. Set
// TargetGraph and the client resolves the endpoint against that graph family,
// materializes a proxy, and links the edge into the LINKAGE graph; leave it
// empty and the edge is an ordinary edge of this collect's own graph. The two
// are different graphs and different shapes, which is why one field decides it
// rather than the client guessing from the endpoint.
//
// EITHER END MAY BE THE FOREIGN ONE. SourceGraph is TargetGraph's mirror: it
// names the family FromID lives in, for a relationship whose far endpoint is the
// source. Set exactly one of the two. An edge naming BOTH is REFUSED by the
// collect, because one resolution reaches one foreign family — the client
// enumerates a single family per edge and locates a single endpoint in it — so
// an edge foreign at both ends names something this contract cannot resolve.
//
// THE BUILT-IN COLLECTORS DO NOT ALL EMIT CROSS-GRAPH EDGES THE SAME WAY, so
// reading one of them and generalizing will mislead you. The cross-graph edges
// that exist in this tree are emitted by the post-collect linker rather than by
// any collector's result, and the built-in log collector's proxy is an
// OWN-GRAPH node that never reaches the linkage graph at all. Use TargetGraph
// for a cross-graph endpoint; emit a proxy node plus a plain edge to it when
// you want the proxy in your own graph.
type Edge struct {
	FromID     string  `json:"from_id"`
	ToID       string  `json:"to_id"`
	Type       string  `json:"type"`
	Weight     float64 `json:"weight,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Method     string  `json:"method,omitempty"`
	Evidence   string  `json:"evidence,omitempty"`

	// SourceGraph is the GRAPH FAMILY FromID lives in — a graph type ("code", or
	// a registered custom family such as "acme-aws"), never a graph instance: the
	// client enumerates that family's loaded graphs and locates the endpoint
	// among them, so the instance is derived rather than asserted here. Empty is
	// the in-graph case and marshals away, so an edge that does not set it is
	// byte-identical to one written before this field existed.
	//
	// IT IS THE FIELD FOR A RELATIONSHIP WHOSE FAR END IS THE SOURCE — a chart
	// file that deploys a workload, a build that produced an artifact. Reversing
	// such an edge to fit TargetGraph asserts a different relationship, and
	// emitting it with no family at all lands an in-graph edge to an id nothing
	// in this collect's graph resolves. Set this instead of doing either.
	//
	// SET AT MOST ONE OF SourceGraph AND TargetGraph; see the type's doc comment.
	SourceGraph string `json:"source_graph,omitempty"`

	// TargetGraph is the GRAPH FAMILY ToID lives in — a graph type ("logs", or a
	// registered custom family such as "acme-aws"), never a graph instance: the
	// client enumerates that family's loaded graphs and locates the endpoint
	// among them, so the instance is derived rather than asserted here. Empty is
	// the in-graph case and marshals away, so an edge that does not set it is
	// byte-identical to one written before this field existed.
	//
	// SET AT MOST ONE OF SourceGraph AND TargetGraph; see the type's doc comment.
	TargetGraph string `json:"target_graph,omitempty"`
}
