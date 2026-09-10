// SPDX-License-Identifier: Apache-2.0

//! envelope.rs — the CONTRACT ENVELOPE the served tool returns, and the refusals
//! that stand between a walk's [`WalkResult`] and it.
//!
//! NO SKIP-IF-EMPTY ON ANY OF THE THREE FIELDS, and `walk_complete` is the one
//! that matters. The contract output schema REQUIRES `walk_complete`; with a
//! skip attribute a walk asserting INCOMPLETE would emit no `walk_complete` at
//! all and the whole collect would fail with a missing-property error at the
//! CLIENT. A walk asserting COMPLETE emits `true` and the defect is invisible,
//! which is why the test that pins this rides the incomplete arm.
//!
//! THIS ENCODE PATH IS MORE LOAD-BEARING HERE THAN IN GO. The Go SDK's generic
//! `AddTool` validates the handler's Out value against the advertised output
//! schema before the result leaves the provider, so Go's `encodeResult` is a
//! first line with a second behind it. rmcp performs no such check: a handler
//! returning `{"nodes": {}, "edges": []}` — `walk_complete` absent, `nodes` an
//! object — reaches the wire unchanged with `isError: false`. This crate's typed
//! envelope is therefore the ONLY thing between a walk and a non-conforming
//! result, and it earns that by construction: the three fields are typed, none
//! is optional, and a `Vec` has no null spelling.
//!
//! THE EMPTY WALK NEEDS NO NORMALIZATION HERE, and the reason is worth stating
//! rather than leaving as a silence. Go normalizes a nil slice to an empty one
//! because nil marshals to `null` and the contract declares both properties as
//! `array` with no null union — an empty region or a filter that matched nothing
//! is the first real run of a new collector, so that is the cell it hits. A Rust
//! `Vec` is never null, so the rule holds for free; what would REINTRODUCE it is
//! an `Option<Vec<_>>` field or a `skip_serializing_if` attribute, and neither is
//! here. The crate's suite still asserts the emitted BYTES carry `[]`, because
//! "for free" is a property of this file's current spelling, not of Rust.

use serde::{Deserialize, Serialize};

use crate::completeness::Completeness;
use crate::error::{Error, Result};
use crate::model::{Edge, Node, WalkResult};

/// CollectOutput is the served tool's output: the contract envelope, exactly.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct CollectOutput {
    pub nodes: Vec<Node>,
    pub edges: Vec<Edge>,
    pub walk_complete: bool,
}

/// encode_result turns one walk's [`WalkResult`] into the envelope, refusing the
/// shapes that are bad input.
///
/// `collector` names the served tool, which is the only identity this crate has
/// for the collector, so a refusal reaching an operator names something they can
/// find in their config entry.
///
/// THE GO FRAMEWORK HAS THREE REFUSALS HERE AND THIS ONE HAS TWO, which is a
/// difference in the type system rather than in the contract. Go's first refusal
/// is a zero `Completeness` value that asserts nothing; [`Completeness`] has no
/// such value, so that arm is absent at compile time. The other two are here.
pub fn encode_result(collector: &str, result: WalkResult) -> Result<CollectOutput> {
    if let Completeness::Incomplete { ref reason } = result.complete {
        if reason.is_empty() {
            return Err(Error::new(format!(
                "framework: collector {collector:?} asserted an INCOMPLETE walk with no reason; \
                 Completeness::incomplete takes the reason the walk did not finish"
            )));
        }
    }
    for (i, node) in result.nodes.iter().enumerate() {
        if node.node_type.is_empty() {
            return Err(Error::new(format!(
                "framework: collector {collector:?}: node[{i}] (id={:?}) has an empty type",
                node.id
            )));
        }
    }
    Ok(CollectOutput {
        walk_complete: result.complete.is_complete(),
        nodes: result.nodes,
        edges: result.edges,
    })
}
