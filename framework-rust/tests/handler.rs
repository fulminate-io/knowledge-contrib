// SPDX-License-Identifier: Apache-2.0

//! handler.rs — what the served tool does with a call, BEFORE and AFTER the
//! walk.
//!
//! Rows R1.4a (the three refusal arms plus the schema-only arm), R1.8, R1.9,
//! R1.10 and R1.13.
//!
//! R1.4a EXISTS BECAUSE THE PREMISE THAT THE SDK VALIDATES WAS MEASURED FALSE.
//! An rmcp server advertising the contract input schema verbatim reaches its
//! handler with the required property missing, with a wrong-typed property and
//! with an undeclared property, each returning `isError: false`. The Go
//! framework's promise of an already-validated params value is the SDK's there
//! and this crate's here.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use knowledge_collector_framework::describe::{Declaration, Vocabulary};
use knowledge_collector_framework::prelude::*;
use knowledge_collector_framework::serve::CollectorServer;
use schemars::{json_schema, JsonSchema, SchemaGenerator};
use serde::Deserialize;
use serde_json::{json, Value};

// ---------------------------------------------------------------------------
// A collector that records what its walk was handed, so a test can assert that
// the walk was NOT reached as well as that a refusal was returned.
// ---------------------------------------------------------------------------
#[derive(Debug, Default, Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct Params {
    #[serde(default)]
    root: String,
}

#[derive(Clone)]
struct Recorder {
    walks: Arc<AtomicUsize>,
    seen_root: Arc<std::sync::Mutex<String>>,
    seen_families: Arc<std::sync::Mutex<Vec<String>>>,
}

impl Recorder {
    fn new() -> Recorder {
        Recorder {
            walks: Arc::new(AtomicUsize::new(0)),
            seen_root: Arc::new(std::sync::Mutex::new(String::new())),
            seen_families: Arc::new(std::sync::Mutex::new(Vec::new())),
        }
    }
}

impl Collector for Recorder {
    type Params = Params;

    fn tool(&self) -> ToolSpec {
        ToolSpec::new("collect", "a recorder")
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
        params: Params,
        foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError> {
        self.walks.fetch_add(1, Ordering::SeqCst);
        *self.seen_root.lock().expect("the recorder's lock") = params.root;
        *self.seen_families.lock().expect("the recorder's lock") = foreign
            .families()
            .into_iter()
            .map(str::to_string)
            .collect();
        Ok(WalkResult::new(
            vec![Node::new("n1", "issue")],
            vec![],
            Completeness::complete(),
        ))
    }
}

fn recording_server() -> (CollectorServer<Recorder>, Recorder) {
    let recorder = Recorder::new();
    let server = CollectorServer::new(recorder.clone()).expect("the recorder serves");
    (server, recorder)
}

// ---------------------------------------------------------------------------
// R1.4a (i) — a call with NO id is refused before the walk.
// ---------------------------------------------------------------------------
#[test]
fn a_call_with_no_id_is_refused_before_the_walk() {
    let (server, recorder) = recording_server();
    let err = server
        .handle_collect(&json!({}))
        .expect_err("a call with no id must be refused");
    assert!(
        err.contains("id"),
        "the refusal must name the missing property: {err}"
    );
    assert_eq!(
        recorder.walks.load(Ordering::SeqCst),
        0,
        "the walk must not run for a call the schema refuses"
    );
}

// ---------------------------------------------------------------------------
// R1.4a (ii) — a call whose `params` does not satisfy the collector's OWN
// declared params schema is refused before the walk.
//
// The wrong-typed case is the one serde alone catches. The BOUNDED case below it
// is the one only the schema check catches, and it is what makes
// validate_call_arguments a guard rather than a decoration.
// ---------------------------------------------------------------------------
#[test]
fn a_call_whose_params_do_not_satisfy_the_declared_schema_is_refused() {
    let (server, recorder) = recording_server();
    let err = server
        .handle_collect(&json!({"id": "probe", "params": "a string"}))
        .expect_err("params typed as a string where the collector declares an object must be refused");
    assert!(
        err.contains("params"),
        "the refusal must name the offending property: {err}"
    );
    assert_eq!(recorder.walks.load(Ordering::SeqCst), 0);
}

/// BoundedParams declares a keyword a Rust type cannot express by itself: a
/// minimum. Its `JsonSchema` impl is hand-written so the assertion is about the
/// SCHEMA check rather than about a derive attribute's spelling.
#[derive(Debug, Default, Deserialize)]
#[serde(deny_unknown_fields)]
struct BoundedParams {
    #[serde(default)]
    depth: u32,
}

impl JsonSchema for BoundedParams {
    fn schema_name() -> std::borrow::Cow<'static, str> {
        "BoundedParams".into()
    }
    fn json_schema(_: &mut SchemaGenerator) -> schemars::Schema {
        json_schema!({
            "type": "object",
            "properties": {"depth": {"type": "integer", "minimum": 1}}
        })
    }
}

struct Bounded {
    walks: Arc<AtomicUsize>,
}

impl Collector for Bounded {
    type Params = BoundedParams;
    fn tool(&self) -> ToolSpec {
        ToolSpec::new("collect", "a collector with a bounded param")
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
        _params: BoundedParams,
        _foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError> {
        self.walks.fetch_add(1, Ordering::SeqCst);
        Ok(WalkResult::new(vec![], vec![], Completeness::complete()))
    }
}

#[test]
fn a_params_value_outside_the_declared_bound_is_refused_by_the_schema_check() {
    let walks = Arc::new(AtomicUsize::new(0));
    let server = CollectorServer::new(Bounded {
        walks: walks.clone(),
    })
    .expect("the bounded collector serves");

    // THE CONTROL, in the same run and through the same path: a value INSIDE the
    // bound reaches the walk, so the refusal below is about the bound and not
    // about the check refusing everything.
    server
        .handle_collect(&json!({"id": "probe", "params": {"depth": 3}}))
        .expect("control: a params value inside the declared bound is admitted");
    assert_eq!(walks.load(Ordering::SeqCst), 1);

    let err = server
        .handle_collect(&json!({"id": "probe", "params": {"depth": 0}}))
        .expect_err("a params value below the declared minimum must be refused");
    assert!(
        err.contains("depth") || err.contains("minimum"),
        "the refusal must name the violated keyword: {err}"
    );
    assert_eq!(
        walks.load(Ordering::SeqCst),
        1,
        "the walk must not run for params the advertised schema refuses"
    );
}

// ---------------------------------------------------------------------------
// R1.4a (iii) — a call carrying a TOP-LEVEL property the contract does not
// declare is refused, NAMING the property.
// ---------------------------------------------------------------------------
#[test]
fn a_call_with_an_undeclared_top_level_property_is_refused_naming_it() {
    let (server, recorder) = recording_server();
    let err = server
        .handle_collect(&json!({"id": "probe", "summry": "typo"}))
        .expect_err("an undeclared top-level property must be refused");
    assert!(
        err.contains("summry"),
        "the refusal must NAME the property: {err}"
    );
    assert_eq!(recorder.walks.load(Ordering::SeqCst), 0);
}

// ---------------------------------------------------------------------------
// R1.8 — an EMPTY collect id is refused by the handler, before the walk.
//
// It is distinct from R1.4a(i): that arm is `id` ABSENT, this one is `id`
// present and empty, and the contract's schema can require presence but not
// non-emptiness.
// ---------------------------------------------------------------------------
#[test]
fn an_empty_collect_id_is_refused_before_the_walk() {
    let (server, recorder) = recording_server();
    let err = server
        .handle_collect(&json!({"id": ""}))
        .expect_err("an empty collect id must be refused");
    assert!(
        err.contains("the collect id is empty"),
        "the refusal must say what is wrong: {err}"
    );
    assert!(
        err.contains("collect"),
        "the refusal must name the collector: {err}"
    );
    assert_eq!(recorder.walks.load(Ordering::SeqCst), 0);
}

// ---------------------------------------------------------------------------
// R1.9 — a failed walk is a tool error with text, never an empty successful
// result; and a PANICKING walk is contained rather than killing the process.
// ---------------------------------------------------------------------------
struct Failing;
impl Collector for Failing {
    type Params = Params;
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
        _params: Params,
        _foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError> {
        Err("the source refused the connection".into())
    }
}

struct Panicking;
impl Collector for Panicking {
    type Params = Params;
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
        _params: Params,
        _foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError> {
        panic!("a collector author's unwrap on a missing directory")
    }
}

#[test]
fn a_failed_walk_is_a_tool_error_with_text() {
    let server = CollectorServer::new(Failing).expect("the failing collector serves");
    let err = server
        .handle_collect(&json!({"id": "probe"}))
        .expect_err("a failed walk must be refused, never an empty successful result");
    assert!(err.contains("the walk failed"), "{err}");
    assert!(err.contains("the source refused the connection"), "{err}");
    assert!(err.contains("collect"), "the refusal must name the collector: {err}");
}

#[test]
fn a_panicking_walk_is_contained_and_reported() {
    // The panic hook is silenced for this test only: the panic is EXPECTED and
    // its default message on stderr would read as a failure in the log.
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(|_| {}));
    let server = CollectorServer::new(Panicking).expect("the panicking collector serves");
    let outcome = server.handle_collect(&json!({"id": "probe"}));
    std::panic::set_hook(previous);

    let err = outcome.expect_err("a panicking walk must reach the caller as a refusal");
    assert!(
        err.contains("PANICKED"),
        "the refusal must say the walk panicked: {err}"
    );
    assert!(err.contains("collect"), "{err}");
}

// ---------------------------------------------------------------------------
// R1.13 — the CONTEXT property is decoded into the walk's parameter, not
// dropped; with a control that a call carrying no context still walks.
// ---------------------------------------------------------------------------
#[test]
fn the_context_property_is_decoded_and_not_dropped() {
    let (server, recorder) = recording_server();
    server
        .handle_collect(&json!({
            "id": "probe",
            "context": {
                "code": [{"graph_name": "knowledge", "nodes": [], "edges": []}],
                "acme-aws": []
            }
        }))
        .expect("a call carrying a context block walks");
    let families = recorder.seen_families.lock().expect("the lock").clone();
    assert_eq!(
        families,
        vec!["acme-aws".to_string(), "code".to_string()],
        "the walk must see the families the call carried, sorted"
    );

    // THE CONTROL: a call carrying NO context still walks, and the walk sees an
    // empty block rather than a refusal.
    let (server, recorder) = recording_server();
    server
        .handle_collect(&json!({"id": "probe"}))
        .expect("a call carrying no context walks");
    assert!(recorder
        .seen_families
        .lock()
        .expect("the lock")
        .is_empty());
}

// ---------------------------------------------------------------------------
// The params value reaches the walk, which is the other half of R1.13's claim
// that this crate's input type carries every property the advertised schema
// declares.
// ---------------------------------------------------------------------------
#[test]
fn the_params_value_reaches_the_walk() {
    let (server, recorder) = recording_server();
    server
        .handle_collect(&json!({"id": "probe", "params": {"root": "/tmp/x"}}))
        .expect("a call carrying params walks");
    assert_eq!(*recorder.seen_root.lock().expect("the lock"), "/tmp/x");
}

// ---------------------------------------------------------------------------
// R1.10 — serverInfo.name equals the TOOL NAME, asserted against this crate's
// own resolved name rather than against a client behaviour. Nothing in the
// client reads the field.
// ---------------------------------------------------------------------------
#[test]
fn the_resolved_tool_name_defaults_and_is_what_the_server_carries() {
    struct Unnamed;
    impl Collector for Unnamed {
        type Params = Params;
        fn tool(&self) -> ToolSpec {
            ToolSpec::new("", "a collector that names no tool")
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
            _params: Params,
            _foreign: &ForeignContext,
        ) -> Result<WalkResult, WalkError> {
            Ok(WalkResult::new(vec![], vec![], Completeness::complete()))
        }
    }

    let server = CollectorServer::new(Unnamed).expect("the unnamed collector serves");
    assert_eq!(server.tool_name(), DEFAULT_TOOL_NAME);
    assert_eq!(server.served_tool().name.as_ref(), DEFAULT_TOOL_NAME);

    struct Named;
    impl Collector for Named {
        type Params = Params;
        fn tool(&self) -> ToolSpec {
            ToolSpec::new("collect_logs", "")
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
            _params: Params,
            _foreign: &ForeignContext,
        ) -> Result<WalkResult, WalkError> {
            Ok(WalkResult::new(vec![], vec![], Completeness::complete()))
        }
    }
    let server = CollectorServer::new(Named).expect("the named collector serves");
    assert_eq!(server.tool_name(), "collect_logs");
}

// ---------------------------------------------------------------------------
// A successful collect returns the ENVELOPE, so the handler's happy path is
// pinned beside its refusals.
// ---------------------------------------------------------------------------
#[test]
fn a_successful_collect_returns_the_envelope() {
    let (server, _) = recording_server();
    let out: Value = server
        .handle_collect(&json!({"id": "probe"}))
        .expect("a well-formed call walks");
    assert_eq!(out["walk_complete"], json!(true));
    assert_eq!(out["nodes"][0]["id"], json!("n1"));
    assert_eq!(out["edges"], json!([]));
}
