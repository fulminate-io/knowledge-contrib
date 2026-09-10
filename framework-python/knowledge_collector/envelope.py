# SPDX-License-Identifier: Apache-2.0

"""envelope.py — the CONTRACT ENVELOPE the served tool returns, and the three
refusals plus the one normalization that stand between a walk's Result and it.

NO FIELD OF THE ENVELOPE IS EVER OMITTED, and `walk_complete` is the one that
matters. The contract output schema REQUIRES walk_complete, and the client
validates the result against that schema; a walk asserting INCOMPLETE that
emitted no walk_complete would fail the whole collect with a missing-property
error. A walk asserting COMPLETE emits `true` and the defect is invisible, which
is why the test that pins this rides the incomplete arm.
"""

from .completeness import Completeness

__all__ = ["EnvelopeError", "encode_result"]


class EnvelopeError(Exception):
    """A walk's Result cannot become a contract envelope. Every message names the
    collector, which is the only identity this package has for it, so a refusal
    reaching an operator names something they can find in their config entry."""


def encode_result(collector, result):
    """Turn one walk's Result into the envelope, refusing the shapes that are bad
    input and normalizing the one that is a language artifact.

    Three refusals, each its own arm so a mutation that deletes one turns exactly
    one test red:

      1. a completeness value that asserts nothing;
      2. an INCOMPLETE assertion carrying no reason;
      3. a node with an empty type.
    """
    complete = getattr(result, "complete", None)
    if not isinstance(complete, Completeness):
        raise EnvelopeError(
            "framework: collector %r returned a result whose completeness is %r, which is not a Completeness; "
            "a walk returns Complete() or Incomplete(reason)" % (collector, complete)
        )
    if not complete.is_asserted():
        raise EnvelopeError(
            "framework: collector %r returned an unasserted Completeness, which asserts nothing; "
            "a walk returns Complete() or Incomplete(reason)" % collector
        )
    if not complete.is_complete() and complete.reason() == "":
        raise EnvelopeError(
            "framework: collector %r asserted an INCOMPLETE walk with no reason; "
            "Incomplete takes the reason the walk did not finish" % collector
        )

    nodes = result.nodes if result.nodes is not None else []
    edges = result.edges if result.edges is not None else []

    wire_nodes = []
    for index, node in enumerate(nodes):
        doc = node.to_wire() if hasattr(node, "to_wire") else dict(node)
        if doc.get("type", "") == "":
            raise EnvelopeError(
                "framework: collector %r: node[%d] (id=%r) has an empty type" % (collector, index, doc.get("id", ""))
            )
        wire_nodes.append(doc)

    wire_edges = [edge.to_wire() if hasattr(edge, "to_wire") else dict(edge) for edge in edges]

    # THE EMPTY WALK IS NORMALIZED, and this is the line between a collector that
    # finds nothing and a collect that fails. A walk that found nothing leaves
    # both lists empty; None would serialize to `null`; the contract output schema
    # declares both as "array" with no null union; and the client validates this
    # value against that schema, so the call would fail with a type error naming
    # whichever of the two properties the validator reached first. An empty
    # region, an empty log group or a filter that matched nothing is the first
    # real run of a new collector, so this is the cell it hits.
    #
    # It emits an empty ARRAY, never a fabricated node: an empty complete collect
    # lands an empty generation, and the server's deletion phase declines to
    # derive anything from a collect that named nothing.
    return {"nodes": wire_nodes, "edges": wire_edges, "walk_complete": complete.is_complete()}
