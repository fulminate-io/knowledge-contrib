// SPDX-License-Identifier: Apache-2.0

//! envelope.rs — the three WIRE-SHAPING rules and the envelope's refusals.
//!
//! Rows R1.5, R1.6 and R1.7. Every assertion here is over the SERIALIZED BYTES
//! rather than over the Rust value, because the properties are about what
//! reaches the wire and a Rust value cannot show them.

use knowledge_collector_framework::completeness::Completeness;
use knowledge_collector_framework::envelope::encode_result;
use knowledge_collector_framework::model::{Edge, Node, WalkResult};

fn encoded(result: WalkResult) -> String {
    let out = encode_result("collect", result).expect("the result encodes");
    serde_json::to_string(&out).expect("the envelope serializes")
}

// ---------------------------------------------------------------------------
// R1.5 — every list serializes as an ARRAY, never null.
//
// In Go the defect is a nil slice marshalling to null; in Rust it is an
// `Option<Vec<_>>` field or a skip-if-empty attribute, either of which would
// reintroduce it. A sibling port put a literal `"edges": null` on the wire
// through its own SDK unchallenged, so nothing below this crate catches it.
// ---------------------------------------------------------------------------
#[test]
fn every_list_serializes_as_an_array_never_null() {
    let raw = encoded(WalkResult::new(vec![], vec![], Completeness::complete()));
    assert!(
        raw.contains(r#""nodes":[]"#),
        "an empty walk must emit an empty ARRAY for nodes: {raw}"
    );
    assert!(
        raw.contains(r#""edges":[]"#),
        "an empty walk must emit an empty ARRAY for edges: {raw}"
    );
    assert!(
        !raw.contains("null"),
        "no field of the envelope may serialize as null: {raw}"
    );
}

// ---------------------------------------------------------------------------
// R1.6 — walk_complete is ALWAYS emitted.
//
// This rides the INCOMPLETE arm deliberately: a complete walk emits `true` and a
// skip-if-empty defect would be invisible.
// ---------------------------------------------------------------------------
#[test]
fn walk_complete_is_always_emitted_including_on_the_incomplete_arm() {
    let raw = encoded(WalkResult::new(
        vec![Node::new("n1", "issue")],
        vec![],
        Completeness::incomplete("the source moved under the walk"),
    ));
    assert!(
        raw.contains(r#""walk_complete":false"#),
        "an INCOMPLETE walk must emit walk_complete false, not omit it: {raw}"
    );

    let raw = encoded(WalkResult::new(vec![], vec![], Completeness::complete()));
    assert!(
        raw.contains(r#""walk_complete":true"#),
        "a complete walk emits walk_complete true: {raw}"
    );
}

// ---------------------------------------------------------------------------
// R1.7 (b) — an INCOMPLETE result with an empty reason is refused, naming the
// collector.
// ---------------------------------------------------------------------------
#[test]
fn an_incomplete_walk_with_no_reason_is_refused() {
    let err = encode_result(
        "collect",
        WalkResult::new(vec![], vec![], Completeness::incomplete("")),
    )
    .expect_err("an incomplete assertion with no reason must be refused");
    assert!(err.message().contains("INCOMPLETE"), "{err}");
    assert!(
        err.message().contains("collect"),
        "the refusal must name the collector: {err}"
    );
}

// ---------------------------------------------------------------------------
// R1.7 (c) — a node with an empty type is refused, naming the INDEX and the ID.
// ---------------------------------------------------------------------------
#[test]
fn a_node_with_an_empty_type_is_refused_naming_the_index_and_the_id() {
    let err = encode_result(
        "collect",
        WalkResult::new(
            vec![Node::new("first", "issue"), Node::new("second", "")],
            vec![],
            Completeness::complete(),
        ),
    )
    .expect_err("a node with an empty type must be refused");
    assert!(err.message().contains("node[1]"), "{err}");
    assert!(err.message().contains("second"), "{err}");
    assert!(err.message().contains("collect"), "{err}");
}

// ---------------------------------------------------------------------------
// R1.7 (b) and (c) CONTROL — the shapes just above are refused for what they
// are, not because encode_result refuses everything.
// ---------------------------------------------------------------------------
#[test]
fn control_a_well_formed_result_encodes() {
    encode_result(
        "collect",
        WalkResult::new(
            vec![Node::new("n1", "issue")],
            vec![Edge::new("n1", "n2", "blocks")],
            Completeness::incomplete("a stated reason"),
        ),
    )
    .expect("control: an incomplete walk WITH a reason and typed nodes encodes");
}

// ---------------------------------------------------------------------------
// R1.14 — the edge's two graph fields serialize away when empty and serialize
// when set.
//
// The client refuses an edge naming BOTH at collect time; that is the CLIENT's
// refusal per the output schema's own description, so this crate asserts only
// the serialization.
// ---------------------------------------------------------------------------
#[test]
fn the_edges_two_graph_fields_serialize_away_when_empty() {
    let raw = encoded(WalkResult::new(
        vec![Node::new("n1", "issue")],
        vec![Edge::new("n1", "n2", "blocks")],
        Completeness::complete(),
    ));
    assert!(
        !raw.contains("source_graph"),
        "an edge setting neither graph field must be byte-identical to one written before the fields existed: {raw}"
    );
    assert!(!raw.contains("target_graph"), "{raw}");

    let mut target = Edge::new("n1", "res-1", "describes");
    target.target_graph = "code".into();
    let raw = encoded(WalkResult::new(
        vec![Node::new("n1", "issue")],
        vec![target],
        Completeness::complete(),
    ));
    assert!(raw.contains(r#""target_graph":"code""#), "{raw}");
    assert!(!raw.contains("source_graph"), "{raw}");

    let mut source = Edge::new("chart-1", "n1", "deploys");
    source.source_graph = "code".into();
    let raw = encoded(WalkResult::new(
        vec![Node::new("n1", "issue")],
        vec![source],
        Completeness::complete(),
    ));
    assert!(raw.contains(r#""source_graph":"code""#), "{raw}");
    assert!(!raw.contains("target_graph"), "{raw}");
}

// ---------------------------------------------------------------------------
// The node's optional fields serialize away when empty and serialize when set —
// the ABSENT versus PRESENT-AND-EMPTY distinction the client's own conforming
// fixture is built around, asserted here on this crate's own type.
// ---------------------------------------------------------------------------
#[test]
fn a_minimal_node_emits_only_id_and_type() {
    let raw = encoded(WalkResult::new(
        vec![Node::new("ISSUE-2", "issue")],
        vec![],
        Completeness::complete(),
    ));
    assert!(raw.contains(r#"{"id":"ISSUE-2","type":"issue"}"#), "{raw}");

    let mut full = Node::new("ISSUE-1", "issue");
    full.summary = "one line".into();
    full.is_exported = true;
    full.start_line = 10;
    full.metadata.insert("priority".into(), "high".into());
    let raw = encoded(WalkResult::new(
        vec![full],
        vec![],
        Completeness::complete(),
    ));
    assert!(raw.contains(r#""summary":"one line""#), "{raw}");
    assert!(raw.contains(r#""is_exported":true"#), "{raw}");
    assert!(raw.contains(r#""start_line":10"#), "{raw}");
    assert!(raw.contains(r#""metadata":{"priority":"high"}"#), "{raw}");
}
