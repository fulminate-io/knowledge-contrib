// SPDX-License-Identifier: Apache-2.0

//! completeness.rs — the COMPLETENESS ASSERTION a walk makes about itself, as a
//! type with no unasserted state at all.
//!
//! WHAT THE ASSERTION DECIDES DOWNSTREAM. It rides through the envelope's
//! `walk_complete` field to the server's deletion guard: a collect asserting a
//! complete walk lets the server treat the rows this collect did not carry as
//! gone, and a collect asserting an incomplete one disables that phase exactly
//! as an incomplete code walk does. Asserting completeness for a walk that gave
//! up half way is therefore not a cosmetic error.
//!
//! WHY IT IS A TYPE AND NOT A `bool`. A bool's `false` reads as a deliberate
//! "incomplete", so an author who forgot to think about completeness and one who
//! decided the walk was partial produce the same value and are
//! indistinguishable to a reviewer. The walk must RETURN one of these, so an
//! author who omits the decision gets a compile error.
//!
//! THE GO FRAMEWORK'S RESIDUAL DOES NOT EXIST HERE, and that is the one place
//! this port is stronger than what it mirrors. The Go type is a struct with an
//! `asserted` flag, because a Go struct always has a zero value a collector can
//! write literally, and `cmd/collectors/framework/completeness.go:24-27` states
//! that residual rather than hiding it. A Rust enum with no `Default` impl and
//! no unit-struct spelling has no such value: there is nothing to construct
//! except the two assertions, so the "unasserted" arm is absent at compile time
//! rather than refused at run time. The `asserted` flag is deliberately NOT
//! ported; the crate's suite asserts the absence of `Default` instead, which is
//! the observable that stands in for the Go arm.

use std::fmt;

/// Completeness is a walk's assertion about whether it enumerated the whole
/// source.
///
/// There is no third state and no default: a walk returns [`Completeness::Complete`]
/// or [`Completeness::Incomplete`] carrying the reason it did not finish.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Completeness {
    /// The walk enumerated the whole source.
    Complete,
    /// The walk did NOT enumerate the whole source, and carries the reason.
    ///
    /// The reason is not sent on the wire — the contract carries a boolean — but
    /// it is required all the same, because it is what a collector author writes
    /// into their own logs and what a reviewer reads to judge whether the arm is
    /// reachable at all. An EMPTY reason is refused when the result is encoded.
    Incomplete { reason: String },
}

impl Completeness {
    /// complete asserts that this walk enumerated the whole source.
    ///
    /// It is the constructor spelling of [`Completeness::Complete`], kept so a
    /// collector reads the same way as the Go framework's `framework.Complete()`.
    pub fn complete() -> Self {
        Completeness::Complete
    }

    /// incomplete asserts that this walk did NOT enumerate the whole source.
    pub fn incomplete(reason: impl Into<String>) -> Self {
        Completeness::Incomplete {
            reason: reason.into(),
        }
    }

    /// is_complete reports the assertion this value carries.
    pub fn is_complete(&self) -> bool {
        matches!(self, Completeness::Complete)
    }

    /// reason is the reason an incomplete walk carries, and is empty on a
    /// complete one. A collector logs it; the wire does not.
    pub fn reason(&self) -> &str {
        match self {
            Completeness::Complete => "",
            Completeness::Incomplete { reason } => reason,
        }
    }
}

impl fmt::Display for Completeness {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Completeness::Complete => f.write_str("complete"),
            Completeness::Incomplete { reason } => write!(f, "incomplete: {reason}"),
        }
    }
}
