// SPDX-License-Identifier: Apache-2.0

//! foreign_context.rs — the four rules of the DECLARED FOREIGN-GRAPH CONTEXT.
//!
//! Row R1.12. The distinction the first two rows pin is the one a collector is
//! entitled to make and that a naive port loses: a declared family that matched
//! no graph arrives as a PRESENT key holding an EMPTY array, and an undeclared
//! family has NO KEY AT ALL.

use knowledge_collector_framework::context::{ForeignContext, ForeignGraph};
use serde_json::json;

fn decoded(block: serde_json::Value) -> ForeignContext {
    serde_json::from_value(block).expect("a context block decodes")
}

// ---------------------------------------------------------------------------
// R1.12 (a) and (b) — a declared family with no matching graph is PRESENT and
// empty; an undeclared family has no key. The two are distinguishable through
// the crate's accessor.
// ---------------------------------------------------------------------------
#[test]
fn a_declared_family_with_no_graphs_is_distinguishable_from_an_undeclared_one() {
    let block = decoded(json!({
        "code": [],
        "acme-aws": [{"graph_name": "prod", "nodes": [], "edges": []}]
    }));

    assert_eq!(
        block.graphs("code").map(<[ForeignGraph]>::len),
        Some(0),
        "a DECLARED family that matched no graph is a present key holding an empty slice"
    );
    assert!(block.declared("code"));

    assert_eq!(
        block.graphs("logs"),
        None,
        "an UNDECLARED family has no key at all, which is a different fact from an empty one"
    );
    assert!(!block.declared("logs"));

    assert_eq!(block.graphs("acme-aws").map(<[ForeignGraph]>::len), Some(1));
}

// ---------------------------------------------------------------------------
// R1.12 (c) — a collector declaring nothing receives no context key, and the
// walk sees an empty block rather than an absent one.
// ---------------------------------------------------------------------------
#[test]
fn a_collector_declaring_nothing_receives_an_empty_block() {
    let block = ForeignContext::default();
    assert!(block.is_empty());
    assert!(block.families().is_empty());
    assert_eq!(block.graphs("code"), None);
    assert!(block.except(&[]).is_empty());
}

// ---------------------------------------------------------------------------
// R1.12 (d) — families() is SORTED. The reason is determinism: a collector that
// walks families and emits edges from them would otherwise produce a different
// result each run from one unchanged input.
// ---------------------------------------------------------------------------
#[test]
fn families_are_sorted() {
    let block = decoded(json!({"zeta": [], "code": [], "acme-aws": [], "middle": []}));
    assert_eq!(
        block.families(),
        vec!["acme-aws", "code", "middle", "zeta"],
        "families must be sorted, or one unchanged input produces a different result each run"
    );
}

// ---------------------------------------------------------------------------
// except() returns every declared family's graphs EXCEPT those named, flattened,
// in sorted family order. It is what a cross-provider correlating collector
// uses, and naming the families to EXCLUDE is what keeps it working when an
// operator registers a provider the author never heard of.
// ---------------------------------------------------------------------------
#[test]
fn except_flattens_every_family_but_the_named_ones_in_sorted_order() {
    let block = decoded(json!({
        "zeta":     [{"graph_name": "z1", "nodes": [], "edges": []}],
        "code":     [{"graph_name": "c1", "nodes": [], "edges": []}],
        "acme-aws": [{"graph_name": "a1", "nodes": [], "edges": []},
                     {"graph_name": "a2", "nodes": [], "edges": []}]
    }));

    let names: Vec<&str> = block
        .except(&["code"])
        .into_iter()
        .map(|g| g.graph_name.as_str())
        .collect();
    assert_eq!(names, vec!["a1", "a2", "z1"]);

    // THE CONTROL: excluding nothing returns every graph, so the row above shows
    // an exclusion rather than a filter that drops everything.
    let all: Vec<&str> = block
        .except(&[])
        .into_iter()
        .map(|g| g.graph_name.as_str())
        .collect();
    assert_eq!(all, vec!["a1", "a2", "c1", "z1"]);
}

// ---------------------------------------------------------------------------
// A block carrying a key this crate's types do not name is DECODED rather than
// refused. That is the opposite posture from the result vocabulary, and it is
// deliberate: this block travels client-to-collector, so refusing an unknown key
// would break every deployed collector on the day the client adds one.
// ---------------------------------------------------------------------------
#[test]
fn an_unknown_key_inside_the_block_does_not_refuse_the_call() {
    let block = decoded(json!({
        "code": [{"graph_name": "c1", "nodes": [{"id": "n", "type": "func", "unheard_of": 1}], "edges": []}]
    }));
    let graphs = block.graphs("code").expect("code is declared");
    assert_eq!(graphs[0].nodes[0].id, "n");
    assert_eq!(graphs[0].nodes[0].node_type, "func");
}

// ---------------------------------------------------------------------------
// A declared field the entry did not ask for arrives empty, because it was never
// sent — never as a claim about the operator's graph.
// ---------------------------------------------------------------------------
#[test]
fn an_undeclared_field_arrives_empty() {
    let block = decoded(json!({
        "code": [{"graph_name": "c1", "nodes": [{"id": "n", "type": "func"}], "edges": []}]
    }));
    let node = &block.graphs("code").expect("code is declared")[0].nodes[0];
    assert_eq!(node.file_path, "");
    assert_eq!(node.content, "");
    assert!(node.metadata.is_empty());
}
