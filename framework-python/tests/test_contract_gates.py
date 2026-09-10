# SPDX-License-Identifier: Apache-2.0

"""R2(b) — the THIRTEEN Go-unit contract properties, replicated one by one.

THE PARITY LIST IS DERIVED, NOT ENUMERATED. The authority is the four client test
files, and `grep -c '^func Test'` over them gives 9 + 5 + 12 + 1 = 27, splitting
into ELEVEN provider-dialing tests and SIXTEEN Go-only units. Re-run that
derivation at the tree you land on rather than copying a list out of a comment.
`test_derivation.py` in this suite runs it as a test where the client's files are
in reach.

THE SIXTEEN SPLIT THREE WAYS AND ONLY THIRTEEN HAVE A COLLECTOR-SIDE REFERENT.

  * THE THIRTEEN below. Each is an assertion about the two checked-in contract
    schema files or about the comparator's admit/refuse behaviour over documents
    derived from them, and this port ships both files and its own comparator, so it
    has a statable counterpart for each.

  * PROPERTY 15 -- TestChildEnv_EmptyBlockIsAnEmptyEnvironmentNotInheritance -- is
    COVERED, NOT MIRRORED. It calls the client's own childEnv and asserts it
    returns an empty, non-nil environment, which is a PARENT-SIDE spawn rule about
    os/exec's nil-means-inherit; a collector is the child. Its collector-side
    consequence is proven from the child's own vantage by the four env-report arms
    among the eleven dialing tests, and by
    tests/test_conformance_stub.py's env-report rows here. A Python mirror of
    childEnv would assert its own fixture: a collector library builds no child
    environment.

  * PROPERTIES 14 AND 16 ARE EXCLUDED BY NAME, so their absence reads as decided
    rather than as a gap. (14) TestContractSummary_StatesTheContextBlock asserts
    over a string the client splices into the description of its OWN registration
    tool; a collector library renders no such string. (16)
    TestRunMCP_RecordShapeGuards hands the client's RunMCP five malformed values,
    every one of them a shape of the CLIENT's own runtime Registration record,
    which a collector library does not hold.

NO MIRROR IS INVENTED TO REACH A COUNT OF SIXTEEN. A unit constructing a client
artifact so it can assert about it is a test asserting its own fixture: green
forever, observing nothing.
"""

import copy
import json
import unittest

from knowledge_collector import (
    ContractError,
    check_tool_schemas,
    decode_result,
    input_contract_json,
    output_contract_json,
    validate_result_payload,
)


def _contract(raw):
    return json.loads(raw)


def _input():
    return _contract(input_contract_json())


def _output():
    return _contract(output_contract_json())


class Property01_OutputContractRequiresTheCompletenessAssertion(unittest.TestCase):
    """(1) The output contract requires the completeness assertion and declares
    nodes/edges as arrays and walk_complete as a boolean. It reads the CHECKED-IN
    artifact rather than a transcription of it, because the artifact is what a
    collector author copies into their tool."""

    def test_it(self):
        schema = _output()
        self.assertEqual(schema["type"], "object")
        self.assertCountEqual(
            schema["required"],
            ["nodes", "edges", "walk_complete"],
            "the completeness assertion is REQUIRED -- a provider must not be able to omit it and disable deletion by silence",
        )
        props = schema["properties"]
        self.assertEqual(props["walk_complete"]["type"], "boolean")
        self.assertEqual(props["nodes"]["type"], "array")
        self.assertEqual(props["edges"]["type"], "array")


class Property02_InputContractRequiresTheCollectID(unittest.TestCase):
    """(2) The input contract requires only the collect id, which names the graph
    instance; a tool that does not take it cannot be driven."""

    def test_it(self):
        schema = _input()
        self.assertEqual(schema["type"], "object")
        self.assertCountEqual(schema["required"], ["id"])
        props = schema["properties"]
        self.assertEqual(props["id"]["type"], "string")
        self.assertEqual(props["params"]["type"], "object")


class Property03_SchemaAndEnvelopeAgree(unittest.TestCase):
    """(3) The drift guard between the two halves of one contract: the checked-in
    JSON a collector author reads, and the envelope this port decodes into. A
    field added to one and not the other is a contract that says two different
    things."""

    def test_the_schemas_own_minimal_document_decodes_into_the_envelope(self):
        minimal = '{"nodes":[{"id":"a","type":"issue"}],"edges":[{"from_id":"a","to_id":"b","type":"blocks"}],"walk_complete":true}'
        decoded = decode_result("t", minimal)
        self.assertEqual(len(decoded.nodes), 1)
        self.assertEqual(len(decoded.edges), 1)
        self.assertIs(decoded.walk_complete, True)

    def test_the_envelope_names_no_top_level_field_the_schema_does_not(self):
        from knowledge_collector.contract import _ENVELOPE_WIRE_FIELDS

        props = _output()["properties"]
        for key in _ENVELOPE_WIRE_FIELDS:
            self.assertIn(
                key,
                props,
                "the envelope carries the top-level field %r and the checked-in contract schema does not describe it" % key,
            )


class Property04_TheTwoMissingSchemaArms(unittest.TestCase):
    """(4) The two absence arms.

    THE NO-INPUT-SCHEMA ARM IS REACHABLE ONLY AS A UNIT. The client's own note
    says so for the Go side -- an MCP server refuses to publish a tool with no input
    schema at all -- and this suite is the only place it can live here either.
    """

    def test_no_input_schema_is_refused_naming_the_tool(self):
        with self.assertRaises(ContractError) as caught:
            check_tool_schemas("collect_graph", None, _output())
        self.assertIn("NO input schema", str(caught.exception))
        self.assertIn("collect_graph", str(caught.exception))

    def test_no_output_schema_is_refused_naming_the_completeness_assertion(self):
        with self.assertRaises(ContractError) as caught:
            check_tool_schemas("collect_graph", _input(), None)
        self.assertIn("NO output schema", str(caught.exception))
        self.assertIn("walk_complete", str(caught.exception))


class Property05_AcceptsTheContractVerbatimAndStricter(unittest.TestCase):
    """(5) What the comparator ADMITS, which is what keeps it from being a
    schema-equality check: a provider may be stricter than the contract, never
    looser."""

    def test_the_contract_verbatim_is_admitted(self):
        check_tool_schemas("collect_graph", _input(), _output())

    def test_a_provider_declaring_more_than_the_contract_is_admitted(self):
        stricter = _output()
        stricter["required"] = ["nodes", "edges", "walk_complete", "collected_at"]
        stricter["properties"]["collected_at"] = {"type": "string"}
        check_tool_schemas("collect_graph", _input(), stricter)


class Property06_RefusesEachLooseningIndividually(unittest.TestCase):
    """(6) The eight ways an advertised output schema can fall short. Each row
    bends ONE thing, so a comparator that stopped checking any single one of them
    turns a row red rather than leaving the suite green."""

    ROWS = (
        ("top-level type is not an object", lambda out: out.__setitem__("type", "array"), 'the contract requires type "object"'),
        ("walk_complete not required", lambda out: out.__setitem__("required", ["nodes", "edges"]), "walk_complete"),
        ("walk_complete property missing", lambda out: out["properties"].pop("walk_complete"), "walk_complete"),
        (
            "walk_complete declared as a string",
            lambda out: out["properties"].__setitem__("walk_complete", {"type": "string"}),
            'requires type "boolean"',
        ),
        (
            "nodes declared as an object",
            lambda out: out["properties"].__setitem__("nodes", {"type": "object"}),
            'requires type "array"',
        ),
        (
            "node items do not require an id",
            lambda out: out["properties"]["nodes"]["items"].__setitem__("required", ["type"]),
            "outputSchema.nodes[]",
        ),
        (
            "edge items do not require the endpoints",
            lambda out: out["properties"]["edges"]["items"].__setitem__("required", ["type"]),
            "from_id",
        ),
        ("nodes declares no item schema", lambda out: out["properties"]["nodes"].pop("items"), "item schema"),
    )

    def test_each_loosening(self):
        for name, bend, expected in self.ROWS:
            with self.subTest(name):
                out = _output()
                bend(out)
                with self.assertRaises(ContractError, msg="a loosened schema must be refused") as caught:
                    check_tool_schemas("collect_graph", _input(), out)
                self.assertIn(expected, str(caught.exception))
                self.assertIn("collect_graph", str(caught.exception), "the refusal must name the tool")


class Property07_PayloadGateRefusesNonConformingResults(unittest.TestCase):
    """(7) The payload gate, a different question from the schema gate: a provider
    may advertise a perfect schema and still return something else."""

    ROWS = (
        ("missing walk_complete", '{"nodes":[],"edges":[]}', "walk_complete"),
        ("nodes is not an array", '{"nodes":{},"edges":[],"walk_complete":true}', "nodes"),
        ("node missing its type", '{"nodes":[{"id":"a"}],"edges":[],"walk_complete":true}', "type"),
        ("walk_complete is a string", '{"nodes":[],"edges":[],"walk_complete":"yes"}', "walk_complete"),
    )

    def test_each_row(self):
        for name, payload, expected in self.ROWS:
            with self.subTest(name):
                with self.assertRaises(ContractError) as caught:
                    validate_result_payload("collect_graph", payload)
                self.assertIn("collect_graph", str(caught.exception))
                self.assertIn(expected, str(caught.exception))

    def test_control_a_conforming_payload_is_admitted(self):
        validate_result_payload("collect_graph", '{"nodes":[],"edges":[],"walk_complete":true}')

    def test_a_boolean_is_not_admitted_where_the_schema_says_integer(self):
        """Python's bool is a subclass of int, so a validator written without that
        distinction admits `true` for start_line. The contract does not constrain
        start_line, so the row rides the validator directly."""
        from knowledge_collector.jsonschema import ValidationError, validate

        with self.assertRaises(ValidationError):
            validate(True, {"type": "integer"})
        validate(7, {"type": "integer"})


class Property08_AnUndefinedFieldIsAnErrorNotASilentDrop(unittest.TestCase):
    """(8) A typo'd key is an error rather than a silent drop: a dropped field is a
    collector author debugging an empty graph with no message to go on."""

    def test_it(self):
        with self.assertRaises(ContractError) as caught:
            decode_result(
                "collect_graph",
                '{"nodes":[{"id":"a","type":"issue","summry":"typo"}],"edges":[],"walk_complete":true}',
            )
        self.assertIn("summry", str(caught.exception))

    def test_control_the_correctly_spelled_key_is_admitted(self):
        decoded = decode_result(
            "collect_graph", '{"nodes":[{"id":"a","type":"issue","summary":"fine"}],"edges":[],"walk_complete":true}'
        )
        self.assertEqual(decoded.nodes[0]["summary"], "fine")

    def test_an_undefined_edge_field_is_refused_too(self):
        with self.assertRaises(ContractError) as caught:
            decode_result(
                "collect_graph",
                '{"nodes":[],"edges":[{"from_id":"a","to_id":"b","type":"t","wieght":1}],"walk_complete":true}',
            )
        self.assertIn("wieght", str(caught.exception))


class Property09_NoStructuredContentIsRefused(unittest.TestCase):
    """(9) The arm a provider reaches by answering with prose instead of a
    structured result."""

    def test_it(self):
        with self.assertRaises(ContractError) as caught:
            decode_result("collect_graph", None)
        self.assertIn("no structuredContent", str(caught.exception))


class Property10_InputContractDeclaresContextAsOptional(unittest.TestCase):
    """(10) Both halves of the placement in one row: the block IS in the published
    schema a collector author reads, and it is NOT on the required list."""

    def test_it(self):
        schema = _input()
        self.assertCountEqual(
            schema["required"],
            ["id"],
            "the collect id is the ONLY required input property; an added required property refuses every existing provider",
        )
        props = schema["properties"]
        self.assertEqual(props["context"]["type"], "object")
        # THE CONTROL: the two properties that were there before are untouched, so
        # the assertion above is about an addition rather than a rewritten file.
        self.assertEqual(props["id"]["type"], "string")
        self.assertEqual(props["params"]["type"], "object")


class Property11_AdmitsAProviderLackingAnOptionalProperty(unittest.TestCase):
    """(11) The core pair: both properties the contract declares without requiring
    must be admitted when absent."""

    def test_advertised_schema_lacks_the_optional_context_property(self):
        advertised = _input()
        del advertised["properties"]["context"]
        check_tool_schemas("collect", advertised, _output())

    def test_advertised_schema_lacks_the_optional_params_property(self):
        advertised = _input()
        del advertised["properties"]["params"]
        check_tool_schemas("collect", advertised, _output())

    def test_control_the_contract_advertised_verbatim_passes(self):
        """THE SAME-RUN CONTROL, through the same instrument and the same path, so
        the two admissions above are not a gate that stopped checking."""
        check_tool_schemas("collect", _input(), _output())


class Property12_StillRefusesAMissingRequiredProperty(unittest.TestCase):
    """(12) The arm the widening must not take with it, and it names WHICH loop
    keeps it: the advertised required list still contains "id", so the
    required-list loop does not fire and the refusal comes from the PROPERTY loop
    reading the CONTRACT's required list."""

    def test_it(self):
        advertised = _input()
        del advertised["properties"]["id"]
        self.assertIn(
            "id",
            advertised["required"],
            "fixture control: the advertised required list still names id, so only the property is missing",
        )
        with self.assertRaises(ContractError) as caught:
            check_tool_schemas("collect", advertised, _output())
        self.assertIn('the property "id"', str(caught.exception))


class Property13_RefusesAnOptionalPropertyOfTheWrongType(unittest.TestCase):
    """(13) The optional arm must skip an ABSENT property, never a PRESENT wrong
    one. Without this row a gate that returned early on the whole property would
    pass a provider declaring context as a string."""

    def test_it(self):
        advertised = _input()
        advertised["properties"]["context"] = {"type": "string"}
        with self.assertRaises(ContractError) as caught:
            check_tool_schemas("collect", advertised, _output())
        self.assertIn("inputSchema.context", str(caught.exception))
        self.assertIn('requires type "object"', str(caught.exception))


class ComparatorReadsTheContractRatherThanItsArgument(unittest.TestCase):
    """A control the client's own suite gets from its embed and this one has to
    state: the comparator's expectations come from the checked-in files, so a
    caller that hands it a mutated contract-shaped document does not move them."""

    def test_a_mutated_copy_of_the_contract_does_not_move_the_gate(self):
        mutated = _output()
        mutated["required"] = []
        with self.assertRaises(ContractError):
            check_tool_schemas("collect", _input(), mutated)
        self.assertEqual(_output()["required"], ["nodes", "edges", "walk_complete"])

    def test_the_helper_returns_a_document_the_caller_may_mutate_freely(self):
        first = _output()
        first["properties"].clear()
        self.assertIn("walk_complete", _output()["properties"])
        self.assertNotEqual(copy.deepcopy(_output()), first)


if __name__ == "__main__":
    unittest.main()
