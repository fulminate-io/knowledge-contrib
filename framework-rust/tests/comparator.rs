// SPDX-License-Identifier: Apache-2.0

//! comparator.rs — the SCHEMA GATE: what an advertised schema pair must declare
//! for the client to admit it, and every way it can fall short.
//!
//! Replicated contract properties 4, 5, 6, 11, 12 and 13. Each row bends ONE
//! thing, so a comparator that stopped checking any single one of them turns a
//! row red rather than leaving the suite green.

use knowledge_collector_framework::contract::check_tool_schemas;
use knowledge_collector_framework::schema::{input_contract_json, output_contract_json};
use serde_json::{json, Value};

fn contract(raw: Vec<u8>) -> Value {
    serde_json::from_slice(&raw).expect("a checked-in contract decodes")
}

fn input() -> Value {
    contract(input_contract_json())
}

fn output() -> Value {
    contract(output_contract_json())
}

fn drop_property(mut doc: Value, name: &str) -> Value {
    doc["properties"]
        .as_object_mut()
        .expect("properties")
        .remove(name);
    doc
}

// ---------------------------------------------------------------------------
// Property 4 — the two absence arms.
//
// The no-INPUT-schema arm is reachable only as a unit, because an MCP server
// refuses to publish a tool with no input schema at all.
// ---------------------------------------------------------------------------
#[test]
fn check_tool_schemas_missing_schemas() {
    let err = check_tool_schemas("collect_graph", None, Some(&output()))
        .expect_err("a missing input schema must be refused");
    assert!(err.message().contains("NO input schema"), "{err}");
    assert!(err.message().contains("collect_graph"), "{err}");

    let err = check_tool_schemas("collect_graph", Some(&input()), None)
        .expect_err("a missing output schema must be refused");
    assert!(err.message().contains("NO output schema"), "{err}");
    assert!(err.message().contains("walk_complete"), "{err}");
}

// ---------------------------------------------------------------------------
// Property 5 — what the comparator ADMITS, which is what keeps it from being a
// schema-equality check: a provider may be stricter than the contract, never
// looser.
// ---------------------------------------------------------------------------
#[test]
fn check_tool_schemas_accepts_the_contract_verbatim_and_stricter() {
    check_tool_schemas("collect_graph", Some(&input()), Some(&output()))
        .expect("the contract advertised verbatim is admitted");

    let mut stricter = output();
    stricter["required"] = json!(["nodes", "edges", "walk_complete", "collected_at"]);
    stricter["properties"]
        .as_object_mut()
        .expect("properties")
        .insert("collected_at".into(), json!({"type": "string"}));
    check_tool_schemas("collect_graph", Some(&input()), Some(&stricter)).expect(
        "a provider declaring MORE than the contract is conforming; the contract is a floor, not an equality",
    );
}

// ---------------------------------------------------------------------------
// Property 6 — EIGHT loosenings, one per row, each with the substring the
// refusal must contain.
// ---------------------------------------------------------------------------
#[test]
fn check_tool_schemas_refuses_each_loosening_individually() {
    struct Row {
        name: &'static str,
        bend: fn(&mut Value),
        want: &'static str,
    }

    let rows = [
        Row {
            name: "top-level type is not an object",
            bend: |out| out["type"] = json!("array"),
            want: r#"the contract requires type "object""#,
        },
        Row {
            name: "walk_complete not required",
            bend: |out| out["required"] = json!(["nodes", "edges"]),
            want: "walk_complete",
        },
        Row {
            name: "walk_complete property missing",
            bend: |out| {
                out["properties"]
                    .as_object_mut()
                    .expect("properties")
                    .remove("walk_complete");
            },
            want: "walk_complete",
        },
        Row {
            name: "walk_complete declared as a string",
            bend: |out| out["properties"]["walk_complete"] = json!({"type": "string"}),
            want: r#"requires type "boolean""#,
        },
        Row {
            name: "nodes declared as an object",
            bend: |out| out["properties"]["nodes"] = json!({"type": "object"}),
            want: r#"requires type "array""#,
        },
        Row {
            name: "node items do not require an id",
            bend: |out| out["properties"]["nodes"]["items"]["required"] = json!(["type"]),
            want: "outputSchema.nodes[]",
        },
        Row {
            name: "edge items do not require the endpoints",
            bend: |out| out["properties"]["edges"]["items"]["required"] = json!(["type"]),
            want: "from_id",
        },
        Row {
            name: "nodes declares no item schema",
            bend: |out| {
                out["properties"]["nodes"]
                    .as_object_mut()
                    .expect("the nodes property is an object")
                    .remove("items");
            },
            want: "item schema",
        },
    ];

    for row in rows {
        let mut out = output();
        (row.bend)(&mut out);
        let Err(err) = check_tool_schemas("collect_graph", Some(&input()), Some(&out)) else {
            panic!("[{}] a loosened schema must be refused", row.name)
        };
        assert!(
            err.message().contains(row.want),
            "[{}] the refusal must contain {:?}: {err}",
            row.name,
            row.want
        );
        assert!(
            err.message().contains("collect_graph"),
            "[{}] the refusal must name the tool: {err}",
            row.name
        );
    }
}

// ---------------------------------------------------------------------------
// Property 11 — THREE rows: an advertised input lacking `context` is admitted,
// one lacking `params` is admitted, and the SAME-RUN CONTROL that the contract
// advertised verbatim passes. The control is what distinguishes a widened rule
// from a gate that stopped checking.
// ---------------------------------------------------------------------------
#[test]
fn check_tool_schemas_admits_a_provider_lacking_an_optional_property() {
    let out = output();

    let without_context = drop_property(input(), "context");
    check_tool_schemas("collect", Some(&without_context), Some(&out))
        .expect("a provider written before this property existed is admitted unchanged");

    let without_params = drop_property(input(), "params");
    check_tool_schemas("collect", Some(&without_params), Some(&out)).expect(
        "params is optional on the contract's own required list, so the same rule reaches it — a deliberate widening",
    );

    // THE DISCRIMINATOR IS THE CONTRACT'S OWN REQUIRED LIST, never the
    // advertised one, and this is the cell that shows the difference: a provider
    // that lists `params` on its OWN required list and then declares no `params`
    // property is still ADMITTED, because the CONTRACT does not require params.
    // A gate reading the advertised list here would refuse it.
    let mut claims_params_required = drop_property(input(), "params");
    claims_params_required["required"] = json!(["id", "params"]);
    check_tool_schemas("collect", Some(&claims_params_required), Some(&out)).expect(
        "the optional-property skip reads the CONTRACT's required list; a provider's own \
         stricter required list must not turn an optional property into a required one",
    );

    // THE SAME-RUN CONTROL, through the same instrument and the same path.
    check_tool_schemas("collect", Some(&input()), Some(&out))
        .expect("control: the contract advertised verbatim passes");
}

// ---------------------------------------------------------------------------
// Property 12 — the arm the widening must not take with it. The advertised
// required list STILL names `id`, so the required-list loop does not fire and
// the refusal comes from the property loop reading the CONTRACT's required list.
// ---------------------------------------------------------------------------
#[test]
fn check_tool_schemas_still_refuses_a_missing_required_property() {
    let without_id = drop_property(input(), "id");
    assert!(
        without_id["required"]
            .as_array()
            .expect("required")
            .contains(&json!("id")),
        "fixture control: the advertised required list still names id, so only the property is missing"
    );

    let err = check_tool_schemas("collect", Some(&without_id), Some(&output()))
        .expect_err("id is on the CONTRACT's required list, so its property is not optional");
    assert!(err.message().contains(r#"the property "id""#), "{err}");
}

// ---------------------------------------------------------------------------
// Property 13 — the optional arm must skip an ABSENT property, never a PRESENT
// wrong one. Without this row a gate that returned early on the whole property
// would pass a provider declaring context as a string.
// ---------------------------------------------------------------------------
#[test]
fn check_tool_schemas_refuses_an_optional_property_of_the_wrong_type() {
    let mut wrong = input();
    wrong["properties"]["context"] = json!({"type": "string"});

    let err = check_tool_schemas("collect", Some(&wrong), Some(&output()))
        .expect_err("a declared optional property is still compared when it IS declared");
    assert!(err.message().contains("inputSchema.context"), "{err}");
    assert!(err.message().contains(r#"requires type "object""#), "{err}");
}

// ---------------------------------------------------------------------------
// What THIS crate advertises is admissible by the same gate. This is the reason
// the comparator is in the crate at all: a collector author proves their own
// advertisement passes before a client ever dials them.
// ---------------------------------------------------------------------------
#[test]
fn this_crates_own_advertisement_is_admissible() {
    use knowledge_collector_framework::sample::SampleCollector;
    use knowledge_collector_framework::serve::CollectorServer;

    let server = CollectorServer::new(SampleCollector).expect("the sample collector serves");
    check_tool_schemas(
        server.tool_name(),
        Some(server.advertised_input_schema()),
        Some(server.advertised_output_schema()),
    )
    .expect("what this crate advertises must satisfy the contract it ships");
}
