// SPDX-License-Identifier: Apache-2.0

//! conformance.rs — the CONFORMANCE STUB the knowledge client's own dialing
//! tests drive, in every shape the collector contract has to be proven against.
//!
//! IT IS NOT THE SAMPLE COLLECTOR AND THE DIFFERENCE IS NOT A PREFERENCE. The
//! client's dialing tests assert this stub's EXACT payloads and strings — three
//! nodes with a named absent-versus-present-and-empty distinction, a literal
//! refusal string, a misspelled key, a 68 MiB result, two process deaths at
//! named points. The sample collector walks a directory; driven against these
//! tests it would fail on the first payload assertion. So the stub is a second
//! binary with its own dispatch, and `tests/binaries.rs` asserts the two exist
//! and differ.
//!
//! IT NEEDS NO RAW-HANDLER ESCAPE, WHICH THE GO STUB DOES. The Go stub uses the
//! SDK's raw handler form and says why: the generic form would validate the
//! result against the advertised output schema server-side, which would make the
//! "provider breaks its own word" arm untestable. rmcp validates neither the
//! arguments nor the result, so `bad-output-schema`, `breaks-own-word` and
//! `bad-input-schema` are ordinary handlers here. The same absence is why the
//! LIBRARY owes both checks itself; see `serve.rs`.
//!
//! THE PAYLOAD BUILDERS ARE IN THE LIBRARY RATHER THAN IN THE BINARY, which is
//! the one place this port is simpler than what it mirrors. The Go stub
//! duplicates its 68 MiB builder deliberately, because that half runs in a
//! re-execed CHILD process that shares no `testing.T` with the parent and cannot
//! call a helper that takes one. Rust's integration tests link the library, so
//! there is one definition and the crate's own suite drives every mode directly.
//!
//! STDOUT IS THE PROTOCOL STREAM. Every diagnostic below goes to stderr.

use std::sync::Arc;

use rmcp::ErrorData;
use rmcp::model::{
    CallToolRequestParams, CallToolResponse, CallToolResult, ContentBlock, ErrorCode,
    Implementation, ListToolsResult, PaginatedRequestParams, ServerCapabilities, ServerInfo, Tool,
};
use rmcp::service::{RequestContext, RoleServer};
use rmcp::{ServerHandler, ServiceExt};
use serde_json::{json, Map, Value};

use crate::framework::DESCRIBE_TOOL_NAME;
use crate::schema::{DESCRIBE_CONTRACT_JSON, INPUT_CONTRACT_JSON, OUTPUT_CONTRACT_JSON};
use crate::serve::{IMPLEMENTATION_VERSION, PROTOCOL_VERSION};

/// STUB_MODE_ENV switches the stub binary to one of its modes. It carries the
/// same name the client's own test harness uses, because the harness sets it in
/// the registration entry's env block.
pub const STUB_MODE_ENV: &str = "FUL1776_STUB_MODE";

/// STUB_TOOL_ENV names the tool the stub advertises; empty means the default.
pub const STUB_TOOL_ENV: &str = "FUL1776_STUB_TOOL";

/// DEFAULT_STUB_TOOL is the tool name every mode but `no-such-tool` serves.
pub const DEFAULT_STUB_TOOL: &str = "collect_graph";

/// STUB_STDERR_MARKER is what this stub writes to stderr on every dial.
///
/// IT EXISTS BECAUSE THE SEAM'S ENGAGEMENT IS NOT OBSERVABLE FROM THE TEST
/// COUNTS. A run of the client's eleven dialing tests against this stub and a
/// run against the client's own in-tree Go stub produce identical PASS, FAIL and
/// SKIP counts — measured, not assumed. A leg that reports those counts has not
/// shown which provider answered. This marker is what distinguishes them: it
/// appears once per dial in a seamed run and never in an unseamed one.
pub const STUB_STDERR_MARKER: &str = "knowledge-collector-framework-rust-conformance-stub";

/// Mode is one shape the collector contract must be proven against.
///
/// THE LIST IS DECLARED ONCE, HERE. The stub's dispatch, the crate's per-mode
/// test rows and anything that reports which modes this port emulates all read
/// [`STUB_MODES`]; a hand-typed copy in a second place is the defect that makes
/// a port claim a mode it does not serve.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    /// Advertises contract-satisfying schemas and returns a small conforming
    /// graph.
    Conforming,
    /// The environment probe: answers PER-NAME lookups and never serializes its
    /// whole environment.
    EnvReport,
    /// Advertises no output schema at all.
    NoOutputSchema,
    /// Advertises an output schema missing the completeness assertion.
    BadOutputSchema,
    /// Advertises an input schema that does not require the collect id.
    BadInputSchema,
    /// Advertises a stricter output schema than the contract and then returns a
    /// result violating it.
    BreaksOwnWord,
    /// Returns an error result.
    ToolError,
    /// Returns a node with an empty type.
    EmptyNodeType,
    /// Returns a node carrying a field the envelope does not define.
    UnknownField,
    /// Returns an edge whose endpoint is not in the result.
    DanglingEdge,
    /// Returns a conforming result asserting `walk_complete` false.
    IncompleteWalk,
    /// Returns zero nodes and zero edges.
    EmptyGraph,
    /// Returns a conforming result LARGER than the 64 MiB bound the client used
    /// to enforce.
    OverFormerCap,
    /// Exits non-zero without speaking MCP.
    ExitBeforeHandshake,
    /// Boots, answers the handshake, is listed and schema-verified, and only
    /// then dies — inside the tool call.
    ExitMidSession,
    /// Serves a provider listing a DIFFERENT tool.
    NoSuchTool,
    /// Serves the collect tool ALONE: the shape every provider written before
    /// the declaration tool existed has.
    NoDescribe,
    /// Serves a describe tool whose advertised output schema omits the node
    /// vocabulary — the half the ingest refusal reads, so a provider bending it
    /// declares nothing the server can refuse against.
    BadDescribeSchema,
    /// Serves a conforming describe SCHEMA and then returns a declaration that
    /// violates it.
    BadDeclaration,
    /// Serves a describe tool that reports an error result.
    DescribeError,
}

/// STUB_MODES is the whole mode vocabulary, in the order the client's own stub
/// declares it.
pub const STUB_MODES: [Mode; 20] = [
    Mode::Conforming,
    Mode::EnvReport,
    Mode::NoOutputSchema,
    Mode::BadOutputSchema,
    Mode::BadInputSchema,
    Mode::BreaksOwnWord,
    Mode::ToolError,
    Mode::EmptyNodeType,
    Mode::UnknownField,
    Mode::DanglingEdge,
    Mode::IncompleteWalk,
    Mode::EmptyGraph,
    Mode::OverFormerCap,
    Mode::ExitBeforeHandshake,
    Mode::ExitMidSession,
    Mode::NoSuchTool,
    Mode::NoDescribe,
    Mode::BadDescribeSchema,
    Mode::BadDeclaration,
    Mode::DescribeError,
];

impl Mode {
    /// as_str is the wire spelling, which is what the env block carries.
    pub fn as_str(&self) -> &'static str {
        match self {
            Mode::Conforming => "conforming",
            Mode::EnvReport => "env-report",
            Mode::NoOutputSchema => "no-output-schema",
            Mode::BadOutputSchema => "bad-output-schema",
            Mode::BadInputSchema => "bad-input-schema",
            Mode::BreaksOwnWord => "breaks-own-word",
            Mode::ToolError => "tool-error",
            Mode::EmptyNodeType => "empty-node-type",
            Mode::UnknownField => "unknown-field",
            Mode::DanglingEdge => "dangling-edge",
            Mode::IncompleteWalk => "incomplete-walk",
            Mode::EmptyGraph => "empty-graph",
            Mode::OverFormerCap => "over-former-cap",
            Mode::ExitBeforeHandshake => "exit-before-handshake",
            Mode::ExitMidSession => "exit-mid-session",
            Mode::NoSuchTool => "no-such-tool",
            Mode::NoDescribe => "no-describe",
            Mode::BadDescribeSchema => "bad-describe-schema",
            Mode::BadDeclaration => "bad-declaration",
            Mode::DescribeError => "describe-error",
        }
    }

    /// serves_describe reports whether this mode publishes the declaration tool
    /// at all. Exactly one mode does not.
    pub fn serves_describe(&self) -> bool {
        *self != Mode::NoDescribe
    }

    /// describe_schema is the describe output schema this mode advertises.
    pub fn describe_schema(&self) -> Option<Map<String, Value>> {
        if !self.serves_describe() {
            return None;
        }
        if *self == Mode::BadDescribeSchema {
            // Conforming but for the node vocabulary, which is the half the
            // ingest refusal reads.
            let mut out = contract_schema_map(DESCRIBE_CONTRACT_JSON);
            out.insert(
                "required".into(),
                json!(["behavior", "edge_types", "environment"]),
            );
            if let Some(props) = out.get_mut("properties").and_then(Value::as_object_mut) {
                props.remove("node_types");
            }
            return Some(out);
        }
        Some(contract_schema_map(DESCRIBE_CONTRACT_JSON))
    }

    /// declaration is the document this mode's describe tool answers with.
    /// `None` is the reports-an-error arm.
    pub fn declaration(&self) -> Option<Value> {
        match self {
            Mode::NoDescribe | Mode::DescribeError => None,
            Mode::BadDeclaration => {
                // A conforming schema, then a document missing a required key.
                let Value::Object(mut doc) = conforming_declaration() else {
                    unreachable!("the declaration is an object")
                };
                doc.remove("behavior");
                Some(Value::Object(doc))
            }
            _ => Some(conforming_declaration()),
        }
    }

    /// parse resolves a mode name. An unknown name is REFUSED naming it and the
    /// vocabulary — a stub that fell back to a default would report a green for
    /// a mode it never served.
    pub fn parse(name: &str) -> Result<Mode, String> {
        STUB_MODES
            .iter()
            .copied()
            .find(|m| m.as_str() == name)
            .ok_or_else(|| {
                format!(
                    "conformance stub: {name:?} is not a mode this stub emulates; the modes are {}",
                    STUB_MODES
                        .iter()
                        .map(Mode::as_str)
                        .collect::<Vec<_>>()
                        .join(", ")
                )
            })
    }

    /// tool_name is the tool this mode advertises.
    pub fn tool_name(&self, configured: &str) -> String {
        if *self == Mode::NoSuchTool {
            return "some_other_tool".to_string();
        }
        if configured.is_empty() {
            DEFAULT_STUB_TOOL.to_string()
        } else {
            configured.to_string()
        }
    }

    /// input_schema is the input schema this mode advertises.
    pub fn input_schema(&self) -> Map<String, Value> {
        if *self == Mode::BadInputSchema {
            // Type object, but it does not require the collect id — the one
            // thing the contract's input side insists on.
            let Value::Object(m) = json!({
                "type": "object",
                "properties": {"params": {"type": "object"}}
            }) else {
                unreachable!("the literal above is an object")
            };
            return m;
        }
        contract_schema_map(INPUT_CONTRACT_JSON)
    }

    /// output_schema is the output schema this mode advertises. `None` is the
    /// advertises-nothing arm.
    pub fn output_schema(&self) -> Option<Map<String, Value>> {
        match self {
            Mode::NoOutputSchema => None,
            Mode::BadOutputSchema => {
                // Conforming but for the completeness assertion, which is exactly
                // the omission the contract exists to refuse.
                let mut out = contract_schema_map(OUTPUT_CONTRACT_JSON);
                out.insert("required".into(), json!(["nodes", "edges"]));
                if let Some(props) = out.get_mut("properties").and_then(Value::as_object_mut) {
                    props.remove("walk_complete");
                }
                Some(out)
            }
            Mode::BreaksOwnWord => {
                // STRICTER than the contract: every node must also carry a
                // summary. The handler then returns one that does not.
                let mut out = contract_schema_map(OUTPUT_CONTRACT_JSON);
                if let Some(items) = out
                    .get_mut("properties")
                    .and_then(Value::as_object_mut)
                    .and_then(|p| p.get_mut("nodes"))
                    .and_then(Value::as_object_mut)
                    .and_then(|n| n.get_mut("items"))
                    .and_then(Value::as_object_mut)
                {
                    items.insert("required".into(), json!(["id", "type", "summary"]));
                }
                Some(out)
            }
            _ => Some(contract_schema_map(OUTPUT_CONTRACT_JSON)),
        }
    }

    /// payload builds the structured content for one mode, given the call
    /// arguments.
    pub fn payload(&self, arguments: &Value) -> Value {
        match self {
            Mode::EnvReport => env_report_payload(arguments),
            Mode::EmptyNodeType => {
                json!({"nodes": [{"id": "n1", "type": ""}], "edges": [], "walk_complete": true})
            }
            Mode::UnknownField => json!({
                "nodes": [{"id": "n1", "type": "issue", "summry": "typo'd key"}],
                "edges": [],
                "walk_complete": true
            }),
            Mode::DanglingEdge => json!({
                "nodes": [{"id": "n1", "type": "issue"}],
                "edges": [{"from_id": "n1", "to_id": "absent", "type": "blocks"}],
                "walk_complete": true
            }),
            Mode::IncompleteWalk => json!({
                "nodes": [{"id": "n1", "type": "issue"}],
                "edges": [],
                "walk_complete": false
            }),
            Mode::EmptyGraph => json!({"nodes": [], "edges": [], "walk_complete": true}),
            Mode::OverFormerCap => over_former_cap_payload(),
            Mode::BreaksOwnWord => {
                // No summary, which the schema this stub advertised requires.
                json!({
                    "nodes": [{"id": "n1", "type": "issue"}],
                    "edges": [],
                    "walk_complete": true
                })
            }
            _ => conforming_payload(),
        }
    }
}

/// contract_schema_map decodes a checked-in contract schema into a mutable map
/// so a mode can advertise it verbatim or bend one keyword of it.
pub fn contract_schema_map(raw: &[u8]) -> Map<String, Value> {
    match serde_json::from_slice::<Value>(raw) {
        Ok(Value::Object(m)) => m,
        Ok(_) => panic!("conformance stub: a checked-in contract schema is not an object"),
        Err(e) => panic!("conformance stub: a checked-in contract schema does not decode: {e}"),
    }
}

/// conforming_payload is the reference result: three nodes, one edge, a complete
/// walk, every optional field exercised on one node, absent on the second and
/// PRESENT AND EMPTY on the third.
///
/// THE THIRD NODE IS NOT A DUPLICATE OF THE SECOND. `ISSUE-2` OMITS its optional
/// fields; `ISSUE-3` carries them present and empty. Absent and empty-valued are
/// distinct inputs both to the JSON Schema validation and to the strict decode
/// the client runs over this payload, so the two cells are two cells.
pub fn conforming_payload() -> Value {
    json!({
        "nodes": [
            {
                "id": "ISSUE-1", "type": "issue",
                "symbol_name": "Login broken", "file_path": "src/login.go", "language": "go",
                "start_line": 10, "end_line": 20, "content": "body", "signature": "sig",
                "summary": "one line", "description": "longer", "source": "stub", "status": "open",
                "keywords": "login auth", "is_exported": true,
                "metadata": {"priority": "high"}
            },
            {"id": "ISSUE-2", "type": "issue"},
            {
                "id": "ISSUE-3", "type": "issue",
                "symbol_name": "", "file_path": "", "language": "", "content": "",
                "signature": "", "summary": "", "description": "", "source": "",
                "status": "", "keywords": "", "start_line": 0, "end_line": 0,
                "is_exported": false,
                "metadata": {}
            }
        ],
        "edges": [{"from_id": "ISSUE-1", "to_id": "ISSUE-2", "type": "blocks"}],
        "walk_complete": true
    })
}

/// DESCRIBE_ERROR_TEXT is the literal a refused describe carries.
pub const DESCRIBE_ERROR_TEXT: &str = "the collector cannot describe itself right now";

/// conforming_declaration is the reference declaration every mode but the two
/// describe-failure arms returns.
pub fn conforming_declaration() -> Value {
    json!({
        "behavior": {
            "summarizable": true, "embeddable": false, "syncable": true,
            "embed_fields": ["summary"]
        },
        "node_types": ["issue", "epic"],
        "edge_types": ["blocks"],
        "environment": [{"name": DECLARED_ENV, "class": "selector"}]
    })
}

/// DECLARED_ENV is the one variable the reference declaration names. It is the
/// same name the client's environment-probe arms plant, so a declaration and an
/// env report are talking about one variable.
pub const DECLARED_ENV: &str = "FUL1776_DECLARED";

/// TOOL_ERROR_TEXT is the literal a refused collect carries. The client's own
/// test asserts this string.
pub const TOOL_ERROR_TEXT: &str = "the stub provider refused this collect";

/// env_report_payload answers the environment probe. It reports ONLY the names
/// the caller asked about, one node each, carrying whether the variable is
/// present in this child's environment and — for a present one — its value.
///
/// IT NEVER SERIALIZES THE WHOLE ENVIRONMENT. Reading every variable and
/// admitting it into a result would write whatever the process held into a
/// graph, which is the shape the collector contract exists to close.
pub fn env_report_payload(arguments: &Value) -> Value {
    let names = asked_names(arguments);
    let nodes: Vec<Value> = names
        .into_iter()
        .map(|name| {
            let mut metadata = Map::new();
            match std::env::var(&name) {
                Ok(value) => {
                    metadata.insert("present".into(), Value::String("true".into()));
                    metadata.insert("value".into(), Value::String(value));
                }
                Err(_) => {
                    metadata.insert("present".into(), Value::String("false".into()));
                }
            }
            json!({"id": name, "type": "env_var", "metadata": Value::Object(metadata)})
        })
        .collect();
    json!({"nodes": nodes, "edges": [], "walk_complete": true})
}

/// asked_names pulls the requested variable names out of the call arguments.
fn asked_names(arguments: &Value) -> Vec<String> {
    arguments
        .get("params")
        .and_then(|p| p.get("names"))
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(Value::as_str)
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default()
}

/// OVER_FORMER_CAP_BODY_LEN is one node body's length in the over-cap payload.
pub const OVER_FORMER_CAP_BODY_LEN: usize = 1 << 20;
/// OVER_FORMER_CAP_NODES is how many nodes the over-cap payload carries. Sixty
/// eight MiB of bodies, over the 64 MiB bound the client retired.
pub const OVER_FORMER_CAP_NODES: usize = 68;

/// over_former_cap_payload builds the conforming result the over-cap mode
/// serves.
///
/// THE FORMER BOUND CANNOT BE REACHED ANY OTHER WAY. The retired over-cap tests
/// reached their boundary by lowering the cap, and there is no cap left to
/// lower: crossing the former bound with real bytes is the only way to prove it
/// no longer applies.
pub fn over_former_cap_payload() -> Value {
    let body = "x".repeat(OVER_FORMER_CAP_BODY_LEN);
    let mut nodes = Vec::with_capacity(OVER_FORMER_CAP_NODES);
    let mut edges = Vec::with_capacity(OVER_FORMER_CAP_NODES - 1);
    for i in 0..OVER_FORMER_CAP_NODES {
        nodes.push(json!({"id": format!("big-{i}"), "type": "blob", "content": body}));
        if i > 0 {
            edges.push(json!({
                "from_id": format!("big-{}", i - 1),
                "to_id": format!("big-{i}"),
                "type": "NEXT"
            }));
        }
    }
    json!({"nodes": nodes, "edges": edges, "walk_complete": true})
}

/// StubServer is the MCP server one stub mode serves.
pub struct StubServer {
    mode: Mode,
    tool: Tool,
    describe_tool: Option<Tool>,
    tool_name: String,
}

impl StubServer {
    /// new builds the server for one mode and the configured tool name.
    pub fn new(mode: Mode, configured_tool: &str) -> Self {
        let tool_name = mode.tool_name(configured_tool);
        let mut tool = Tool::new(
            tool_name.clone(),
            "stub custom collector",
            Arc::new(mode.input_schema()),
        );
        tool.output_schema = mode.output_schema().map(Arc::new);

        let describe_tool = mode.describe_schema().map(|schema| {
            let mut m = Map::new();
            m.insert("type".into(), Value::String("object".into()));
            let mut t = Tool::new(DESCRIBE_TOOL_NAME, "stub declaration", Arc::new(m));
            t.output_schema = Some(Arc::new(schema));
            t
        });

        StubServer {
            mode,
            tool,
            describe_tool,
            tool_name,
        }
    }

    /// served_describe_tool is the declaration tool this mode publishes, if any.
    pub fn served_describe_tool(&self) -> Option<&Tool> {
        self.describe_tool.as_ref()
    }

    /// served_tool is the tool a `tools/list` returns.
    pub fn served_tool(&self) -> &Tool {
        &self.tool
    }
}

impl ServerHandler for StubServer {
    fn get_info(&self) -> ServerInfo {
        let mut info = ServerInfo::default();
        info.protocol_version = PROTOCOL_VERSION;
        info.capabilities = ServerCapabilities::builder().enable_tools().build();
        info.server_info = Implementation::new("knowledge-rust-stub", IMPLEMENTATION_VERSION);
        info
    }

    async fn list_tools(
        &self,
        _request: Option<PaginatedRequestParams>,
        _context: RequestContext<RoleServer>,
    ) -> Result<ListToolsResult, ErrorData> {
        let mut tools = vec![self.tool.clone()];
        if let Some(describe) = &self.describe_tool {
            tools.push(describe.clone());
        }
        Ok(ListToolsResult::with_all_items(tools))
    }

    async fn call_tool(
        &self,
        request: CallToolRequestParams,
        _context: RequestContext<RoleServer>,
    ) -> Result<CallToolResponse, ErrorData> {
        if request.name == DESCRIBE_TOOL_NAME && self.describe_tool.is_some() {
            let Some(declaration) = self.mode.declaration() else {
                return Ok(CallToolResponse::Complete(CallToolResult::error(vec![
                    ContentBlock::text(DESCRIBE_ERROR_TEXT),
                ])));
            };
            return Ok(CallToolResponse::Complete(CallToolResult::structured(
                declaration,
            )));
        }
        if request.name != self.tool_name {
            return Err(ErrorData {
                code: ErrorCode::METHOD_NOT_FOUND,
                message: format!("this stub serves {:?} and no other", self.tool_name).into(),
                data: None,
            });
        }
        if self.mode == Mode::ExitMidSession {
            // Dies WITH THE CALL IN FLIGHT. A process exit rather than an error
            // return: the arm under test is a provider that stops existing, not
            // one that reports a failure, and those reach different client code.
            eprintln!("{STUB_STDERR_MARKER}: exiting mid-session, deliberately");
            std::process::exit(7);
        }
        if self.mode == Mode::ToolError {
            return Ok(CallToolResponse::Complete(CallToolResult::error(vec![
                ContentBlock::text(TOOL_ERROR_TEXT),
            ])));
        }
        let arguments = Value::Object(request.arguments.unwrap_or_else(Map::new));
        Ok(CallToolResponse::Complete(CallToolResult::structured(
            self.mode.payload(&arguments),
        )))
    }
}

/// MARKER_FILE_FLAG names a file this stub APPENDS one dial line to, given as an
/// argv flag rather than an environment variable.
///
/// ARGV IS THE ONE CHANNEL THAT SURVIVES, and that is the whole reason for the
/// flag. A collector's environment is the registration entry's block and nothing
/// else — that is the property four of the eleven dialing tests exist to prove —
/// so a variable the CI leg exports does not reach this child. The seam takes a
/// COMMAND WITH ARGUMENTS, and arguments are delivered verbatim.
///
/// WHY A FILE AND NOT THE STDERR LINE. The stderr line below is written on every
/// dial and the crate's own suite reads it, but the contract runner does not
/// forward a child's stderr into its own report, so a CI leg reading that report
/// cannot see it. A file the leg names and then counts is the same observation
/// through a channel the leg controls.
pub const MARKER_FILE_FLAG: &str = "--marker-file";

/// run is the stub binary's whole body: announce the dial, then serve the named
/// mode.
///
/// `marker_file`, when given, receives one appended line per dial. A failure to
/// write it is FATAL rather than ignored: the file is an assertion's subject, and
/// a silently unwritten marker would read as a seam that never engaged.
pub async fn run(mode: Mode, configured_tool: &str, marker_file: Option<&str>) -> std::io::Result<()> {
    let exe = std::env::current_exe()
        .map(|p| p.display().to_string())
        .unwrap_or_else(|_| "<unresolved>".to_string());
    // THE DIAL MARKER. See STUB_STDERR_MARKER: the seamed and unseamed runs are
    // numerically identical, so this line is what shows which provider answered.
    eprintln!("{STUB_STDERR_MARKER}: dialed mode={} exe={exe}", mode.as_str());
    if let Some(path) = marker_file {
        use std::io::Write;
        let mut file = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)?;
        writeln!(file, "{STUB_STDERR_MARKER} mode={} exe={exe}", mode.as_str())?;
    }

    if mode == Mode::ExitBeforeHandshake {
        eprintln!("{STUB_STDERR_MARKER}: exiting before the handshake, deliberately");
        std::process::exit(3);
    }

    let server = StubServer::new(mode, configured_tool);
    match server.serve(rmcp::transport::stdio()).await {
        Ok(running) => {
            let _ = running.waiting().await;
            Ok(())
        }
        Err(e) => {
            eprintln!("{STUB_STDERR_MARKER}: {e}");
            std::process::exit(1);
        }
    }
}
