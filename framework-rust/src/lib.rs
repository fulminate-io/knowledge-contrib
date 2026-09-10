// SPDX-License-Identifier: Apache-2.0

//! The common MCP serving and contract layer a custom knowledge collector is
//! built on, in Rust.
//!
//! A collector author writes a WALK and a params type; this crate serves that
//! walk as an MCP tool over stdio, advertises the collector contract's input and
//! output schemas, validates the call's arguments before the walk runs, and
//! encodes the walk's result as the contract envelope.
//!
//! # The division of labor is the point
//!
//! Nothing about MCP, JSON Schema or the envelope is visible to a collector: it
//! implements [`framework::Collector`] and calls [`serve::serve_stdio`]. A
//! collector that carries MCP server code or envelope-encoding code of its own
//! has re-derived something this crate already settled.
//!
//! # This crate is not the client's
//!
//! The knowledge client's own consumer side — dial, verify, call, convert — is a
//! different program. The contract between the two is the checked-in JSON schema
//! pair this crate embeds a copy of, never a shared library. [`schema`] says what
//! that costs and how the copy is kept honest.
//!
//! # What this crate refuses, and why it refuses it here
//!
//! Bad input always errors; there is no silent coercion, no default on error and
//! no degraded path. That invariant lands harder in Rust than in Go, because the
//! Rust MCP SDK validates NEITHER the call's arguments against the advertised
//! input schema NOR the handler's result against the advertised output schema —
//! measured on both halves against a same-run control. The Go framework's SDK
//! does both, so its handler can assume validated input. Here the checks are
//! this crate's own: [`serve::CollectorServer::validate_call_arguments`] for the
//! call and the typed [`envelope::CollectOutput`] for the result. A port that
//! copied the Go doc comments without porting the checks would ship a documented
//! guarantee nothing enforces.
//!
//! # Worked example
//!
//! ```no_run
//! use knowledge_collector_framework::describe::{Declaration, Vocabulary};
//! use knowledge_collector_framework::prelude::*;
//!
//! #[derive(Debug, Default, serde::Deserialize, schemars::JsonSchema)]
//! struct Params {
//!     /// The directory to walk.
//!     root: String,
//! }
//!
//! struct Files;
//!
//! impl Collector for Files {
//!     type Params = Params;
//!
//!     fn tool(&self) -> ToolSpec {
//!         ToolSpec::new("collect", "walk a directory into a graph")
//!     }
//!
//!     fn declaration(&self) -> Declaration {
//!         Declaration {
//!             vocabulary: Vocabulary::new(vec!["directory".into()], vec![]),
//!             ..Declaration::default()
//!         }
//!     }
//!
//!     fn walk(
//!         &self,
//!         id: &str,
//!         params: Params,
//!         _foreign: &ForeignContext,
//!     ) -> Result<WalkResult, WalkError> {
//!         let node = Node::new(params.root.clone(), "directory");
//!         Ok(WalkResult::new(vec![node], vec![], Completeness::complete()))
//!     }
//! }
//!
//! #[tokio::main]
//! async fn main() -> Result<(), Box<dyn std::error::Error>> {
//!     serve_stdio(Files).await?;
//!     Ok(())
//! }
//! ```

pub mod completeness;
pub mod context;
pub mod contract;
pub mod describe;
pub mod envelope;
pub mod error;
pub mod framework;
pub mod model;
pub mod sample;
pub mod schema;
pub mod serve;

#[doc(hidden)]
pub mod conformance;

/// prelude re-exports everything a collector author needs in one line.
pub mod prelude {
    pub use crate::completeness::Completeness;
    pub use crate::context::{ForeignContext, ForeignEdge, ForeignGraph, ForeignNode, FAMILY_CODE};
    pub use crate::error::{Error, WalkError};
    pub use crate::framework::{Collector, ToolSpec, DEFAULT_TOOL_NAME};
    pub use crate::model::{Edge, Node, WalkResult};
    pub use crate::serve::serve_stdio;
}

pub use completeness::Completeness;
pub use context::{ForeignContext, ForeignEdge, ForeignGraph, ForeignNode, FAMILY_CODE};
pub use envelope::{encode_result, CollectOutput};
pub use error::{Error, WalkError};
pub use framework::{Collector, ToolSpec, DEFAULT_TOOL_NAME};
pub use model::{Edge, Node, WalkResult};
pub use serve::{serve_stdio, CollectorServer, PROTOCOL_VERSION};
