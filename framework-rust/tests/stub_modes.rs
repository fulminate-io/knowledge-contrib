// SPDX-License-Identifier: Apache-2.0

//! stub_modes.rs — the CONFORMANCE STUB, mode by mode.
//!
//! Rows R2.3, R2.4, R2.5, R2.6 and R2.7. The client's own dialing tests assert
//! these payloads from the far side of a process boundary; these rows assert
//! them HERE, so a payload defect turns red in this crate's CI rather than in a
//! seamed run that has to be read backwards.
//!
//! THE MODE LIST IS DERIVED, NEVER RETYPED. Every row below iterates
//! `conformance::STUB_MODES`, which is the one declaration the stub's own
//! dispatch reads. A hand-typed copy of the vocabulary in a second place is
//! exactly the defect that lets a port claim a mode it does not serve.

mod common;

use std::collections::BTreeSet;

use knowledge_collector_framework::conformance::{
    conforming_payload, over_former_cap_payload, Mode, DEFAULT_STUB_TOOL, OVER_FORMER_CAP_NODES,
    STUB_MODES, STUB_MODE_ENV, STUB_STDERR_MARKER, TOOL_ERROR_TEXT,
};
use knowledge_collector_framework::contract::{check_tool_schemas, validate_result_payload};
use serde_json::{json, Value};

use common::{error_text, is_error, McpChild};

const STUB: &str = env!("CARGO_BIN_EXE_conformance-stub");
const SAMPLE: &str = env!("CARGO_BIN_EXE_sample-rs");

// ---------------------------------------------------------------------------
// R2.3 — the stub is a SEPARATE BINARY from the sample collector.
//
// The distinction is a reproduced fact rather than a preference: the client's
// dialing tests assert this stub's exact payloads and strings, and the sample
// walks a directory, so a suite driven against the sample would fail on the
// first payload assertion. This row exists to stop the shortcut being taken and
// then diagnosed.
// ---------------------------------------------------------------------------
#[test]
fn the_stub_and_the_sample_are_two_different_binaries() {
    assert_ne!(STUB, SAMPLE, "the two binary targets are distinct");
    let stub = std::fs::read(STUB).expect("the stub binary exists");
    let sample = std::fs::read(SAMPLE).expect("the sample binary exists");
    assert_ne!(
        stub, sample,
        "the conformance stub and the sample collector must be different programs"
    );
}

// ---------------------------------------------------------------------------
// R2.4 — EVERY mode is emulated, and the vocabulary is complete and unique.
// ---------------------------------------------------------------------------
#[test]
fn the_mode_vocabulary_is_complete_and_unique() {
    let names: BTreeSet<&str> = STUB_MODES.iter().map(Mode::as_str).collect();
    assert_eq!(
        names.len(),
        STUB_MODES.len(),
        "no mode name may appear twice"
    );
    for mode in STUB_MODES {
        assert_eq!(
            Mode::parse(mode.as_str()).expect("every declared mode parses"),
            mode
        );
    }
    // A name OUTSIDE the vocabulary is refused naming it, so a stub asked for a
    // mode it does not serve exits rather than silently serving a default.
    let err = Mode::parse("no-such-mode").expect_err("an unknown mode must be refused");
    assert!(err.contains("no-such-mode"), "{err}");
}

// ---------------------------------------------------------------------------
// R2.4 — one row per mode, over the payload and the advertised schemas, driven
// directly without the client.
// ---------------------------------------------------------------------------
#[test]
fn every_mode_advertises_and_answers_what_the_client_asserts() {
    let no_args = json!({"id": "probe"});

    for mode in STUB_MODES {
        let name = mode.as_str();
        let input = Value::Object(mode.input_schema());
        let output = mode.output_schema().map(Value::Object);
        let tool = mode.tool_name("");

        match mode {
            Mode::NoSuchTool => assert_eq!(tool, "some_other_tool", "[{name}]"),
            _ => assert_eq!(tool, DEFAULT_STUB_TOOL, "[{name}]"),
        }

        // The four SCHEMA-BENDING modes are the ones the client's schema gate
        // must refuse; every other mode advertises the contract verbatim and is
        // admitted. That split is the assertion, run through this crate's own
        // comparator.
        let admitted = check_tool_schemas(&tool, Some(&input), output.as_ref());
        match mode {
            Mode::BadInputSchema | Mode::NoOutputSchema | Mode::BadOutputSchema => {
                let err = admitted
                    .unwrap_err_or_panic(name);
                assert!(err.contains(&tool), "[{name}] the refusal names the tool: {err}");
            }
            _ => {
                admitted.unwrap_or_else(|e| {
                    panic!("[{name}] this mode must advertise an admissible pair: {e}")
                });
            }
        }

        // The PAYLOAD each mode answers with. The two process-death modes and the
        // tool-error mode never produce one.
        if matches!(
            mode,
            Mode::ExitBeforeHandshake | Mode::ExitMidSession | Mode::ToolError
        ) {
            continue;
        }
        let payload = mode.payload(&no_args);
        let raw = serde_json::to_vec(&payload).expect("a payload serializes");
        let conforming = validate_result_payload(&tool, &raw);
        match mode {
            // The payload arms the client's own result gate must refuse.
            Mode::EmptyNodeType => {
                assert_eq!(payload["nodes"][0]["type"], json!(""), "[{name}]");
            }
            Mode::UnknownField => {
                assert_eq!(payload["nodes"][0]["summry"], json!("typo'd key"), "[{name}]");
                conforming.unwrap_or_else(|e| {
                    panic!("[{name}] an undefined FIELD still satisfies the contract schema, \
                            which is why the strict decode is a separate gate: {e}")
                });
            }
            Mode::DanglingEdge => {
                assert_eq!(payload["edges"][0]["to_id"], json!("absent"), "[{name}]");
                conforming.expect("[dangling-edge] a dangling edge is admitted deliberately");
            }
            Mode::IncompleteWalk => {
                assert_eq!(payload["walk_complete"], json!(false), "[{name}]");
                conforming.expect("[incomplete-walk] an incomplete walk is conforming");
            }
            Mode::EmptyGraph => {
                assert_eq!(payload["nodes"], json!([]), "[{name}]");
                assert_eq!(payload["edges"], json!([]), "[{name}]");
                conforming.expect("[empty-graph] an empty graph is conforming");
            }
            Mode::BreaksOwnWord => {
                // It satisfies the CONTRACT and violates the schema it itself
                // advertised, which is the whole arm.
                conforming.expect("[breaks-own-word] the payload satisfies the contract");
                assert!(
                    payload["nodes"][0].get("summary").is_none(),
                    "[{name}] the node must lack the summary its own schema required"
                );
            }
            Mode::EnvReport => {
                assert_eq!(payload["nodes"], json!([]), "[{name}] no names asked, no nodes");
                conforming.expect("[env-report] an empty report is conforming");
            }
            Mode::OverFormerCap => {
                assert_eq!(
                    payload["nodes"].as_array().expect("nodes").len(),
                    OVER_FORMER_CAP_NODES,
                    "[{name}]"
                );
                conforming.expect("[over-former-cap] the payload is conforming");
            }
            _ => {
                conforming.unwrap_or_else(|e| {
                    panic!("[{name}] this mode's payload must satisfy the contract: {e}")
                });
            }
        }
    }
}

/// A tiny helper so a row can name the mode in its panic message.
trait UnwrapErrOrPanic {
    fn unwrap_err_or_panic(self, mode: &str) -> String;
}

impl UnwrapErrOrPanic for Result<(), knowledge_collector_framework::Error> {
    fn unwrap_err_or_panic(self, mode: &str) -> String {
        match self {
            Ok(()) => panic!("[{mode}] this mode's advertised pair must be REFUSED"),
            Err(e) => e.to_string(),
        }
    }
}

// ---------------------------------------------------------------------------
// R2.5 — the conforming payload's three nodes, and the absent-versus-
// present-and-empty distinction that survives serialization.
// ---------------------------------------------------------------------------
#[test]
fn the_conforming_payload_carries_the_absent_and_the_empty_node() {
    let payload = conforming_payload();
    let nodes = payload["nodes"].as_array().expect("nodes");
    assert_eq!(nodes.len(), 3);

    let full = nodes[0].as_object().expect("an object");
    assert_eq!(full["id"], json!("ISSUE-1"));
    assert_eq!(full["metadata"], json!({"priority": "high"}));
    for field in [
        "symbol_name",
        "file_path",
        "language",
        "start_line",
        "end_line",
        "content",
        "signature",
        "summary",
        "description",
        "source",
        "status",
        "keywords",
        "is_exported",
        "metadata",
    ] {
        assert!(full.contains_key(field), "ISSUE-1 sets {field}");
    }

    // ISSUE-2 OMITS its optional fields.
    let absent = nodes[1].as_object().expect("an object");
    assert_eq!(
        absent.keys().collect::<Vec<_>>(),
        vec!["id", "type"],
        "ISSUE-2 carries ONLY id and type"
    );

    // ISSUE-3 carries them PRESENT AND EMPTY. Absent and empty-valued are
    // distinct inputs to both the schema validation and the strict decode, so
    // the two nodes are two cells rather than a duplicate.
    let empty = nodes[2].as_object().expect("an object");
    // EXACTLY the contract's node vocabulary and nothing else: a key the
    // envelope does not define would be dropped by the client's strict decode,
    // and this node's whole purpose is to be decoded field for field.
    let mut empty_keys: Vec<&str> = empty.keys().map(String::as_str).collect();
    empty_keys.sort_unstable();
    assert_eq!(
        empty_keys,
        vec![
            "content", "description", "end_line", "file_path", "id", "is_exported", "keywords",
            "language", "metadata", "signature", "source", "start_line", "status", "summary",
            "symbol_name", "type",
        ],
        "ISSUE-3 carries every optional field present-and-empty, and no field the contract \
         does not define"
    );
    assert_eq!(empty["symbol_name"], json!(""));
    assert_eq!(empty["start_line"], json!(0));
    assert_eq!(empty["is_exported"], json!(false));
    assert_eq!(empty["metadata"], json!({}));

    let edge = payload["edges"][0].as_object().expect("an object");
    assert_eq!(edge["from_id"], json!("ISSUE-1"));
    assert_eq!(edge["to_id"], json!("ISSUE-2"));
    assert_eq!(edge["type"], json!("blocks"));
    assert_eq!(payload["walk_complete"], json!(true));
}

// ---------------------------------------------------------------------------
// The env-report mode answers ONE NODE PER NAME ASKED and never serializes the
// whole environment. A stub that dumped its environment into a result the
// collect then admitted would write whatever the process held into a graph.
// ---------------------------------------------------------------------------
#[test]
fn env_report_answers_only_the_names_asked_and_never_the_environment() {
    let mut child = McpChild::spawn(
        STUB,
        &[
            (STUB_MODE_ENV, Mode::EnvReport.as_str()),
            ("FUL1776_DECLARED", "declared-value"),
            ("FUL1776_EMPTY", ""),
        ],
    );
    child.initialize();
    let result = child.call_tool(
        DEFAULT_STUB_TOOL,
        json!({
            "id": "probe",
            "params": {"names": ["FUL1776_DECLARED", "FUL1776_ABSENT", "FUL1776_EMPTY"]}
        }),
    );
    let nodes = result["structuredContent"]["nodes"]
        .as_array()
        .expect("the report carries nodes");
    assert_eq!(nodes.len(), 3, "one node per name asked, and no more");

    assert_eq!(nodes[0]["id"], json!("FUL1776_DECLARED"));
    assert_eq!(nodes[0]["type"], json!("env_var"));
    assert_eq!(nodes[0]["metadata"]["present"], json!("true"));
    assert_eq!(nodes[0]["metadata"]["value"], json!("declared-value"));

    // AN ABSENT NAME reports present=false and carries NO value key.
    assert_eq!(nodes[1]["id"], json!("FUL1776_ABSENT"));
    assert_eq!(nodes[1]["metadata"]["present"], json!("false"));
    assert!(nodes[1]["metadata"].get("value").is_none());

    // A PRESENT-AND-EMPTY name is present with an empty value, which is a
    // different fact from absent.
    assert_eq!(nodes[2]["metadata"]["present"], json!("true"));
    assert_eq!(nodes[2]["metadata"]["value"], json!(""));

    // AND THE ENVIRONMENT IS NOT IN THE RESULT. The child was spawned with PATH
    // carried, so PATH is the known positive: it IS in the child's environment
    // and is NOT in the report.
    let text = serde_json::to_string(&result).expect("the result serializes");
    assert!(
        !text.contains("PATH"),
        "the report must carry only the names asked: {text}"
    );
}

// ---------------------------------------------------------------------------
// R2.6 — the two process-death modes actually KILL THE PROCESS, at their two
// different points.
//
// A stub that returned an error instead would reach different client code and
// the corresponding sub-rows would go green for the wrong reason.
// ---------------------------------------------------------------------------
#[test]
fn exit_before_handshake_dies_before_writing_a_frame() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::ExitBeforeHandshake.as_str())]);
    std::thread::sleep(std::time::Duration::from_millis(300));
    let stderr = child.drain_stderr().join("\n");
    assert!(
        stderr.contains(STUB_STDERR_MARKER),
        "the dial marker is written even by the mode that dies first: {stderr}"
    );
    assert_eq!(
        child.wait_for_exit(),
        Some(3),
        "exit-before-handshake exits 3 without speaking MCP"
    );
}

#[test]
fn exit_mid_session_serves_the_handshake_then_dies_inside_the_call() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::ExitMidSession.as_str())]);
    child.initialize();
    let tools = child.list_tools();
    assert_eq!(
        tools.len(),
        2,
        "this mode is listed and schema-verified BEFORE it dies, with both tools"
    );
    assert!(tools.iter().any(|t| t["name"] == json!(DEFAULT_STUB_TOOL)));

    // The call never answers: the child dies with it in flight.
    child.send_raw(
        &serde_json::to_string(&json!({
            "jsonrpc": "2.0", "id": 99, "method": "tools/call",
            "params": {"name": DEFAULT_STUB_TOOL, "arguments": {"id": "probe"}}
        }))
        .expect("a frame serializes"),
    );
    while let Some(line) = child.read_line() {
        if line.contains(r#""id":99"#) {
            panic!("exit-mid-session answered the call instead of dying: {line}");
        }
    }
    assert_eq!(
        child.wait_for_exit(),
        Some(7),
        "exit-mid-session exits 7 INSIDE the tool call"
    );
}

// ---------------------------------------------------------------------------
// A child with NO mode exits loud rather than guessing one. The mode is
// delivered through the entry's env block, which is itself under test, so its
// absence is the interesting failure.
// ---------------------------------------------------------------------------
#[test]
fn a_stub_with_no_mode_exits_loud() {
    let child = McpChild::spawn(STUB, &[]);
    assert_eq!(child.wait_for_exit(), Some(9));
}

// ---------------------------------------------------------------------------
// R2.7 — over-former-cap emits MORE THAN 64 MiB and the process survives it,
// measured by the test rather than claimed by the stub.
// ---------------------------------------------------------------------------
#[test]
fn over_former_cap_crosses_the_retired_bound_and_the_process_survives() {
    const FORMER_CAP: usize = 64 << 20;

    // The builder's own size, measured.
    let payload = over_former_cap_payload();
    let built = serde_json::to_vec(&payload).expect("the payload serializes");
    assert!(
        built.len() > FORMER_CAP,
        "the payload must cross the retired 64 MiB bound: it is {} bytes",
        built.len()
    );

    // And it survives the transport, which is the half a builder cannot show.
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::OverFormerCap.as_str())]);
    child.initialize();
    let result = child.call_tool(DEFAULT_STUB_TOOL, json!({"id": "probe"}));
    let nodes = result["structuredContent"]["nodes"]
        .as_array()
        .expect("the over-cap result carries nodes");
    assert_eq!(nodes.len(), OVER_FORMER_CAP_NODES);
    let on_the_wire = serde_json::to_vec(&result).expect("the result serializes");
    assert!(
        on_the_wire.len() > FORMER_CAP,
        "the result that crossed the transport is {} bytes",
        on_the_wire.len()
    );
    assert!(!is_error(&result));
}

// ---------------------------------------------------------------------------
// The tool-error mode returns the LITERAL string the client's own test asserts.
// ---------------------------------------------------------------------------
#[test]
fn tool_error_returns_the_literal_refusal() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::ToolError.as_str())]);
    child.initialize();
    let result = child.call_tool(DEFAULT_STUB_TOOL, json!({"id": "probe"}));
    assert!(is_error(&result), "the tool-error mode answers isError");
    // THE LITERAL, not the constant. The client's own dialing test asserts this
    // exact string, so comparing the stub's output against the stub's own
    // constant would be a subject supplying its own answer key.
    assert_eq!(error_text(&result), "the stub provider refused this collect");
    assert_eq!(
        TOOL_ERROR_TEXT, "the stub provider refused this collect",
        "and the constant the crate exports is that same literal"
    );
}

// ---------------------------------------------------------------------------
// The no-such-tool mode serves a DIFFERENT tool name, which is what the client's
// own no-such-tool arm dials for.
// ---------------------------------------------------------------------------
#[test]
fn no_such_tool_serves_a_different_tool() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::NoSuchTool.as_str())]);
    child.initialize();
    let tools = child.list_tools();
    // The COLLECT tool is the one renamed; the declaration tool keeps its fixed
    // name, because the client must know what to call before it can call
    // anything. So this mode is "no such COLLECT tool", not "no tools".
    assert!(
        tools.iter().any(|t| t["name"] == json!("some_other_tool")),
        "{tools:?}"
    );
    assert!(
        !tools.iter().any(|t| t["name"] == json!(DEFAULT_STUB_TOOL)),
        "the collect tool must NOT be served under its usual name in this mode: {tools:?}"
    );
}

// ---------------------------------------------------------------------------
// R2.2a's positive half, crate-side: the dial marker is on stderr for an
// ordinary mode too, and nothing of it reaches stdout.
// ---------------------------------------------------------------------------
#[test]
fn every_dial_writes_the_marker_to_stderr_and_never_to_stdout() {
    let mut child = McpChild::spawn(STUB, &[(STUB_MODE_ENV, Mode::Conforming.as_str())]);
    let handshake = child.initialize();
    assert_eq!(handshake["protocolVersion"], json!("2025-06-18"));

    let stderr = child.drain_stderr().join("\n");
    assert!(
        stderr.contains(STUB_STDERR_MARKER),
        "the stub announces every dial on stderr: {stderr}"
    );
    assert!(
        stderr.contains(Mode::Conforming.as_str()),
        "the marker names the mode it dialed in: {stderr}"
    );

    let result = child.call_tool(DEFAULT_STUB_TOOL, json!({"id": "probe"}));
    assert_eq!(result["structuredContent"], conforming_payload());
}
