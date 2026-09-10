// SPDX-License-Identifier: Apache-2.0

//! model.rs — the NODE and EDGE vocabulary of the collector contract, as the
//! Rust shapes a walk builds and the envelope serializes.
//!
//! THE FIELD NAMES ARE THE CONTRACT'S, NOT RUST'S. Every `serde(rename)` and
//! every `skip_serializing_if` below is the Go counterpart's json tag, read out
//! of `cmd/collectors/framework/framework.go:93-175`: `id` and `type` are always
//! emitted, the other fourteen node fields and seven of the nine edge fields are
//! omitted when empty. A collector author in any language reads
//! `contract/collector_output.schema.json`; these types are that file in Rust.
//!
//! SERVER-OWNED BOOKKEEPING FIELDS ARE DELIBERATELY ABSENT (created/updated/
//! tombstoned stamps, the collect epoch): the collect-write path stamps them, so
//! a collector cannot set them. Anything beyond the typed fields rides in
//! `metadata`.
//!
//! `deny_unknown_fields` IS ON BOTH TYPES AND IT IS LOAD-BEARING. A typo'd key
//! silently dropped is a collector author debugging an empty graph with no
//! message to go on; the client refuses one on its own side
//! (`DecodeResult`'s strict decode) and this crate refuses the same thing on the
//! collector's side, where the author can still fix it.
//!
//! METADATA IS A `BTreeMap` FOR DETERMINISM. Go's `encoding/json` sorts map keys
//! when it marshals; a `HashMap` here would emit a different byte order per run
//! and break the byte-equality the client's own conformance test asserts between
//! two transports.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::completeness::Completeness;

/// Node is one node a walk produced.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Node {
    pub id: String,
    #[serde(rename = "type")]
    pub node_type: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub symbol_name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub file_path: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub language: String,
    #[serde(default, skip_serializing_if = "is_zero_i64")]
    pub start_line: i64,
    #[serde(default, skip_serializing_if = "is_zero_i64")]
    pub end_line: i64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub content: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub signature: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub summary: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub source: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub keywords: String,
    #[serde(default, skip_serializing_if = "is_false")]
    pub is_exported: bool,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub metadata: BTreeMap<String, String>,
}

impl Node {
    /// new builds the minimal conforming node: an id and a type, every optional
    /// field absent. It is the shape a walk starts from.
    pub fn new(id: impl Into<String>, node_type: impl Into<String>) -> Self {
        Node {
            id: id.into(),
            node_type: node_type.into(),
            ..Node::default()
        }
    }
}

/// Edge is one edge a walk produced. Endpoints are referenced by node id.
///
/// AN IN-GRAPH EDGE IS PASSED THROUGH UNTOUCHED, including one naming an
/// endpoint this result does not carry. That is not an oversight and a collector
/// must not "fix" it: the client converts a dangling edge deliberately, and the
/// write path resolves no endpoint.
///
/// AN EDGE INTO ANOTHER GRAPH IS THE ONE THING THAT IS NOT PASSED THROUGH. Set
/// [`Edge::target_graph`] and the client resolves the endpoint against that
/// graph family, materializes a proxy and links the edge into the LINKAGE graph;
/// leave it empty and the edge is an ordinary edge of this collect's own graph.
///
/// EITHER END MAY BE THE FOREIGN ONE. [`Edge::source_graph`] is the mirror: it
/// names the family `from_id` lives in. SET AT MOST ONE OF THE TWO — an edge
/// naming BOTH is REFUSED by the collect, because one resolution reaches one
/// foreign family. That refusal is the CLIENT's, per the output schema's own
/// description, and this crate does not duplicate it.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Edge {
    pub from_id: String,
    pub to_id: String,
    #[serde(rename = "type")]
    pub edge_type: String,
    #[serde(default, skip_serializing_if = "is_zero_f64")]
    pub weight: f64,
    #[serde(default, skip_serializing_if = "is_zero_f64")]
    pub confidence: f64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub method: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub evidence: String,
    /// source_graph is the GRAPH FAMILY `from_id` lives in — a graph type
    /// ("code", or a registered custom family), never a graph instance. Empty is
    /// the in-graph case and serializes away, so an edge that does not set it is
    /// byte-identical to one written before this field existed.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub source_graph: String,
    /// target_graph is the GRAPH FAMILY `to_id` lives in. Same rules as
    /// [`Edge::source_graph`]; set at most one of the two.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub target_graph: String,
}

impl Edge {
    /// new builds the minimal conforming edge: two endpoints and a type.
    pub fn new(
        from_id: impl Into<String>,
        to_id: impl Into<String>,
        edge_type: impl Into<String>,
    ) -> Self {
        Edge {
            from_id: from_id.into(),
            to_id: to_id.into(),
            edge_type: edge_type.into(),
            ..Edge::default()
        }
    }
}

/// WalkResult is one walk's output.
///
/// IT IS NAMED `WalkResult` RATHER THAN `Result`, which is the Go framework's
/// name for it, because `Result` in Rust is the standard library's and shadowing
/// it in a public API would make every collector's signatures ambiguous to read.
/// The rename is the only place this crate departs from the Go names for a
/// reason that is purely Rust's.
#[derive(Debug, Clone, PartialEq)]
pub struct WalkResult {
    pub nodes: Vec<Node>,
    pub edges: Vec<Edge>,
    /// complete is the walk's own assertion. It has no default: see
    /// [`Completeness`].
    pub complete: Completeness,
}

impl WalkResult {
    /// new builds a result from its three parts.
    pub fn new(nodes: Vec<Node>, edges: Vec<Edge>, complete: Completeness) -> Self {
        WalkResult {
            nodes,
            edges,
            complete,
        }
    }
}

fn is_zero_i64(v: &i64) -> bool {
    *v == 0
}

fn is_zero_f64(v: &f64) -> bool {
    *v == 0.0
}

fn is_false(v: &bool) -> bool {
    !*v
}
