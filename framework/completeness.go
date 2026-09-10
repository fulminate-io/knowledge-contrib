// SPDX-License-Identifier: Apache-2.0

package framework

// completeness.go — the COMPLETENESS ASSERTION a walk makes about itself, as a
// type with no usable zero value.
//
// WHAT THE ASSERTION DECIDES DOWNSTREAM. It rides through the envelope's
// walk_complete field to the server's deletion guard: a collect asserting a
// complete walk lets the server treat the rows this collect did not carry as
// gone, and a collect asserting an incomplete one disables that phase exactly
// as an incomplete code walk does. Asserting completeness for a walk that gave
// up half way is therefore not a cosmetic error.
//
// WHY IT IS A TYPE AND NOT A bool. A bool has a zero value that reads as a
// deliberate "incomplete", so a collector author who forgot to think about
// completeness and one who decided the walk was partial produce the same value
// and are indistinguishable to a reviewer. The type below cannot be built
// meaningfully outside this package — its fields are unexported and its only
// constructors are [Complete] and [Incomplete] — and the walk must RETURN one,
// so an author who omits the decision gets a compile error rather than a
// silently incomplete walk.
//
// THE RESIDUAL IS STATED RATHER THAN HIDDEN: a collector can still write the
// zero literal `framework.Completeness{}` into a Result. That value is REFUSED
// LOUDLY when the envelope is encoded, naming the collector, which is this
// repository's bad-input invariant applied to its own API.

// Completeness is a walk's assertion about whether it enumerated the whole
// source. Build it with [Complete] or [Incomplete]; its zero value is not a
// valid assertion and is refused when a result carrying it is encoded.
type Completeness struct {
	// asserted distinguishes a constructed value from the zero value. It is
	// what makes "the author never decided" observable.
	asserted bool
	complete bool
	reason   string
}

// Complete asserts that this walk enumerated the whole source.
func Complete() Completeness {
	return Completeness{asserted: true, complete: true}
}

// Incomplete asserts that this walk did NOT enumerate the whole source, and
// carries the reason it did not.
//
// The reason is not sent on the wire — the contract carries a boolean — but it
// is required all the same, because it is what a collector author writes into
// their own logs and what a reviewer reads to judge whether the arm is
// reachable at all. An empty reason is refused when the result is encoded, on
// the same terms as the zero value.
func Incomplete(reason string) Completeness {
	return Completeness{asserted: true, complete: false, reason: reason}
}

// IsComplete reports the assertion this value carries. It is meaningful only on
// an asserted value; see [Completeness.IsAsserted].
func (c Completeness) IsComplete() bool { return c.complete }

// IsAsserted reports whether this value was built by [Complete] or
// [Incomplete] rather than left as the zero value.
func (c Completeness) IsAsserted() bool { return c.asserted }

// Reason is the reason an incomplete walk carries, and is empty on a complete
// one. A collector logs it; the wire does not.
func (c Completeness) Reason() string { return c.reason }
