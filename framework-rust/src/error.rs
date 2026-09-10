// SPDX-License-Identifier: Apache-2.0

//! error.rs — the ONE error type this crate returns, and why it carries a
//! rendered message rather than a variant per cause.
//!
//! BAD INPUT ALWAYS ERRORS, NEVER COERCES. Every refusal in this crate is a
//! value of this type and every one of them names the collector (the served
//! tool name), because that is the only identity this crate has for a collector
//! and a refusal reaching an operator has to name something they can find in
//! their config entry.
//!
//! WHY A MESSAGE AND NOT AN ENUM. A collector author never branches on which
//! refusal fired — the refusals are programming errors in the collector or bad
//! input from the caller, and both are read rather than handled. An enum would
//! be a public API surface with a variant per message, which is a compatibility
//! obligation this crate would owe for no caller's benefit. The messages
//! themselves are the contract, and the crate's own suite asserts their
//! substrings.

use std::fmt;

/// Error is a refusal from this crate: a malformed collector, a bad call, or a
/// result that does not satisfy the collector contract.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error {
    message: String,
}

impl Error {
    /// new builds a refusal from an already-rendered message.
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }

    /// message is the rendered refusal.
    pub fn message(&self) -> &str {
        &self.message
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for Error {}

/// WalkError is what a collector's own walk returns when it fails.
///
/// IT IS A BOXED STANDARD ERROR RATHER THAN THIS CRATE'S TYPE, because a walk
/// fails for the collector author's own reasons — an HTTP status, a parse
/// failure, a missing directory — and forcing those through a framework type
/// would make every collector author restate their own error. The serving layer
/// renders it into a TOOL error naming the collector, exactly as the Go
/// framework does.
pub type WalkError = Box<dyn std::error::Error + Send + Sync>;

/// Result is this crate's result alias for its own refusals.
pub type Result<T> = std::result::Result<T, Error>;
