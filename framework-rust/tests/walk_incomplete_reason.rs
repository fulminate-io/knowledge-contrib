// SPDX-License-Identifier: Apache-2.0

//! walk_incomplete_reason.rs — the incomplete walk's reason on the wire.
//!
//! The encoder sends the reason `Completeness::incomplete` carries, as
//! `walk_incomplete_reason` beside `walk_complete` false, and sends no key at all
//! on a complete walk. The crate's own strict decoder admits the key, still
//! refuses a misspelled one, and refuses the two shapes that contradict the key's
//! contract: a reason beside a walk asserting completeness, and an empty reason.
//! An incomplete walk with no reason key is admitted, because the key is
//! optional. The encoder assertions are over the SERIALIZED BYTES, as the rest of
//! the envelope rows are.

use knowledge_collector_framework::completeness::Completeness;
use knowledge_collector_framework::contract::decode_result;
use knowledge_collector_framework::envelope::encode_result;
use knowledge_collector_framework::model::{Node, WalkResult};
use serde_json::json;

fn encoded(result: WalkResult) -> String {
    let out = encode_result("collect", result).expect("the result encodes");
    serde_json::to_string(&out).expect("the envelope serializes")
}

#[test]
fn an_incomplete_walk_sends_its_reason() {
    let raw = encoded(WalkResult::new(
        vec![Node::new("n1", "issue")],
        vec![],
        Completeness::incomplete("the source moved under the walk"),
    ));
    assert!(
        raw.contains(r#""walk_incomplete_reason":"the source moved under the walk""#),
        "an INCOMPLETE walk must send its reason as walk_incomplete_reason: {raw}"
    );
}

#[test]
fn a_complete_walk_sends_no_reason_key() {
    let raw = encoded(WalkResult::new(vec![], vec![], Completeness::complete()));
    assert!(
        !raw.contains("walk_incomplete_reason"),
        "a complete walk must send no walk_incomplete_reason key at all: {raw}"
    );
}

#[test]
fn the_crates_own_incomplete_result_round_trips() {
    let out = encode_result(
        "collect",
        WalkResult::new(vec![], vec![], Completeness::incomplete("the source moved under the walk")),
    )
    .expect("the result encodes");
    let value = serde_json::to_value(&out).expect("the envelope serializes");
    let decoded = decode_result("collect", Some(&value)).expect("the crate's own incomplete result decodes");
    assert!(!decoded.walk_complete);
    assert_eq!(
        decoded.walk_incomplete_reason.as_deref(),
        Some("the source moved under the walk")
    );
}

#[test]
fn a_misspelled_reason_key_is_still_refused() {
    let payload = json!({"nodes": [], "edges": [], "walk_complete": false, "walk_incomplete_reasn": "r"});
    let err = decode_result("collect", Some(&payload)).expect_err("a misspelled key must be refused");
    assert!(err.to_string().contains("walk_incomplete_reasn"), "{err}");
}

#[test]
fn a_reason_beside_a_complete_walk_is_refused() {
    let payload = json!({"nodes": [], "edges": [], "walk_complete": true, "walk_incomplete_reason": "r"});
    let err = decode_result("collect", Some(&payload)).expect_err("a reason beside a complete walk must be refused");
    let msg = err.to_string();
    assert!(msg.contains("walk_incomplete_reason"), "{msg}");
    assert!(msg.contains("walk_complete true"), "{msg}");
}

#[test]
fn an_empty_reason_beside_an_incomplete_walk_is_refused() {
    let payload = json!({"nodes": [], "edges": [], "walk_complete": false, "walk_incomplete_reason": ""});
    let err = decode_result("collect", Some(&payload)).expect_err("an empty reason must be refused");
    let msg = err.to_string();
    assert!(msg.contains("empty walk_incomplete_reason"), "{msg}");
    assert!(msg.contains("walk_complete false"), "{msg}");
}

#[test]
fn control_an_incomplete_walk_with_no_reason_key_is_admitted() {
    let payload = json!({"nodes": [], "edges": [], "walk_complete": false});
    let decoded = decode_result("collect", Some(&payload)).expect("an incomplete walk with no reason key is admitted");
    assert!(!decoded.walk_complete);
    assert_eq!(decoded.walk_incomplete_reason, None);
}
