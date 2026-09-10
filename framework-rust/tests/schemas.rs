// SPDX-License-Identifier: Apache-2.0

//! schemas.rs — what this crate ADVERTISES, and what the checked-in contract
//! documents themselves require.
//!
//! Rows R1.2, R1.3 and R1.4 of the ticket's what-to-test list, and replicated
//! contract properties 1, 2, 3 and 10.

use std::collections::BTreeMap;

use knowledge_collector_framework::contract::decode_result;
use knowledge_collector_framework::envelope::CollectOutput;
use knowledge_collector_framework::schema::{
    advertised_input_schema, advertised_output_schema, input_contract_json, output_contract_json,
    params_schema_document,
};
use schemars::JsonSchema;
use serde::Deserialize;
use serde_json::{json, Value};

/// A params type with fields of its own, so the splice is observable.
#[derive(Debug, Default, Deserialize, JsonSchema)]
struct DemoParams {
    /// The directory to walk.
    #[serde(default)]
    root: String,
    #[serde(default)]
    depth: u32,
}

/// A params type with NO fields. It is a BRACED empty struct rather than a unit
/// struct on purpose: a unit struct serializes as JSON null and infers the schema
/// type "null", which this crate refuses. That is the Rust shape of the Go
/// framework's `struct{}`.
#[derive(Debug, Default, Deserialize, JsonSchema)]
struct NoParams {}

fn contract_doc(raw: Vec<u8>) -> serde_json::Map<String, Value> {
    match serde_json::from_slice::<Value>(&raw).expect("a checked-in contract decodes") {
        Value::Object(m) => m,
        other => panic!("a checked-in contract is a {other} rather than an object"),
    }
}

// ---------------------------------------------------------------------------
// R2(b) property 1 — the output contract requires the completeness assertion.
// It reads the CHECKED-IN artifact rather than a transcription of it, because
// the artifact is what a collector author copies into their tool.
// ---------------------------------------------------------------------------
#[test]
fn output_contract_requires_the_completeness_assertion() {
    let schema = contract_doc(output_contract_json());
    assert_eq!(schema["type"], json!("object"));

    let mut required: Vec<&str> = schema["required"]
        .as_array()
        .expect("required is an array")
        .iter()
        .map(|v| v.as_str().expect("a required entry is a string"))
        .collect();
    required.sort();
    assert_eq!(
        required,
        vec!["edges", "nodes", "walk_complete"],
        "the completeness assertion is REQUIRED — a provider must not be able to omit it and disable deletion by silence"
    );

    let props = schema["properties"].as_object().expect("properties");
    assert_eq!(props["walk_complete"]["type"], json!("boolean"));
    assert_eq!(props["nodes"]["type"], json!("array"));
    assert_eq!(props["edges"]["type"], json!("array"));
}

// ---------------------------------------------------------------------------
// R2(b) property 2 — the input contract requires the collect id, which names the
// graph instance, so a tool that does not take it cannot be driven.
// ---------------------------------------------------------------------------
#[test]
fn input_contract_requires_the_collect_id() {
    let schema = contract_doc(input_contract_json());
    assert_eq!(schema["type"], json!("object"));
    assert_eq!(schema["required"], json!(["id"]));
    let props = schema["properties"].as_object().expect("properties");
    assert_eq!(props["id"]["type"], json!("string"));
    assert_eq!(props["params"]["type"], json!("object"));
}

// ---------------------------------------------------------------------------
// R2(b) property 3 — the drift guard between the two halves of one contract: the
// checked-in JSON a collector author reads, and the Rust type this crate encodes
// into. A field added to one and not the other is a contract that says two
// different things.
// ---------------------------------------------------------------------------
#[test]
fn contract_schema_and_envelope_agree() {
    // Every key the schema names must decode into the envelope...
    let minimal = json!({
        "nodes": [{"id": "a", "type": "issue"}],
        "edges": [{"from_id": "a", "to_id": "b", "type": "blocks"}],
        "walk_complete": true
    });
    let decoded = decode_result("t", Some(&minimal))
        .expect("the schema's own minimal document must decode into the envelope");
    assert_eq!(decoded.nodes.len(), 1);
    assert_eq!(decoded.edges.len(), 1);
    assert!(decoded.walk_complete);

    // ...and the envelope must name no top-level field the schema does not.
    let empty = CollectOutput {
        nodes: vec![],
        edges: vec![],
        walk_complete: false,
    };
    let encoded = serde_json::to_value(&empty).expect("the envelope serializes");
    let schema = contract_doc(output_contract_json());
    let props = schema["properties"].as_object().expect("properties");
    for key in encoded.as_object().expect("an object").keys() {
        assert!(
            props.contains_key(key),
            "the envelope carries the top-level field {key:?} and the checked-in contract schema does not describe it"
        );
    }
}

// ---------------------------------------------------------------------------
// R2(b) property 10 — the context block IS a declared property of the published
// input schema and is NOT on the required list.
// ---------------------------------------------------------------------------
#[test]
fn input_contract_declares_context_as_an_optional_property() {
    let schema = contract_doc(input_contract_json());
    assert_eq!(
        schema["required"],
        json!(["id"]),
        "the collect id is the ONLY required input property; an added required property refuses every existing provider"
    );
    let props = schema["properties"].as_object().expect("properties");
    let ctx = props
        .get("context")
        .expect("the context block is a declared property of the published input schema");
    assert_eq!(ctx["type"], json!("object"));

    // THE CONTROL: the two properties that were there before are untouched, so
    // the assertion above is about an addition rather than about a rewritten file.
    assert_eq!(props["id"]["type"], json!("string"));
    assert_eq!(props["params"]["type"], json!("object"));
}

// ---------------------------------------------------------------------------
// R1.2 — the advertised output schema is the contract file, verbatim.
// ---------------------------------------------------------------------------
#[test]
fn advertised_output_schema_is_the_contract_file_verbatim() {
    let advertised = advertised_output_schema().expect("the output schema resolves");
    let embedded = contract_doc(output_contract_json());
    assert_eq!(
        Value::Object(advertised),
        Value::Object(embedded),
        "the advertised output schema is the checked-in document and nothing derived from a Rust type"
    );
}

// ---------------------------------------------------------------------------
// R1.3 — the advertised input schema is the contract file with ONLY
// properties.params replaced.
// ---------------------------------------------------------------------------
#[test]
fn advertised_input_schema_is_the_contract_with_only_params_replaced() {
    let advertised = advertised_input_schema::<DemoParams>().expect("the input schema resolves");

    // (a) top-level required is exactly ["id"].
    assert_eq!(
        advertised["required"],
        json!(["id"]),
        "splicing params in must not put it on the top-level required list; the client omits params from a paramless collect"
    );

    // (b) a params property is present and its type is object.
    let props = advertised["properties"]
        .as_object()
        .expect("properties is an object");
    assert_eq!(props["params"]["type"], json!("object"));

    // (c) the spliced sub-schema carries the collector's OWN fields.
    let params_props = props["params"]["properties"]
        .as_object()
        .expect("the spliced params schema declares properties");
    assert!(params_props.contains_key("root"));
    assert!(params_props.contains_key("depth"));

    // (d) everything OUTSIDE properties.params is the contract document,
    // key for key. Strip the one property from both sides and compare.
    let mut stripped = Value::Object(advertised.clone());
    stripped["properties"]
        .as_object_mut()
        .expect("properties")
        .remove("params");
    let mut contract = Value::Object(contract_doc(input_contract_json()));
    contract["properties"]
        .as_object_mut()
        .expect("properties")
        .remove("params");
    assert_eq!(
        stripped,
        contract,
        "the splice must touch properties.params and nothing else"
    );
}

// ---------------------------------------------------------------------------
// R1.4 — a params type that is not an object is REFUSED, naming the type.
// The Go twin accepts an empty struct and a map and refuses a string; the three
// cases are the same here.
// ---------------------------------------------------------------------------
#[test]
fn params_type_must_infer_an_object() {
    params_schema_document::<NoParams>().expect("an empty struct params type is an object");
    params_schema_document::<BTreeMap<String, String>>()
        .expect("a map params type is an object");

    let err = params_schema_document::<String>()
        .expect_err("a string params type must be refused");
    assert!(
        err.message().contains("object"),
        "the refusal must say the contract declares params as an object: {err}"
    );
    assert!(
        err.message().contains("alloc::string::String") || err.message().contains("String"),
        "the refusal must NAME the params type: {err}"
    );
}

// ---------------------------------------------------------------------------
// R1.4, second arm — a collector whose params type is not an object cannot be
// served at all: the refusal reaches the caller when the server is built, not
// when a collect arrives.
// ---------------------------------------------------------------------------
#[test]
fn a_non_object_params_type_refuses_the_server() {
    use knowledge_collector_framework::describe::{Declaration, Vocabulary};
use knowledge_collector_framework::prelude::*;
    use knowledge_collector_framework::serve::CollectorServer;

    struct BadParamsCollector;
    impl Collector for BadParamsCollector {
        type Params = String;
        fn tool(&self) -> ToolSpec {
            ToolSpec::new("collect", "")
        }

    fn declaration(&self) -> Declaration {
        // A minimal conforming declaration: the vocabulary this fixture's walk
        // emits, and nothing else. Every collector owes one, so every fixture
        // does too.
        Declaration {
            vocabulary: Vocabulary::new(vec!["issue".into()], vec!["blocks".into()]),
            ..Declaration::default()
        }
    }
        fn walk(
            &self,
            _id: &str,
            _params: String,
            _foreign: &ForeignContext,
        ) -> Result<WalkResult, WalkError> {
            Ok(WalkResult::new(vec![], vec![], Completeness::complete()))
        }
    }

    let err = CollectorServer::new(BadParamsCollector)
        .err()
        .expect("a string params type must refuse the server");
    assert!(err.message().contains("object"), "{err}");
}
