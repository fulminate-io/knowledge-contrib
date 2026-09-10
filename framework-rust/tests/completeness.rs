// SPDX-License-Identifier: Apache-2.0

//! completeness.rs — row R1.7 (a): the arm the Go framework refuses at run time
//! and this port refuses at COMPILE time.
//!
//! Go's `Completeness` is a struct with an `asserted` flag, because a Go struct
//! always has a zero value a collector can write literally; its envelope refuses
//! that value loudly, and the Go doc states the residual rather than hiding it.
//! A Rust enum with no `Default` impl has no such value. So the observable that
//! stands in for the Go arm is the ABSENCE OF `Default`, asserted here rather
//! than asserted in a comment.

use std::marker::PhantomData;

use knowledge_collector_framework::completeness::Completeness;

/// Probe is the carrier for an autoref-specialization check: whether a type
/// implements `Default` is decided by which of the two `is_default` methods
/// resolves, and the by-value inherent impl wins whenever the bound holds.
struct Probe<T>(PhantomData<T>);

impl<T: Default> Probe<T> {
    fn is_default(&self) -> bool {
        true
    }
}

trait NotDefault {
    /// `self` BY VALUE on the reference type is what makes this candidate sit at
    /// the same autoref step as the inherent method above, where an inherent
    /// candidate wins whenever its bound holds.
    fn is_default(self) -> bool;
}

impl<T> NotDefault for &Probe<T> {
    fn is_default(self) -> bool {
        false
    }
}

/// implements_default answers whether ONE NAMED TYPE implements `Default`.
///
/// IT IS A MACRO RATHER THAN A GENERIC FUNCTION, and the reason is the mechanism
/// rather than taste: autoref specialization resolves at the CALL SITE, so a
/// generic `fn implements_default<T>()` would have to resolve the method once
/// against an unbounded `T` and could never reach the inherent impl. Each
/// expansion below names a concrete type, which is what lets the two candidates
/// be distinguished.
macro_rules! implements_default {
    ($t:ty) => {{
        // The receiver is a VALUE, not a reference: method resolution tries the
        // inherent `Probe<$t>` impl (which needs `$t: Default`) one autoref step
        // before it reaches the blanket trait impl on `&Probe<$t>`. Starting
        // from a reference skips the inherent candidate and always answers
        // false.
        Probe::<$t>(PhantomData).is_default()
    }};
}

// ---------------------------------------------------------------------------
// R1.7 (a) — the completeness type CANNOT be constructed unasserted, so there is
// no run-time arm to refuse. The same-run KNOWN POSITIVE is the point of the
// second assertion: without it, an `implements_default` that always answered
// false would pass this test.
// ---------------------------------------------------------------------------
#[test]
fn completeness_has_no_unasserted_value() {
    assert!(
        implements_default!(String),
        "control: the probe must answer TRUE for a type that does implement Default, \
         or its answer for Completeness means nothing"
    );
    assert!(
        !implements_default!(Completeness),
        "Completeness must NOT implement Default: a default value would be an assertion \
         a collector author never made, which is exactly what the type exists to prevent"
    );
}

// ---------------------------------------------------------------------------
// The two assertions the type does carry, and the reason it carries a reason.
// ---------------------------------------------------------------------------
#[test]
fn the_two_assertions_report_themselves() {
    let complete = Completeness::complete();
    assert!(complete.is_complete());
    assert_eq!(complete.reason(), "", "a complete walk carries no reason");

    let incomplete = Completeness::incomplete("the source moved under the walk");
    assert!(!incomplete.is_complete());
    assert_eq!(incomplete.reason(), "the source moved under the walk");
}
