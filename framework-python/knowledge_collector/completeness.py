# SPDX-License-Identifier: Apache-2.0

"""completeness.py — the COMPLETENESS ASSERTION a walk makes about itself.

WHAT THE ASSERTION DECIDES DOWNSTREAM. It rides through the envelope's
`walk_complete` field to the server's deletion guard: a collect asserting a
complete walk lets the server treat the rows this collect did not carry as gone,
and a collect asserting an incomplete one disables that phase exactly as an
incomplete code walk does. Asserting completeness for a walk that gave up half
way is therefore not a cosmetic error.

WHY IT IS A TYPE AND NOT A bool. A bare False reads as a deliberate "incomplete",
so an author who forgot to think about completeness and one who decided the walk
was partial produce the same value and are indistinguishable to a reviewer. The
Go framework reaches for a struct with no usable zero value and gets a compile
error for the author who omits the decision; Python has no compile step to lean
on, so the port makes the assertion a REQUIRED CONSTRUCTOR ARGUMENT of Result and
keeps the two refusals in the encoder. That pairing is the same observable: a
walk that asserts nothing is refused loudly, naming the collector.

THE RESIDUAL IS STATED RATHER THAN HIDDEN: an author can still build the bare
`Completeness()` and put it in a Result. That value asserts nothing and is
REFUSED LOUDLY when the envelope is encoded, which is this repository's bad-input
invariant applied to the framework's own API.
"""

__all__ = ["Completeness", "Complete", "Incomplete"]


class Completeness:
    """A walk's assertion about whether it enumerated the whole source.

    Build it with `Complete()` or `Incomplete(reason)`. The bare constructor
    produces the UNASSERTED value, which exists so the "the author never decided"
    arm is reachable and refusable rather than unrepresentable.
    """

    __slots__ = ("_asserted", "_complete", "_reason")

    def __init__(self):
        self._asserted = False
        self._complete = False
        self._reason = ""

    def is_asserted(self):
        """Report whether this value was built by Complete() or Incomplete()."""
        return self._asserted

    def is_complete(self):
        """Report the assertion this value carries. Meaningful only on an
        asserted value; see is_asserted()."""
        return self._complete

    def reason(self):
        """The reason an incomplete walk carries, empty on a complete one. A
        collector logs it; the wire does not."""
        return self._reason

    def __repr__(self):
        if not self._asserted:
            return "Completeness(unasserted)"
        if self._complete:
            return "Completeness(complete)"
        return "Completeness(incomplete, reason=%r)" % self._reason

    def __eq__(self, other):
        if not isinstance(other, Completeness):
            return NotImplemented
        return (self._asserted, self._complete, self._reason) == (other._asserted, other._complete, other._reason)

    def __hash__(self):
        return hash((self._asserted, self._complete, self._reason))


def Complete():  # noqa: N802 -- mirrors the Go framework's exported name
    """Assert that this walk enumerated the whole source."""
    value = Completeness()
    value._asserted = True
    value._complete = True
    return value


def Incomplete(reason):  # noqa: N802 -- mirrors the Go framework's exported name
    """Assert that this walk did NOT enumerate the whole source, and carry the
    reason it did not.

    The reason is not sent on the wire -- the contract carries a boolean -- but it
    is required all the same, because it is what a collector author writes into
    their own logs and what a reviewer reads to judge whether the arm is
    reachable at all. An empty reason is refused when the result is encoded, on
    the same terms as the unasserted value.
    """
    value = Completeness()
    value._asserted = True
    value._complete = False
    value._reason = reason
    return value
