// SPDX-License-Identifier: Apache-2.0

//! framework.rs — the COLLECTOR-AUTHOR SURFACE: the one trait a collector
//! implements and the tool it names.
//!
//! THE DIVISION OF LABOR IS THE POINT. Nothing about MCP, JSON Schema or the
//! envelope is visible to a collector: it implements [`Collector`] and calls
//! [`crate::serve::serve_stdio`]. A collector that carries MCP server code or
//! envelope-encoding code of its own has re-derived something this crate already
//! settled.
//!
//! THIS CRATE IS NOT THE CLIENT'S. The knowledge client's own consumer side
//! (dial, verify, call, convert) is a different program in a different language;
//! the contract between the two is the checked-in JSON schema pair this crate
//! embeds a copy of. See `schema.rs` for what that costs and how the copy is
//! kept honest.
//!
//! THE WALK IS SYNCHRONOUS, AND THAT IS A DELIBERATE DEPARTURE FROM THE GO
//! SIGNATURE, which takes a `context.Context`. Three reasons, in the order they
//! bind. (1) rmcp's `ServerHandler` requires futures that are `Send`, and an
//! `async fn` in a trait produces one that is not without a `-> impl Future +
//! Send` return-position bound that every collector author would then have to
//! spell. (2) A collector's walk is ONE call per collect and this crate serves
//! one stdio session, so there is no concurrency for an async walk to buy. (3) A
//! synchronous walk is what lets the serving layer CATCH A PANIC and turn it
//! into a tool error rather than a dead process — `std::panic::catch_unwind`
//! takes a closure, and containing a panic that escapes a future needs machinery
//! that would be this crate's rather than the standard library's. A walk that
//! wants async internally builds its own runtime or blocks on one.

use schemars::JsonSchema;
use serde::de::DeserializeOwned;

use crate::context::ForeignContext;
use crate::describe::Declaration;
use crate::error::WalkError;
use crate::model::WalkResult;

/// DEFAULT_TOOL_NAME is the tool name a collector serves when its [`ToolSpec`]
/// names none. It matches the `tool` value in the config-file entry's own worked
/// example.
pub const DEFAULT_TOOL_NAME: &str = "collect";

/// DESCRIBE_TOOL_NAME is the REQUIRED second tool every collector serves.
///
/// ITS NAME IS FIXED RATHER THAN DECLARED, and the reason is a bootstrap one:
/// the client has to know what to call before it can call anything, so the
/// declaration cannot name the tool that carries it. The value mirrors the Go
/// framework's own constant, and a pending pin reads that constant out of the
/// framework's source and asserts this crate serves a tool by exactly it —
/// rather than the two being one literal typed twice.
pub const DESCRIBE_TOOL_NAME: &str = "describe";

/// ToolSpec names the single MCP tool a collector serves.
///
/// The name is the collector's, not a constant: the config-file entry that
/// registers a collector carries a `tool` field naming the one tool the daemon
/// calls, and an operator is free to serve `collect_logs` beside another
/// provider's `collect`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ToolSpec {
    /// name is the served tool's name. Empty means [`DEFAULT_TOOL_NAME`].
    pub name: String,
    /// description is the tool's human-readable description, shown in an MCP
    /// tool listing. Empty is allowed; a collector that means to be installed by
    /// a human should write one.
    pub description: String,
}

impl ToolSpec {
    /// new builds a spec from a name and a description.
    pub fn new(name: impl Into<String>, description: impl Into<String>) -> Self {
        ToolSpec {
            name: name.into(),
            description: description.into(),
        }
    }

    /// resolved_name is the spec's name, defaulted.
    pub fn resolved_name(&self) -> &str {
        if self.name.is_empty() {
            DEFAULT_TOOL_NAME
        } else {
            &self.name
        }
    }
}

/// Collector is the whole surface a collector author implements.
///
/// [`Collector::Params`] is the collector's params type: this crate infers its
/// JSON Schema, advertises it inside the contract's input schema, and hands the
/// walk an ALREADY-VALIDATED value of it — a call whose params do not satisfy
/// the advertised schema is refused before [`Collector::walk`] is reached.
///
/// THAT PROMISE IS THIS CRATE'S OWN AND IT IS ENFORCED HERE, which is where this
/// port diverges from the Go framework in mechanism while keeping its contract.
/// In Go the refusal is the SDK's: `mcp.AddTool`'s generic form validates the
/// call arguments against the advertised input schema before the handler runs.
/// rmcp performs NO such validation — a call with the required property missing,
/// with a wrong-typed property and with an undeclared property each reach the
/// handler untouched — so `serve.rs` runs the check itself. A port that copied
/// this doc comment without porting the check would ship a documented guarantee
/// nothing enforces.
///
/// The `id` the walk receives is the collect id, which names the graph INSTANCE
/// the result lands in. It is NOT the graph family: the family is the
/// registration name, which the client derives on its own side and never sends,
/// so a collector cannot choose the graph type it writes into.
pub trait Collector: Send + Sync + 'static {
    /// Params is this collector's params type.
    ///
    /// It owes `Default` because the client OMITS `params` entirely from a
    /// paramless collect — the contract's input schema does not require the
    /// property — so the absent case has to decode into something. A collector
    /// with no parameters declares a unit struct.
    type Params: DeserializeOwned + JsonSchema + Default + Send + 'static;

    /// tool names the MCP tool this collector serves.
    fn tool(&self) -> ToolSpec;

    /// declaration is what this collector says about itself: the node and edge
    /// vocabulary its walk emits, the environment it needs and in which class,
    /// the graph-level behavior, and the foreign-graph context it reads.
    ///
    /// IT IS REQUIRED, NOT OPTIONAL, because the contract's second tool is. A
    /// collector that does not serve a conforming declaration is refused at
    /// registration, so a default here would only move the refusal later.
    ///
    /// DECLARING A VOCABULARY IS A PROMISE THE INGEST PATH ENFORCES: a collect
    /// carrying a node type this declaration omits is refused, naming the node
    /// and the offending value. Declaring an EMPTY vocabulary is a different
    /// posture rather than an unfilled field — it accepts everything and the
    /// client says so once per collect.
    fn declaration(&self) -> Declaration;

    /// walk enumerates the source and returns what it found, together with the
    /// completeness assertion for this walk. An `Err` becomes a TOOL-call error
    /// naming the collector and the cause; it never becomes an empty successful
    /// result.
    ///
    /// `foreign` is the DECLARED FOREIGN-GRAPH CONTEXT: the cloud resources or
    /// code-graph nodes this collector's registration entry declared it needs,
    /// read out of the operator's own graphs by the client and sent with the
    /// call. It is EMPTY for a collector whose entry declares nothing.
    ///
    /// IT IS A THIRD PARAMETER RATHER THAN A FIELD ON A REQUEST STRUCT, and the
    /// choice is worth stating because it is the one this seam had. A request
    /// struct would let this crate add inputs later without moving the
    /// signature; a parameter makes the block impossible to receive by accident
    /// and impossible to ignore by omission. A collector with no use for the
    /// block names it `_`.
    fn walk(
        &self,
        id: &str,
        params: Self::Params,
        foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError>;
}
