# SPDX-License-Identifier: Apache-2.0

"""Rows 1, 2, 3 and 9 — what the served tool ADVERTISES, and who the server says
it is.

Row 1: the advertised output schema is the contract file byte-for-byte.
Row 2: the advertised input schema is the contract file with ONLY
       properties.params replaced -- asserted as three separate things, so a
       splice that also moved the required list turns a named row red.
Row 3: a params declaration that is not an object is refused, naming the type.
Row 9: serverInfo.name equals the resolved tool name.
"""

import json
import unittest

from knowledge_collector import (
    BehaviorDefaults,
    Collector,
    Complete,
    Declaration,
    Result,
    SchemaDeclarationError,
    ToolSpec,
    advertised_input_schema,
    advertised_output_schema,
    input_contract_json,
    new_speaker,
    output_contract_json,
)

from . import support


class FixtureCollector(Collector):
    """A minimal collector whose params schema is the subject of these rows."""

    def __init__(self, params_schema=None, tool_name="", description=""):
        self._params_schema = params_schema if params_schema is not None else {
            "type": "object",
            "properties": {"project": {"type": "string"}},
            "required": ["project"],
        }
        self._tool_name = tool_name
        self._description = description

    def tool(self):
        return ToolSpec(name=self._tool_name, description=self._description)

    def params_schema(self):
        return self._params_schema

    def describe(self):
        return Declaration(
            behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True),
            node_types=["issue"],
            edge_types=["blocks"],
        )

    def walk(self, collect_id, params, foreign):
        return Result(nodes=[], edges=[], complete=Complete())


class AdvertisedSchemaTest(unittest.TestCase):
    def test_row1_the_advertised_output_schema_is_the_contract_file(self):
        """The bytes the server puts on the wire, compared against the checked-in
        file read from disk, never against a transcription."""
        collector = FixtureCollector()
        responses, _, _ = support.drive_in_process(
            lambda i, o, e: new_speaker(collector, i, o, e),
            [support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})],
        )
        listing = support.result_of(responses, 2)
        collect = _tool_named(listing, "collect")
        self.assertEqual(collect["outputSchema"], json.loads(output_contract_json()))

    def test_row2a_the_top_level_required_list_is_exactly_the_collect_id(self):
        advertised = advertised_input_schema(FixtureCollector())
        self.assertEqual(advertised["required"], ["id"])

    def test_row2b_id_and_context_are_byte_equal_to_the_contract_files(self):
        advertised = advertised_input_schema(FixtureCollector())
        contract = json.loads(input_contract_json())
        for name in ("id", "context"):
            with self.subTest(name):
                self.assertEqual(advertised["properties"][name], contract["properties"][name])

    def test_row2c_params_is_the_collectors_own_schema(self):
        declared = {"type": "object", "properties": {"region": {"type": "string"}}}
        advertised = advertised_input_schema(FixtureCollector(params_schema=declared))
        self.assertEqual(advertised["properties"]["params"], declared)
        self.assertNotEqual(
            advertised["properties"]["params"],
            json.loads(input_contract_json())["properties"]["params"],
            "the advertised input schema still carries the contract's placeholder params sub-schema",
        )

    def test_row2d_nothing_else_of_the_contract_document_moved(self):
        """The control that makes the three assertions above about a SPLICE rather
        than about a rewritten document."""
        advertised = advertised_input_schema(FixtureCollector())
        contract = json.loads(input_contract_json())
        advertised_without_params = dict(advertised)
        advertised_without_params["properties"] = {
            k: v for k, v in advertised["properties"].items() if k != "params"
        }
        contract_without_params = dict(contract)
        contract_without_params["properties"] = {k: v for k, v in contract["properties"].items() if k != "params"}
        self.assertEqual(advertised_without_params, contract_without_params)

    def test_row3_a_params_declaration_that_is_not_an_object_is_refused_naming_the_type(self):
        for declared, spelled in (
            ({"type": "string"}, "'string'"),
            ({"type": "array", "items": {"type": "string"}}, "'array'"),
            ({}, "None"),
        ):
            with self.subTest(declared):
                with self.assertRaises(SchemaDeclarationError) as caught:
                    advertised_input_schema(FixtureCollector(params_schema=declared))
                self.assertIn(spelled, str(caught.exception))
                self.assertIn("object", str(caught.exception))

    def test_row3b_a_params_declaration_that_is_not_a_document_is_refused_naming_what_it_is(self):
        with self.assertRaises(SchemaDeclarationError) as caught:
            advertised_input_schema(FixtureCollector(params_schema="an object please"))
        self.assertIn("str", str(caught.exception))

    def test_row9_server_info_name_equals_the_resolved_tool_name(self):
        for tool_name, want in (("", "collect"), ("collect_logs", "collect_logs")):
            with self.subTest(tool_name):
                collector = FixtureCollector(tool_name=tool_name)
                responses, _, _ = support.drive_in_process(
                    lambda i, o, e, c=collector: new_speaker(c, i, o, e), [support.initialize_frame()]
                )
                self.assertEqual(support.result_of(responses, 1)["serverInfo"]["name"], want)

    def test_the_advertised_output_schema_helper_returns_a_fresh_document(self):
        """A caller that mutates what it was handed must not reach this package's
        own copy."""
        first = advertised_output_schema()
        first["required"] = ["nothing"]
        self.assertEqual(advertised_output_schema()["required"], ["nodes", "edges", "walk_complete"])


def _tool_named(listing, name):
    for tool in listing["tools"]:
        if tool["name"] == name:
            return tool
    raise AssertionError("the listing carries no tool named %r: %r" % (name, [t["name"] for t in listing["tools"]]))


if __name__ == "__main__":
    unittest.main()
