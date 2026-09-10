// SPDX-License-Identifier: Apache-2.0

//! serve.rs — the ENTRY POINT: one collector, served as one MCP tool over
//! stdio.
//!
//! STDOUT IS THE PROTOCOL STREAM. A collector that prints to stdout corrupts the
//! JSON-RPC framing and surfaces to an operator as an opaque handshake failure;
//! every diagnostic a collector writes goes to stderr. `println!`, `print!` and
//! `dbg!` are the Rust spellings of that defect and a corpus check scans this
//! crate for them.
//!
//! THIS CRATE SERVES STDIO ONLY, DELIBERATELY. The Go framework also serves
//! streamable HTTP, for collectors installed as a `type: http` entry that run on
//! another host. A stdio collector is the shape a daemon SPAWNS, which is what a
//! collector author building from source and registering a local binary needs,
//! and it is the transport this crate's ticket specifies. An HTTP arm is a
//! second transport with its own authentication posture and its own request-body
//! bound, and adding one without a caller would be untested surface.
//!
//! THE PROTOCOL REVISION IS PINNED, AND THE PIN IS THE ONLY THING THAT DECIDES
//! THE ANSWER. rmcp echoes the revision the SERVER declares, not the one the
//! client asked for: a client requesting `2026-07-28` against a server whose
//! `get_info` names `2025-06-18` is answered `2025-06-18`. The knowledge client
//! requests the newest revision its own SDK knows and accepts five revisions
//! down, so `2025-06-18` is a FEATURE FLOOR rather than a negotiation, and this
//! crate names it because that is the revision the collector contract is written
//! against.
//!
//! THE SUPPORTED-VERSION LIST IS LEFT AT rmcp's DEFAULT and that is deliberate.
//! Narrowing it to the pinned revision alone would make rmcp refuse every
//! post-handshake request whose own `_meta` names a newer revision, which is
//! exactly what a current client sends. The pin belongs on the ANSWER, not on
//! the acceptance set.
//!
//! WHAT THIS FILE OWNS THAT THE GO FRAMEWORK DELEGATES. rmcp validates NEITHER
//! the call's arguments against the advertised input schema NOR the handler's
//! result against the advertised output schema — measured on both halves with a
//! same-run control. The Go SDK's generic `AddTool` does both, so Go's handler
//! can assume a validated input. Here [`CollectorServer::call_tool`] runs the
//! input check itself, and the typed envelope in `envelope.rs` is what keeps the
//! output conforming. Neither is optional and neither is decoration.

use std::panic::AssertUnwindSafe;
use std::sync::Arc;

use rmcp::ErrorData;
use rmcp::model::{
    CallToolRequestParams, CallToolResponse, CallToolResult, ContentBlock, ErrorCode,
    Implementation, ListToolsResult, PaginatedRequestParams, ProtocolVersion, ServerCapabilities,
    ServerInfo, Tool,
};
use rmcp::service::{RequestContext, RoleServer};
use rmcp::{ServerHandler, ServiceExt};
use serde::Deserialize;
use serde_json::{Map, Value};

use crate::context::ForeignContext;
use crate::envelope::encode_result;
use crate::error::{Error, Result};
use crate::framework::{Collector, DESCRIBE_TOOL_NAME};
use crate::schema::{advertised_describe_schema, advertised_input_schema, advertised_output_schema};

/// PROTOCOL_VERSION is the MCP revision this crate answers the handshake with.
/// See the module doc for why it is pinned and why the supported set is not.
pub const PROTOCOL_VERSION: ProtocolVersion = ProtocolVersion::V_2025_06_18;

/// IMPLEMENTATION_VERSION identifies this crate in the MCP handshake. A consumer
/// logs it, so it names the serving layer rather than the crate path.
pub const IMPLEMENTATION_VERSION: &str = "v1";

/// CollectorServer is the MCP server one collector serves: one tool, carrying the
/// contract's advertised schemas with this collector's params spliced in, bound
/// to a handler that validates, walks and encodes.
///
/// It is public because a collector with its own transport story (a test, an
/// embedding host) needs the value; a collector's `main` calls [`serve_stdio`]
/// and never sees it.
pub struct CollectorServer<C: Collector> {
    collector: C,
    tool_name: String,
    tool: Tool,
    describe_tool: Tool,
    declaration: Value,
    advertised_input: Value,
    advertised_output: Value,
    advertised_describe: Value,
}

impl<C: Collector> CollectorServer<C> {
    /// new builds the server for one collector, resolving its tool name and
    /// advertised schemas.
    pub fn new(collector: C) -> Result<Self> {
        let spec = collector.tool();
        let tool_name = spec.resolved_name().to_string();
        let input = advertised_input_schema::<C::Params>()?;
        let output = advertised_output_schema()?;
        let mut tool = Tool::new(
            tool_name.clone(),
            spec.description.clone(),
            Arc::new(input.clone()),
        );
        tool.output_schema = Some(Arc::new(output.clone()));

        // THE DECLARATION IS RENDERED AND VALIDATED AT CONSTRUCTION, not at the
        // first describe call. A malformed declaration is a defect in the
        // collector, and a collector that cannot describe itself should fail to
        // start rather than fail the registration dial of an operator who did
        // nothing wrong.
        let declaration = collector.declaration().render()?;
        let describe = advertised_describe_schema()?;
        let mut describe_tool = Tool::new(
            DESCRIBE_TOOL_NAME,
            "declare this collector's vocabulary, environment and behavior",
            Arc::new(empty_object_schema()),
        );
        describe_tool.output_schema = Some(Arc::new(describe.clone()));

        Ok(CollectorServer {
            collector,
            tool_name,
            tool,
            describe_tool,
            declaration,
            advertised_input: Value::Object(input),
            advertised_output: Value::Object(output),
            advertised_describe: Value::Object(describe),
        })
    }

    /// declaration is the rendered document the describe tool serves.
    pub fn declaration(&self) -> &Value {
        &self.declaration
    }

    /// advertised_describe_schema is the document this server publishes for the
    /// describe tool's output.
    pub fn advertised_describe_schema(&self) -> &Value {
        &self.advertised_describe
    }

    /// tool_name is the resolved name of the served tool.
    pub fn tool_name(&self) -> &str {
        &self.tool_name
    }

    /// advertised_input_schema is the document this server publishes for the
    /// tool's input, as an MCP tool listing carries it.
    pub fn advertised_input_schema(&self) -> &Value {
        &self.advertised_input
    }

    /// advertised_output_schema is the document this server publishes for the
    /// tool's output.
    pub fn advertised_output_schema(&self) -> &Value {
        &self.advertised_output
    }

    /// served_tool is the tool definition a `tools/list` returns.
    pub fn served_tool(&self) -> &Tool {
        &self.tool
    }

    /// served_describe_tool is the declaration tool a `tools/list` returns
    /// beside the collect tool.
    pub fn served_describe_tool(&self) -> &Tool {
        &self.describe_tool
    }

    /// served_tools is every tool this server publishes.
    ///
    /// IT IS A SLICE RATHER THAN THE ONE TOOL, and the reason is a defect the Go
    /// framework already paid for: its own suite carried two one-tool assertions
    /// that had to be rewritten the day a second tool arrived. A caller that
    /// looks a tool up BY NAME and asserts no cardinality survives that day,
    /// which is also how the client's own tool lookup is written.
    pub fn served_tools(&self) -> Vec<Tool> {
        vec![self.tool.clone(), self.describe_tool.clone()]
    }

    /// validate_call_arguments refuses a call whose arguments do not satisfy the
    /// schema THIS SERVER ADVERTISED, before anything is decoded.
    ///
    /// IT IS NOT REDUNDANT WITH THE STRICT DECODE BELOW IT, and the two catch
    /// different things. The decode enforces the SHAPE of the collector's own
    /// params type: a missing `id`, a `params` that is a string where the type is
    /// a struct, a top-level key the contract does not declare. This check
    /// enforces the SCHEMA, including every keyword a params type can carry that
    /// a Rust type cannot express by itself — a numeric bound, a string pattern,
    /// an enumeration. Remove it and a collector declaring `minimum: 1` accepts
    /// zero and finds out inside the walk.
    pub fn validate_call_arguments(&self, arguments: &Value) -> Result<()> {
        let validator = jsonschema::validator_for(&self.advertised_input).map_err(|e| {
            Error::new(format!(
                "{}: the advertised input schema does not resolve: {e}",
                self.tool_name
            ))
        })?;
        let failures: Vec<String> = validator
            .iter_errors(arguments)
            .map(|e| {
                let at = e.instance_path().to_string();
                let at = if at.is_empty() {
                    "the arguments".to_string()
                } else {
                    at
                };
                format!("at {at}: {e}")
            })
            .collect();
        if failures.is_empty() {
            return Ok(());
        }
        Err(Error::new(format!(
            "{} collector: the call arguments do not satisfy the schema this collector advertised: {}",
            self.tool_name,
            failures.join("; ")
        )))
    }

    /// handle_collect is the whole tool call, as a function over plain values so
    /// the crate's own suite can drive it without a transport.
    ///
    /// It returns the refusal as a `String` rather than as an error type because
    /// every arm below reaches the caller the same way: an `isError` tool result
    /// carrying text. A refused collect is the CALLER's problem to read, never a
    /// JSON-RPC protocol error, which an MCP client renders opaquely.
    pub fn handle_collect(&self, arguments: &Value) -> std::result::Result<Value, String> {
        self.validate_call_arguments(arguments)
            .map_err(|e| e.to_string())?;

        let input: CollectInput<C::Params> =
            serde_json::from_value(arguments.clone()).map_err(|e| {
                format!(
                    "{} collector: the call arguments do not decode into this collector's input: {e}",
                    self.tool_name
                )
            })?;

        // The schema requires the id to be PRESENT and a string; it cannot
        // require it to be non-empty. An empty collect id names no graph
        // instance, so it is refused here rather than walked and discarded
        // downstream.
        if input.id.is_empty() {
            return Err(format!(
                "{} collector: the collect id is empty; it names the graph instance this result lands in",
                self.tool_name
            ));
        }

        // A PANICKING WALK IS A TOOL ERROR, NOT A DEAD PROCESS. A collector
        // author's `unwrap` on a missing directory would otherwise take the
        // whole provider down mid-call, which reaches the client as a transport
        // failure rather than as a message naming the collector.
        let walked = std::panic::catch_unwind(AssertUnwindSafe(|| {
            self.collector.walk(&input.id, input.params, &input.context)
        }));
        let result = match walked {
            Err(_) => {
                return Err(format!(
                    "{} collector: the walk PANICKED; the panic message is on this collector's stderr",
                    self.tool_name
                ));
            }
            Ok(Err(e)) => {
                return Err(format!("{} collector: the walk failed: {e}", self.tool_name));
            }
            Ok(Ok(result)) => result,
        };

        let out = encode_result(&self.tool_name, result).map_err(|e| e.to_string())?;
        serde_json::to_value(out).map_err(|e| {
            format!(
                "{} collector: the encoded result does not serialize: {e}",
                self.tool_name
            )
        })
    }
}

/// empty_object_schema is the describe tool's advertised INPUT: the tool takes
/// no arguments, and an MCP server may not publish a tool with no input schema
/// at all.
fn empty_object_schema() -> Map<String, Value> {
    let mut m = Map::new();
    m.insert("type".into(), Value::String("object".into()));
    m
}

/// CollectInput is the served tool's input: the collect id, the collector's own
/// params, and the declared foreign-graph context.
///
/// `deny_unknown_fields` IS THE POINT OF THIS TYPE. The advertised schema carries
/// the `context` property whether this struct names it or not, because that
/// schema is the contract file with only `params` spliced in — so a struct that
/// forgot the field would be sent the block, decode nothing from it, and walk as
/// if the operator had declared none. That exact defect shipped in the Go
/// framework before its `Context` field existed. The attribute turns the same
/// mistake into a refusal naming the property.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct CollectInput<P> {
    id: String,
    #[serde(default)]
    params: P,
    #[serde(default)]
    context: ForeignContext,
}

impl<C: Collector> ServerHandler for CollectorServer<C> {
    fn get_info(&self) -> ServerInfo {
        let mut info = ServerInfo::default();
        info.protocol_version = PROTOCOL_VERSION;
        info.capabilities = ServerCapabilities::builder().enable_tools().build();
        // serverInfo.name IS THE TOOL NAME. Nothing in the client reads it; it is
        // what an operator sees in a handshake log, and naming the tool is what
        // makes that line locate the config entry.
        info.server_info = Implementation::new(self.tool_name.clone(), IMPLEMENTATION_VERSION);
        info.instructions = None;
        info
    }

    async fn list_tools(
        &self,
        _request: Option<PaginatedRequestParams>,
        _context: RequestContext<RoleServer>,
    ) -> std::result::Result<ListToolsResult, ErrorData> {
        Ok(ListToolsResult::with_all_items(self.served_tools()))
    }

    async fn call_tool(
        &self,
        request: CallToolRequestParams,
        _context: RequestContext<RoleServer>,
    ) -> std::result::Result<CallToolResponse, ErrorData> {
        if request.name == DESCRIBE_TOOL_NAME {
            // THE DECLARATION IS ALREADY RENDERED AND VALIDATED. Serving it is a
            // clone rather than a build, so a describe call cannot fail for a
            // reason a collect call would not have failed for at startup.
            return Ok(CallToolResponse::Complete(CallToolResult::structured(
                self.declaration.clone(),
            )));
        }
        if request.name != self.tool_name {
            // AN UNKNOWN TOOL IS A PROTOCOL ERROR, not a tool error: the request
            // could not be routed at all, which is a different fact from a tool
            // that ran and refused.
            return Err(ErrorData {
                code: ErrorCode::METHOD_NOT_FOUND,
                message: format!(
                    "this collector serves the tool {:?} and no other; {:?} was called",
                    self.tool_name, request.name
                )
                .into(),
                data: None,
            });
        }
        let arguments = Value::Object(request.arguments.unwrap_or_else(Map::new));
        match self.handle_collect(&arguments) {
            Ok(structured) => Ok(CallToolResponse::Complete(CallToolResult::structured(
                structured,
            ))),
            Err(message) => Ok(CallToolResponse::Complete(CallToolResult::error(vec![
                ContentBlock::text(message),
            ]))),
        }
    }
}

/// serve_stdio serves this collector over MCP on stdin/stdout and blocks until
/// the session ends.
///
/// It is the entry point for a collector installed as a `type: stdio` config
/// entry, which is the shape a daemon spawns.
pub async fn serve_stdio<C: Collector>(collector: C) -> Result<()> {
    let server = CollectorServer::new(collector)?;
    let running = server
        .serve(rmcp::transport::stdio())
        .await
        .map_err(|e| Error::new(format!("framework: serving over stdio: {e}")))?;
    running
        .waiting()
        .await
        .map_err(|e| Error::new(format!("framework: the stdio session ended in error: {e}")))?;
    Ok(())
}
