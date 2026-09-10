// SPDX-License-Identifier: Apache-2.0

//! handshake.rs — what reaches the WIRE: the negotiated revision, the server
//! identity, the advertised schemas as a tool listing carries them, and the rule
//! that stdout carries nothing but JSON-RPC.
//!
//! Rows R1.10, R1.11, R1.15, and the wire half of R1.2 and R1.3. None of these
//! is observable from a function return, which is why the whole file drives a
//! spawned binary.

mod common;

use knowledge_collector_framework::conformance::{Mode, STUB_MODE_ENV};
use knowledge_collector_framework::contract::check_tool_schemas;
use knowledge_collector_framework::schema::{input_contract_json, output_contract_json};
use knowledge_collector_framework::serve::PROTOCOL_VERSION;
use serde_json::{json, Value};

use common::{error_text, is_error, McpChild, CLIENT_REQUESTED_PROTOCOL_VERSION};

const SAMPLE: &str = env!("CARGO_BIN_EXE_sample-rs");
const STUB: &str = env!("CARGO_BIN_EXE_conformance-stub");

fn sample() -> McpChild {
    McpChild::spawn(SAMPLE, &[])
}

// ---------------------------------------------------------------------------
// R1.11 — the handshake negotiates 2025-06-18, and the PIN is the only thing
// that decides the answer.
//
// The client REQUESTS a newer revision (2026-07-28, which is what the knowledge
// client's own SDK asks for) and is answered the pinned one, because rmcp echoes
// what the SERVER declares rather than what the client asked for. So the request
// is the control: change the pinned constant and this row reds even though the
// request is unchanged.
// ---------------------------------------------------------------------------
#[test]
fn the_handshake_answers_the_pinned_revision() {
    let mut child = sample();
    let result = child.initialize();
    assert_eq!(
        result["protocolVersion"],
        json!(PROTOCOL_VERSION.as_str()),
        "the handshake answers the revision this crate pins"
    );
    assert_eq!(
        result["protocolVersion"],
        json!("2025-06-18"),
        "and that revision is the collector contract's feature floor, stated as a literal here \
         so a change to the constant cannot silently move the wire"
    );
    assert_ne!(
        result["protocolVersion"],
        json!(CLIENT_REQUESTED_PROTOCOL_VERSION),
        "control: the client asked for a NEWER revision, so the answer comes from the pin"
    );
}

// ---------------------------------------------------------------------------
// A post-handshake request whose own metadata names a newer revision is still
// served. That is why the supported-version list is left at the SDK's default:
// narrowing it to the pinned revision alone would refuse exactly what a current
// client sends.
// ---------------------------------------------------------------------------
#[test]
fn a_request_naming_a_newer_revision_is_still_served() {
    let mut child = sample();
    child.initialize();
    child.send_raw(
        &serde_json::to_string(&json!({
            "jsonrpc": "2.0", "id": 77, "method": "tools/list", "params": {},
            "_meta": {"protocolVersion": CLIENT_REQUESTED_PROTOCOL_VERSION}
        }))
        .expect("a frame serializes"),
    );
    loop {
        let line = child.read_line().expect("the server answers");
        let value: Value = serde_json::from_str(&line).expect("the answer is JSON-RPC");
        if value.get("id").and_then(Value::as_i64) == Some(77) {
            assert!(
                value.get("error").is_none(),
                "a request naming a newer revision must still be served: {value}"
            );
            break;
        }
    }
}

// ---------------------------------------------------------------------------
// R1.10 — serverInfo.name on the wire equals the TOOL NAME.
// ---------------------------------------------------------------------------
#[test]
fn server_info_name_is_the_tool_name() {
    let mut child = sample();
    let result = child.initialize();
    let tools = child.list_tools();
    // TWO TOOLS: the collect tool and the REQUIRED declaration tool. The lookup
    // is BY NAME and asserts no cardinality beyond that, which is how the
    // client's own tool lookup is written — a suite that asserted "one tool" is
    // exactly what had to be rewritten in the Go framework the day describe
    // arrived.
    let collect = tools
        .iter()
        .find(|t| t["name"] == json!("collect"))
        .expect("the collect tool is served");
    assert!(
        tools.iter().any(|t| t["name"] == json!("describe")),
        "the declaration tool is REQUIRED and must be served beside the collect tool: {tools:?}"
    );
    assert_eq!(
        result["serverInfo"]["name"],
        collect["name"],
        "serverInfo.name is the served tool's name; nothing in the client reads it, and naming \
         the tool is what makes a handshake log line locate the operator's config entry"
    );
    assert_eq!(result["serverInfo"]["name"], json!("collect"));
}

// ---------------------------------------------------------------------------
// R1.2 and R1.3 on the wire — the tool listing carries the contract documents,
// with only params spliced, and the whole pair is admissible.
// ---------------------------------------------------------------------------
#[test]
fn the_tool_listing_carries_the_contract_documents() {
    let mut child = sample();
    child.initialize();
    let tools = child.list_tools();
    let tool = tools
        .iter()
        .find(|t| t["name"] == json!("collect"))
        .expect("the collect tool is served");

    let advertised_output = tool
        .get("outputSchema")
        .expect("the tool advertises an output schema");
    let embedded_output: Value =
        serde_json::from_slice(&output_contract_json()).expect("the contract decodes");
    assert_eq!(
        advertised_output, &embedded_output,
        "the advertised output schema is the checked-in document, verbatim, on the wire"
    );

    let advertised_input = tool
        .get("inputSchema")
        .expect("the tool advertises an input schema");
    let mut embedded_input: Value =
        serde_json::from_slice(&input_contract_json()).expect("the contract decodes");
    // The contract file's own description strings reach the wire unchanged;
    // assert one of them rather than only the structure.
    assert_eq!(
        advertised_input["properties"]["id"]["description"],
        embedded_input["properties"]["id"]["description"],
        "the contract file's own property descriptions reach the wire"
    );
    assert_eq!(advertised_input["required"], json!(["id"]));

    // Everything outside properties.params is the contract document.
    let mut stripped = advertised_input.clone();
    stripped["properties"]
        .as_object_mut()
        .expect("properties")
        .remove("params");
    embedded_input["properties"]
        .as_object_mut()
        .expect("properties")
        .remove("params");
    assert_eq!(stripped, embedded_input);

    // And the pair the wire carries is admissible by the contract's own gate.
    check_tool_schemas(
        tool["name"].as_str().expect("the tool has a name"),
        Some(advertised_input),
        Some(advertised_output),
    )
    .expect("what this crate puts on the wire must satisfy the contract it ships");
}

// ---------------------------------------------------------------------------
// R1.15 — STDOUT CARRIES NOTHING BUT JSON-RPC.
//
// Every byte the server writes to stdout across a whole session parses as a
// JSON-RPC frame. The mutation that reds it is a `println!` anywhere on the
// serving path; a corpus check scans the crate for the same shape statically,
// and this is the runtime half.
// ---------------------------------------------------------------------------
#[test]
fn stdout_carries_nothing_but_json_rpc() {
    let mut child = sample();
    child.initialize();
    child.list_tools();
    let root = std::env::temp_dir();
    let result = child.call_tool(
        "collect",
        json!({"id": "probe", "params": {"root": root.to_string_lossy(), "max_depth": 1}}),
    );
    assert!(!is_error(&result), "{}", error_text(&result));

    // Drain whatever else the server wrote, and parse every line.
    child.send_raw(&serde_json::to_string(&json!({"jsonrpc": "2.0", "id": 4242, "method": "ping", "params": {}})).expect("a frame serializes"));
    let mut frames = 0usize;
    while let Some(line) = child.read_line() {
        if line.trim().is_empty() {
            continue;
        }
        let value: Value = serde_json::from_str(&line)
            .unwrap_or_else(|e| panic!("stdout carried a line that is not JSON-RPC ({e}): {line}"));
        assert_eq!(
            value["jsonrpc"],
            json!("2.0"),
            "every stdout line is a JSON-RPC frame: {line}"
        );
        frames += 1;
        if value.get("id").and_then(Value::as_i64) == Some(4242) {
            break;
        }
    }
    assert!(frames > 0, "the server answered at least one frame");
}

// ---------------------------------------------------------------------------
// A tool the server does not serve is a PROTOCOL error, not a tool error: the
// request could not be routed at all, which is a different fact from a tool that
// ran and refused.
// ---------------------------------------------------------------------------
#[test]
fn an_unknown_tool_is_a_protocol_error() {
    let mut child = sample();
    child.initialize();
    let response = child.call_tool_raw("no_such_tool", json!({"id": "probe"}));
    let error = response
        .get("error")
        .expect("an unknown tool is refused as a protocol error");
    assert_eq!(error["code"], json!(-32601));
}

// ---------------------------------------------------------------------------
// A refused collect reaches the caller as an isError result carrying TEXT, over
// the wire and not only from the handler. That is the transport half of R1.9.
// ---------------------------------------------------------------------------
#[test]
fn a_refused_collect_reaches_the_wire_as_is_error_with_text() {
    let mut child = sample();
    child.initialize();
    let result = child.call_tool("collect", json!({"id": ""}));
    assert!(is_error(&result), "an empty collect id is refused: {result}");
    assert!(
        error_text(&result).contains("the collect id is empty"),
        "{}",
        error_text(&result)
    );

    // A PANICKING walk too: the process must still be alive to answer the next
    // call, which is what distinguishes a contained panic from a dead provider.
    let result = child.call_tool(
        "collect",
        json!({"id": "probe", "params": {"root": ".", "demo_panic": true}}),
    );
    assert!(is_error(&result));
    assert!(error_text(&result).contains("PANICKED"), "{}", error_text(&result));

    let tools = child.list_tools();
    assert_eq!(
        tools.len(),
        2,
        "the provider survived the panic and still answers with both tools"
    );
}

// ---------------------------------------------------------------------------
// The stub's conforming mode puts the reference payload on the wire byte for
// byte, which is the crate-side half of the client's own both-transports
// equality.
// ---------------------------------------------------------------------------
#[test]
fn the_conforming_stub_payload_survives_the_transport_unchanged() {
    use knowledge_collector_framework::conformance::conforming_payload;

    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::Conforming.as_str())]);
    child.initialize();
    let result = child.call_tool("collect_graph", json!({"id": "probe"}));
    assert_eq!(
        result["structuredContent"], conforming_payload(),
        "the payload the client asserts must reach the wire unchanged"
    );
}
