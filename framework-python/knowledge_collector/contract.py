# SPDX-License-Identifier: Apache-2.0

"""contract.py — the CONSUMER-SIDE GATES, replicated on the collector side.

WHY A COLLECTOR LIBRARY CARRIES THE CLIENT'S GATES AT ALL. These three functions
are the knowledge client's own: it runs `check_tool_schemas` at registration and
again at every collect, `validate_result_payload` on every result it receives,
and `decode_result` to turn that result into its envelope. A collector cannot
import them -- the client's package is unimportable from a published sibling
repository in every language -- so a port that shipped without them would leave a
collector author finding out at the operator's registration what a test could
have told them in a second.

THEY ARE THE SAME RULES, STATED HERE. A drift between this copy and the client's
is a defect of this file, and the port's suite pins each rule against the
checked-in contract documents rather than against a transcription of the client's
behaviour.

WHAT THIS FILE IS NOT. It is not a pre-emit validator the server runs on its own
output. The client validates every result it receives regardless, so an output
validator inside the collector would duplicate a check the consumer already
performs -- and would then have to be bypassable for a conformance stub that
must be able to break its own word. The port has no such escape because it has no
such validator; a collector author who wants the check calls these directly.
"""

import json

from .framework import EDGE_WIRE_FIELDS, NODE_WIRE_FIELDS
from .jsonschema import SchemaError, ValidationError, satisfies, validate
from .schema import input_contract_json, output_contract_json

__all__ = [
    "ContractError",
    "check_tool_schemas",
    "validate_result_payload",
    "decode_result",
    "DecodedResult",
]


class ContractError(Exception):
    """A tool's advertised schemas or its result do not satisfy the collector
    contract. Every message names the tool."""


def check_tool_schemas(tool, advertised_input, advertised_output):
    """The HARD GATE the client runs at registration and again at collect: the
    target tool must advertise BOTH an input schema and an output schema, and both
    must satisfy the contract.

    A missing schema or a mismatch raises, naming the tool and the mismatch; there
    is no admit-and-validate-later path.
    """
    if advertised_input is None:
        raise ContractError(
            "custom collector: tool %r advertises NO input schema; the collector contract requires one "
            "(see contract/collector_input.schema.json)" % tool
        )
    if advertised_output is None:
        raise ContractError(
            "custom collector: tool %r advertises NO output schema; the collector contract requires one, "
            "including the walk_complete completeness assertion "
            "(see contract/collector_output.schema.json)" % tool
        )
    _check_against_contract(tool, "input", input_contract_json(), advertised_input)
    _check_against_contract(tool, "output", output_contract_json(), advertised_output)


def _check_against_contract(tool, side, contract_json, advertised):
    try:
        contract = json.loads(contract_json)
    except ValueError as exc:
        raise ContractError("custom collector: the checked-in %s contract schema is unreadable: %s" % (side, exc)) from exc
    if not isinstance(advertised, dict):
        raise ContractError(
            "custom collector: tool %r %s schema is a %s, not a JSON Schema object" % (tool, side, type(advertised).__name__)
        )
    try:
        satisfies(contract, advertised, side + "Schema")
    except ValidationError as exc:
        raise ContractError(
            "custom collector: tool %r %s schema does not satisfy the collector contract: %s" % (tool, side, exc)
        ) from exc
    except SchemaError as exc:
        raise ContractError(
            "custom collector: tool %r %s schema is not a readable JSON Schema: %s" % (tool, side, exc)
        ) from exc


def validate_result_payload(tool, raw):
    """Validate raw result JSON against the checked-in contract output schema.

    Split from decode_result so a caller holding bytes validates through the same
    instrument. `raw` may be bytes, str, or an already-decoded document.
    """
    contract = json.loads(output_contract_json())
    if isinstance(raw, (bytes, bytearray, str)):
        try:
            instance = json.loads(raw)
        except ValueError as exc:
            raise ContractError("custom collector: tool %r result is not JSON: %s" % (tool, exc)) from exc
    else:
        instance = raw
    try:
        validate(instance, contract)
    except ValidationError as exc:
        raise ContractError(
            "custom collector: tool %r result does not satisfy the collector contract's output schema: %s" % (tool, exc)
        ) from exc


class DecodedResult:
    """A result decoded into the envelope's own shape: the two lists and the
    completeness assertion, with every field named by the contract."""

    __slots__ = ("nodes", "edges", "walk_complete")

    def __init__(self, nodes, edges, walk_complete):
        self.nodes = nodes
        self.edges = edges
        self.walk_complete = walk_complete

    def __repr__(self):
        return "DecodedResult(nodes=%d, edges=%d, walk_complete=%r)" % (len(self.nodes), len(self.edges), self.walk_complete)


_ENVELOPE_WIRE_FIELDS = ("nodes", "edges", "walk_complete")


def decode_result(tool, structured):
    """Validate a tool call's structuredContent against the CONTRACT output schema
    and decode it into the envelope.

    TWO GATES, ANSWERING DIFFERENT QUESTIONS. The schema validation asks whether
    the payload is a conforming collector result at all; the strict decode asks
    whether every field it carries is one the contract defines. A key the envelope
    does not name is an ERROR rather than a silent drop, because a typo'd field
    name silently dropped is a collector author debugging an empty graph with no
    message to go on.
    """
    if structured is None:
        raise ContractError(
            "custom collector: tool %r returned no structuredContent; the contract requires a structured result "
            "matching its advertised output schema" % tool
        )
    validate_result_payload(tool, structured)
    if isinstance(structured, (bytes, bytearray, str)):
        structured = json.loads(structured)

    _refuse_undefined(tool, structured, _ENVELOPE_WIRE_FIELDS, "the result")
    for index, node in enumerate(structured["nodes"]):
        _refuse_undefined(tool, node, NODE_WIRE_FIELDS, "node[%d]" % index)
    for index, edge in enumerate(structured["edges"]):
        _refuse_undefined(tool, edge, EDGE_WIRE_FIELDS, "edge[%d]" % index)

    return DecodedResult(structured["nodes"], structured["edges"], structured["walk_complete"])


def _refuse_undefined(tool, doc, known, where):
    for key in doc:
        if key not in known:
            raise ContractError(
                "custom collector: tool %r result carries a field this collector contract does not define: "
                "%s has the key %r (the contract defines %s)" % (tool, where, key, ", ".join(known))
            )
