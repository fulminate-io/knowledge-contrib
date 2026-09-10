// SPDX-License-Identifier: Apache-2.0

//! payload.rs — the PAYLOAD GATE, which is a different question from the schema
//! gate: a provider may advertise a perfect schema and still return something
//! else.
//!
//! Replicated contract properties 7, 8 and 9, and row R1.7a — the row that
//! exists because the Rust MCP SDK validates the handler's result no more than
//! it validates the call's arguments, so this crate's own encode path is the
//! only thing between a walk and a non-conforming result.

use knowledge_collector_framework::completeness::Completeness;
use knowledge_collector_framework::contract::{decode_result, validate_result_payload};
use knowledge_collector_framework::envelope::encode_result;
use knowledge_collector_framework::model::{Edge, Node, WalkResult};
use serde_json::json;

// ---------------------------------------------------------------------------
// Property 7 — FOUR non-conforming payloads, each refused, each naming the tool
// and the offending property.
// ---------------------------------------------------------------------------
#[test]
fn validate_result_payload_refuses_non_conforming_results() {
    for (name, payload, want) in [
        ("missing walk_complete", r#"{"nodes":[],"edges":[]}"#, "walk_complete"),
        (
            "nodes is not an array",
            r#"{"nodes":{},"edges":[],"walk_complete":true}"#,
            "nodes",
        ),
        (
            "node missing its type",
            r#"{"nodes":[{"id":"a"}],"edges":[],"walk_complete":true}"#,
            "type",
        ),
        (
            "walk_complete is a string",
            r#"{"nodes":[],"edges":[],"walk_complete":"yes"}"#,
            "walk_complete",
        ),
    ] {
        let Err(err) = validate_result_payload("collect_graph", payload.as_bytes()) else {
            panic!("[{name}] a non-conforming payload must be refused")
        };
        assert!(
            err.message().contains("collect_graph"),
            "[{name}] the refusal must name the tool: {err}"
        );
        assert!(
            err.message().contains(want),
            "[{name}] the refusal must contain {want:?}: {err}"
        );
    }
}

// ---------------------------------------------------------------------------
// Property 7's CONTROL — a conforming payload passes through the same
// instrument, so the four refusals above are not a validator that refuses
// everything.
// ---------------------------------------------------------------------------
#[test]
fn validate_result_payload_admits_a_conforming_result() {
    validate_result_payload(
        "collect_graph",
        br#"{"nodes":[{"id":"a","type":"issue"}],"edges":[],"walk_complete":true}"#,
    )
    .expect("control: a conforming payload is admitted");
}

// ---------------------------------------------------------------------------
// Property 8 — a typo'd key is an error rather than a silent drop: a dropped
// field is a collector author debugging an empty graph with no message to go on.
// ---------------------------------------------------------------------------
#[test]
fn decode_result_refuses_an_undefined_field() {
    let payload = json!({
        "nodes": [{"id": "a", "type": "issue", "summry": "typo"}],
        "edges": [],
        "walk_complete": true
    });
    let err = decode_result("collect_graph", Some(&payload))
        .expect_err("a field the contract does not define must be refused");
    assert!(err.message().contains("summry"), "{err}");
}

// ---------------------------------------------------------------------------
// Property 9 — the arm a provider reaches by answering with prose instead of a
// structured result.
// ---------------------------------------------------------------------------
#[test]
fn decode_result_refuses_no_structured_content() {
    let err = decode_result("collect_graph", None)
        .expect_err("an absent structured result must be refused");
    assert!(err.message().contains("no structuredContent"), "{err}");
}

// ---------------------------------------------------------------------------
// R1.7a — EVERY result this crate emits satisfies the advertised output schema,
// asserted over the SERIALIZED envelope rather than over the Rust value.
//
// WHAT DOES AND DOES NOT CATCH THIS IN PRODUCTION: the client re-validates the
// result against the provider's advertised output schema on its own side, so a
// violation is caught eventually — but at the CLIENT, with a message about the
// provider breaking its word, rather than at the collector where the author can
// fix it. Nothing in rmcp catches it at all.
// ---------------------------------------------------------------------------
#[test]
fn every_encoded_result_satisfies_the_advertised_output_schema() {
    let cases = vec![
        (
            "an empty complete walk",
            WalkResult::new(vec![], vec![], Completeness::complete()),
        ),
        (
            "an empty incomplete walk",
            WalkResult::new(vec![], vec![], Completeness::incomplete("the source moved")),
        ),
        (
            "a minimal node and edge",
            WalkResult::new(
                vec![Node::new("a", "issue")],
                vec![Edge::new("a", "b", "blocks")],
                Completeness::complete(),
            ),
        ),
        ("a fully populated node", {
            let mut node = Node::new("ISSUE-1", "issue");
            node.symbol_name = "Login broken".into();
            node.file_path = "src/login.go".into();
            node.language = "go".into();
            node.start_line = 10;
            node.end_line = 20;
            node.content = "body".into();
            node.signature = "sig".into();
            node.summary = "one line".into();
            node.description = "longer".into();
            node.source = "walk".into();
            node.status = "open".into();
            node.keywords = "login auth".into();
            node.is_exported = true;
            node.metadata.insert("priority".into(), "high".into());
            let mut edge = Edge::new("ISSUE-1", "res-1", "describes");
            edge.target_graph = "code".into();
            edge.weight = 0.5;
            edge.confidence = 0.9;
            edge.method = "manual".into();
            edge.evidence = "a file".into();
            WalkResult::new(vec![node], vec![edge], Completeness::complete())
        }),
    ];

    for (name, result) in cases {
        let out = encode_result("collect", result)
            .unwrap_or_else(|e| panic!("[{name}] the result encodes: {e}"));
        let raw = serde_json::to_vec(&out).expect("the envelope serializes");
        validate_result_payload("collect", &raw).unwrap_or_else(|e| {
            panic!(
                "[{name}] the encoded result does not satisfy the advertised output schema: {e}\npayload: {}",
                String::from_utf8_lossy(&raw)
            )
        });
    }
}
