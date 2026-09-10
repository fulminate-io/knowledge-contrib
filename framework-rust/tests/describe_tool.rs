// SPDX-License-Identifier: Apache-2.0

//! describe_tool.rs — the REQUIRED second tool, on the wire and in the crate.
//!
//! A collector serves two tools: the collect tool it names, and a declaration
//! tool whose name is FIXED because the client has to know what to call before
//! it can call anything. This file is that tool's observations: it is listed, it
//! advertises the contract's describe document verbatim, it answers with a
//! declaration that satisfies that document, and the conformance stub's four
//! describe-failure modes each fail in their own way.

mod common;

use knowledge_collector_framework::conformance::{
    conforming_declaration, Mode, DEFAULT_STUB_TOOL, DESCRIBE_ERROR_TEXT, STUB_MODE_ENV,
};
use knowledge_collector_framework::framework::DESCRIBE_TOOL_NAME;
use knowledge_collector_framework::sample::SampleCollector;
use knowledge_collector_framework::schema::describe_contract_json;
use knowledge_collector_framework::serve::CollectorServer;
use serde_json::{json, Value};

use common::{error_text, is_error, McpChild};

const SAMPLE: &str = env!("CARGO_BIN_EXE_sample-rs");
const STUB: &str = env!("CARGO_BIN_EXE_conformance-stub");

fn describe_schema() -> Value {
    serde_json::from_slice(&describe_contract_json()).expect("the describe contract decodes")
}

/// conforms validates a document against the contract's describe schema and
/// returns every failure, so a caller can assert either direction.
fn conforms(document: &Value) -> Vec<String> {
    let schema = describe_schema();
    let validator = jsonschema::validator_for(&schema).expect("the describe schema resolves");
    validator
        .iter_errors(document)
        .map(|e| format!("at {}: {e}", e.instance_path()))
        .collect()
}

// ---------------------------------------------------------------------------
// The tool is SERVED, and found BY NAME rather than by position.
//
// The Go framework's own suite carried two one-tool assertions that had to be
// rewritten the day a second tool arrived. A lookup by name asserting no
// cardinality is what survives the third.
// ---------------------------------------------------------------------------
#[test]
fn the_declaration_tool_is_served_beside_the_collect_tool() {
    let mut child = McpChild::spawn(SAMPLE, &[]);
    child.initialize();
    let tools = child.list_tools();

    let describe = tools
        .iter()
        .find(|t| t["name"] == json!(DESCRIBE_TOOL_NAME))
        .unwrap_or_else(|| panic!("the declaration tool is not served: {tools:?}"));
    assert!(
        tools.iter().any(|t| t["name"] == json!("collect")),
        "control: the collect tool is served too, so the row above is about an addition"
    );

    // ITS OUTPUT SCHEMA IS THE CONTRACT DOCUMENT, VERBATIM. A schema derived
    // from the crate's own declaration type would be refused by the client's
    // gate for the same reason an inferred output schema is.
    assert_eq!(
        describe["outputSchema"],
        describe_schema(),
        "the declaration tool must advertise the checked-in describe document unchanged"
    );

    // AND IT TAKES NO ARGUMENTS. An MCP server may not publish a tool with no
    // input schema at all, so the empty object is the shape rather than an
    // omission.
    assert_eq!(describe["inputSchema"]["type"], json!("object"));
    assert!(
        describe["inputSchema"].get("required").is_none(),
        "the declaration tool requires no argument: {}",
        describe["inputSchema"]
    );
}

// ---------------------------------------------------------------------------
// It ANSWERS with a declaration that satisfies the document it advertised.
//
// That is the property the Rust SDK does not check for us, exactly as it does
// not check the collect result: the crate is what keeps its own word.
// ---------------------------------------------------------------------------
#[test]
fn the_declaration_answered_on_the_wire_satisfies_the_advertised_schema() {
    let mut child = McpChild::spawn(SAMPLE, &[]);
    child.initialize();
    let result = child.call_tool(DESCRIBE_TOOL_NAME, json!({}));
    assert!(!is_error(&result), "{}", error_text(&result));

    let declaration = result["structuredContent"].clone();
    assert_eq!(
        conforms(&declaration),
        Vec::<String>::new(),
        "the declaration on the wire does not satisfy the schema the same tool advertised: {declaration}"
    );

    // The vocabulary the sample's walk emits, on the wire.
    assert_eq!(declaration["node_types"], json!(["directory", "file"]));
    assert_eq!(declaration["edge_types"], json!(["contains"]));
    assert_eq!(declaration["environment"], json!([]));
    assert_eq!(declaration["behavior"]["syncable"], json!(true));

    // THE KNOWN POSITIVE for the validator, in the same run: a document missing
    // a required key IS reported. Without it a validator that accepted anything
    // would pass the assertion above.
    let mut broken = declaration.clone();
    broken.as_object_mut().expect("an object").remove("behavior");
    assert!(
        !conforms(&broken).is_empty(),
        "control: the validator must refuse a declaration missing a required key"
    );
}

// ---------------------------------------------------------------------------
// The declaration is rendered and validated when the SERVER IS BUILT, not at the
// first describe call: a collector that cannot describe itself should fail to
// start rather than fail an operator's registration dial.
// ---------------------------------------------------------------------------
#[test]
fn a_malformed_declaration_refuses_the_server_at_construction() {
    use knowledge_collector_framework::describe::{Declaration, EnvClass, EnvVar, Vocabulary};
    use knowledge_collector_framework::prelude::*;

    // A BRACED empty struct, not a unit one: a unit struct infers the JSON
    // Schema type "null" and the params check refuses it first, which would make
    // this test pass for the wrong reason.
    #[derive(Debug, Default, serde::Deserialize, schemars::JsonSchema)]
    struct NoParams {}

    struct BadDeclaration;
    impl Collector for BadDeclaration {
        type Params = NoParams;
        fn tool(&self) -> ToolSpec {
            ToolSpec::new("collect", "")
        }
        fn declaration(&self) -> Declaration {
            Declaration {
                // A blank environment name, which the client's own env block
                // refuses; the declaration must not be able to render one.
                env: vec![EnvVar::new("", EnvClass::Path)],
                vocabulary: Vocabulary::new(vec!["issue".into()], vec![]),
                ..Declaration::default()
            }
        }
        fn walk(
            &self,
            _id: &str,
            _params: NoParams,
            _foreign: &ForeignContext,
        ) -> Result<WalkResult, WalkError> {
            Ok(WalkResult::new(vec![], vec![], Completeness::complete()))
        }
    }

    let err = CollectorServer::new(BadDeclaration)
        .err()
        .expect("a malformed declaration must refuse the server");
    assert!(err.message().contains("blank name"), "{err}");

    // THE CONTROL: the same collector with a well-formed declaration serves.
    let server = CollectorServer::new(SampleCollector).expect("the sample serves");
    assert_eq!(conforms(server.declaration()), Vec::<String>::new());
}

// ---------------------------------------------------------------------------
// THE FOUR DESCRIBE-FAILURE MODES, each failing in its own way. The client's
// dialing tests assert these; this file asserts them without the client, so a
// payload defect reds here rather than in a seamed run read backwards.
// ---------------------------------------------------------------------------
#[test]
fn the_no_describe_mode_serves_the_collect_tool_alone() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::NoDescribe.as_str())]);
    child.initialize();
    let tools = child.list_tools();
    assert_eq!(tools.len(), 1, "{tools:?}");
    assert_eq!(tools[0]["name"], json!(DEFAULT_STUB_TOOL));

    // THE CONTROL, through the same reader: an ordinary mode serves both.
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::Conforming.as_str())]);
    child.initialize();
    assert_eq!(child.list_tools().len(), 2);
}

#[test]
fn the_bad_describe_schema_mode_advertises_a_schema_missing_the_vocabulary() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::BadDescribeSchema.as_str())]);
    child.initialize();
    let tools = child.list_tools();
    let describe = tools
        .iter()
        .find(|t| t["name"] == json!(DESCRIBE_TOOL_NAME))
        .expect("this mode DOES serve the tool; what it bends is the schema");

    let required: Vec<&str> = describe["outputSchema"]["required"]
        .as_array()
        .expect("required is an array")
        .iter()
        .filter_map(Value::as_str)
        .collect();
    assert!(
        !required.contains(&"node_types"),
        "the bent schema must not require the node vocabulary: {required:?}"
    );
    assert!(
        describe["outputSchema"]["properties"]
            .get("node_types")
            .is_none(),
        "and must not declare it either"
    );
    // THE CONTROL: the conforming mode's schema DOES require it.
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::Conforming.as_str())]);
    child.initialize();
    let tools = child.list_tools();
    let good = tools
        .iter()
        .find(|t| t["name"] == json!(DESCRIBE_TOOL_NAME))
        .expect("served");
    assert_eq!(good["outputSchema"], describe_schema());
}

#[test]
fn the_bad_declaration_mode_serves_a_good_schema_and_breaks_it() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::BadDeclaration.as_str())]);
    child.initialize();
    let tools = child.list_tools();
    let describe = tools
        .iter()
        .find(|t| t["name"] == json!(DESCRIBE_TOOL_NAME))
        .expect("served");
    assert_eq!(
        describe["outputSchema"],
        describe_schema(),
        "this mode's SCHEMA is conforming — the declaration is what breaks its word"
    );

    let result = child.call_tool(DESCRIBE_TOOL_NAME, json!({}));
    let declaration = result["structuredContent"].clone();
    assert!(
        declaration.get("behavior").is_none(),
        "the declaration must omit the required behavior key: {declaration}"
    );
    assert!(
        !conforms(&declaration).is_empty(),
        "and must therefore fail the schema it advertised"
    );
    // THE CONTROL: the conforming mode's declaration passes the same validator.
    assert_eq!(conforms(&conforming_declaration()), Vec::<String>::new());
}

#[test]
fn the_describe_error_mode_reports_an_error_result() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::DescribeError.as_str())]);
    child.initialize();
    let result = child.call_tool(DESCRIBE_TOOL_NAME, json!({}));
    assert!(is_error(&result), "{result}");
    assert_eq!(error_text(&result), DESCRIBE_ERROR_TEXT);

    // ...AND ITS COLLECT TOOL STILL WORKS. The arm is a provider that cannot
    // describe itself, not one that is broken: a mode that failed both would not
    // isolate the describe path.
    let collect = child.call_tool(DEFAULT_STUB_TOOL, json!({"id": "probe"}));
    assert!(!is_error(&collect), "{}", error_text(&collect));
}
