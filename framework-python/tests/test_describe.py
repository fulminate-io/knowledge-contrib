# SPDX-License-Identifier: Apache-2.0

"""Rows 11, 11a and 11b — the describe tool.

Row 11:  it is served under a fixed name; its result validates against the
         describe contract schema; and the two declaration refusals fire by name.
Row 11a: THE FIELD SET, PART BY PART. Schema validity is not field-set coverage: a
         schema admits a document that omits every optional part, so row 11 alone
         passes on a declaration that declares nothing. Six parts, one assertion
         each, against a declaration that populates all six.

         THE PART NAMES ARE THE CONTRACT FILE'S, not this port's: behavior,
         node_type_overrides, node_types, edge_types, environment and context.
         An earlier draft of this port named the vocabularies node_vocabulary and
         edge_vocabulary and put the overrides under node_types, which is the
         shape the client's frozen schema does not carry; the rows below assert
         the frozen spellings so the drift cannot come back.
Row 11b: the sample collector declares its own vocabulary, and the vocabulary it
         declares EQUALS the node and edge types its walk actually emits --
         computed from the walk's output in the same test, never a hand-written
         list beside it.

Row 11c, `knowledge collector add` filling the entry from the describe result, is
a pending pin and lives in test_pending_pins.py: the client's fill path is the
describe-tool ticket's, not this port's.
"""

import json
import os
import sys
import unittest

from knowledge_collector import (
    BehaviorDefaults,
    Collector,
    Complete,
    DESCRIBE_TOOL_NAME,
    Declaration,
    DeclarationError,
    ENV_CLASSES,
    EnvName,
    FamilyDeclaration,
    NodeTypeBehavior,
    Result,
    ToolSpec,
    describe_contract_json,
    new_speaker,
)
from knowledge_collector.jsonschema import validate

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import sample_collector  # noqa: E402

from . import support  # noqa: E402


def _declaration_with(**overrides):
    """A minimal renderable declaration with one part replaced, for the refusal
    rows that are about ONE part and would otherwise carry a whole fixture."""
    fields = {
        "behavior": BehaviorDefaults(summarizable=False, embeddable=False, syncable=False),
        "node_types": ["issue"],
        "edge_types": ["blocks"],
        "environment": [],
    }
    fields.update(overrides)
    return Declaration(**fields)


def _full_declaration():
    """A declaration populating all SIX parts the requirement names."""
    return Declaration(
        behavior=BehaviorDefaults(
            summarizable=True,
            embeddable=False,
            syncable=True,
            embed_fields=["content"],
            summarize_fields=["content", "description"],
            bm25_fields=["symbol_name"],
        ),
        node_type_overrides={"issue": NodeTypeBehavior(summarizable=False, bm25_fields=["summary"])},
        node_types=["issue"],
        edge_types=["blocks"],
        environment=[
            EnvName("ACME_TOKEN", "secret", "the API token"),
            EnvName("ACME_ROOT", "path"),
            EnvName("ACME_PROJECT", "selector"),
            EnvName("ACME_PROXY", "not-carried", "read when the host sets it; no installed entry declares it"),
        ],
        context=[
            FamilyDeclaration(
                "code",
                node_types=["function"],
                node_fields=["id", "file_path"],
                metadata_keys=["package"],
                edge_fields=["from_id", "to_id"],
                reason="correlating issues with the functions they name",
            )
        ],
    )


class _Fixture(Collector):
    def __init__(self, declaration=None, tool_name="collect"):
        self._declaration = declaration if declaration is not None else _full_declaration()
        self._tool_name = tool_name

    def tool(self):
        return ToolSpec(name=self._tool_name)

    def params_schema(self):
        return {"type": "object"}

    def describe(self):
        return self._declaration

    def walk(self, collect_id, params, foreign):
        return Result([], [], Complete())


def _describe(collector):
    responses, _, _ = support.drive_in_process(
        lambda i, o, e: new_speaker(collector, i, o, e), [support.call_frame(DESCRIBE_TOOL_NAME, {})]
    )
    return support.result_of(responses, 3)


class DescribeToolTest(unittest.TestCase):
    def test_row11a_the_describe_tool_is_served_under_a_fixed_name(self):
        collector = _Fixture()
        responses, _, _ = support.drive_in_process(
            lambda i, o, e: new_speaker(collector, i, o, e),
            [support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})],
        )
        names = [t["name"] for t in support.result_of(responses, 2)["tools"]]
        self.assertIn(DESCRIBE_TOOL_NAME, names)
        self.assertEqual(DESCRIBE_TOOL_NAME, "describe")

    def test_row11b_the_result_validates_against_the_describe_contract_schema(self):
        document = _describe(_Fixture())["structuredContent"]
        validate(document, json.loads(describe_contract_json()))

    def test_row11c_the_describe_tool_advertises_that_schema_as_its_output_schema(self):
        collector = _Fixture()
        responses, _, _ = support.drive_in_process(
            lambda i, o, e: new_speaker(collector, i, o, e),
            [support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})],
        )
        tool = [t for t in support.result_of(responses, 2)["tools"] if t["name"] == DESCRIBE_TOOL_NAME][0]
        self.assertEqual(tool["outputSchema"], json.loads(describe_contract_json()))


class DeclarationFieldSetTest(unittest.TestCase):
    """Row 11a. One assertion per named part, so a part silently dropped from the
    renderer is a named red rather than a still-valid document."""

    def setUp(self):
        self.document = _describe(_Fixture())["structuredContent"]

    def test_part1_the_behaviour_defaults(self):
        behavior = self.document["behavior"]
        self.assertIs(behavior["summarizable"], True)
        self.assertIs(behavior["embeddable"], False)
        self.assertIs(behavior["syncable"], True)

    def test_part2_the_embed_summary_and_bm25_field_lists(self):
        behavior = self.document["behavior"]
        self.assertEqual(behavior["embed_fields"], ["content"])
        self.assertEqual(behavior["summarize_fields"], ["content", "description"])
        self.assertEqual(behavior["bm25_fields"], ["symbol_name"])

    def test_part3_the_per_node_type_overrides(self):
        self.assertEqual(
            self.document["node_type_overrides"], {"issue": {"summarizable": False, "bm25_fields": ["summary"]}}
        )

    def test_part4_the_environment_names_with_their_class(self):
        env = {entry["name"]: entry["class"] for entry in self.document["environment"]}
        self.assertEqual(
            env,
            {
                "ACME_TOKEN": "secret",
                "ACME_ROOT": "path",
                "ACME_PROJECT": "selector",
                "ACME_PROXY": "not-carried",
            },
        )
        # EVERY CLASS THE VOCABULARY CARRIES IS EXERCISED, which is what keeps a
        # class added to ENV_CLASSES from arriving with no fixture behind it.
        self.assertEqual(sorted(set(env.values())), sorted(ENV_CLASSES))

    def test_part4b_a_declaration_carries_environment_NAMES_and_never_a_value(self):
        for entry in self.document["environment"]:
            self.assertEqual(sorted(entry), sorted(k for k in ("name", "class", "description") if k in entry))
            self.assertNotIn("value", entry)

    def test_part5_the_foreign_context_declaration(self):
        # KEYED BY FAMILY, and the family name is the key rather than a member of
        # the value: that is the shape the registration entry's own `context` key
        # carries, and the client's loader refuses an unknown member.
        self.assertEqual(sorted(self.document["context"]), ["code"])
        family = self.document["context"]["code"]
        self.assertNotIn("family", family)
        self.assertEqual(family["node_types"], ["function"])
        self.assertEqual(family["node_fields"], ["id", "file_path"])
        self.assertEqual(family["metadata_keys"], ["package"])
        self.assertEqual(family["edge_fields"], ["from_id", "to_id"])

    def test_part6_the_node_and_edge_vocabulary(self):
        self.assertEqual(self.document["node_types"], ["issue"])
        self.assertEqual(self.document["edge_types"], ["blocks"])

    def test_the_rendered_declaration_never_emits_reason(self):
        """The client's config loader decodes the entry's foreign-context object
        with unknown fields refused, so a rendered `reason` is refused by name at
        load. The author's reason stays on the declaration and out of the wire."""
        self.assertNotIn("reason", json.dumps(self.document))
        self.assertEqual(_full_declaration().context[0].reason, "correlating issues with the functions they name")


class DeclarationRefusalTest(unittest.TestCase):
    def test_a_per_node_type_override_outside_the_vocabulary_is_refused_by_name(self):
        declaration = _full_declaration()
        declaration.node_type_overrides["ticket"] = NodeTypeBehavior(embeddable=True)
        with self.assertRaises(DeclarationError) as caught:
            declaration.render()
        self.assertIn("ticket", str(caught.exception))
        self.assertIn("node vocabulary", str(caught.exception))

    def test_control_an_override_inside_the_vocabulary_renders(self):
        declaration = _full_declaration()
        declaration.node_types.append("ticket")
        declaration.node_type_overrides["ticket"] = NodeTypeBehavior(embeddable=True)
        self.assertIn("ticket", declaration.render()["node_type_overrides"])

    def test_the_family_selector_beside_a_non_empty_node_type_list_is_refused_by_name(self):
        declaration = _full_declaration()
        declaration.context[0].all_node_types = True
        with self.assertRaises(DeclarationError) as caught:
            declaration.render()
        self.assertIn("all_node_types", str(caught.exception))
        self.assertIn("code", str(caught.exception))

    def test_control_the_family_selector_alone_renders_and_the_empty_list_keeps_its_meaning(self):
        declaration = _full_declaration()
        declaration.context[0].node_types = []
        declaration.context[0].all_node_types = True
        family = declaration.render()["context"]["code"]
        self.assertIs(family["all_node_types"], True)
        self.assertNotIn("node_types", family)

        # AN EMPTY node_types WITH NO SELECTOR keeps its deliberate meaning: the
        # graph names alone and no nodes. It is not the selector by another name.
        declaration.context[0].all_node_types = False
        family = declaration.render()["context"]["code"]
        self.assertNotIn("all_node_types", family)
        self.assertNotIn("node_types", family)

    def test_an_environment_class_outside_the_vocabulary_is_refused_naming_it(self):
        declaration = _full_declaration()
        declaration.environment.append(EnvName("ACME_OTHER", "credential"))
        with self.assertRaises(DeclarationError) as caught:
            declaration.render()
        self.assertIn("credential", str(caught.exception))
        self.assertIn("secret", str(caught.exception))

    def test_an_environment_name_the_installer_would_refuse_is_refused_here(self):
        """THE CONTRACT FILE DESCRIBES THE NAME SHAPE IN PROSE AND CARRIES NO
        PATTERN, so the schema admits a malformed name and the installer's own
        consumption loop is what refuses it, in a run the collector author never
        sees. The renderer refuses it where the author is standing."""
        declaration = _full_declaration()
        declaration.environment.append(EnvName("ACME-TOKEN", "secret"))
        with self.assertRaises(DeclarationError) as caught:
            declaration.render()
        self.assertIn("ACME-TOKEN", str(caught.exception))

    def test_a_declaration_that_renders_but_violates_the_schema_is_refused_by_the_served_tool(self):
        """THE SERVED TOOL VALIDATES ITS OWN RESULT, and this is the row that
        observes it. render() checks what it can check about a declaration's
        internal consistency; the schema checks the shapes render() has no reason
        to know about -- here an optional description carrying a number where the
        contract declares a string. Without this row the validation call in the
        describe handler could be deleted and nothing would notice."""
        declaration = _full_declaration()
        declaration.environment.append(EnvName("ACME_EXTRA", "selector", 7))
        # It RENDERS: the name and the class are both legal, so the declaration's
        # own checks pass and the number goes out as it was written.
        self.assertIn("7", json.dumps(declaration.render()))
        # And it is refused when served, because the schema types the field.
        result = _describe(_Fixture(declaration=declaration))
        self.assertIs(result["isError"], True)
        self.assertIn("describe contract schema", result["content"][0]["text"])

    def test_a_declaration_carrying_no_behavior_is_refused(self):
        """The schema REQUIRES behavior with its three axes set, so a declaration
        that leaves the block out is refused where the author can see it rather
        than by a schema message about a missing property."""
        with self.assertRaises(DeclarationError) as caught:
            Declaration(node_types=[], edge_types=[], environment=[]).render()
        self.assertIn("behavior", str(caught.exception))

    def test_a_behavior_leaving_one_axis_unset_is_refused_naming_the_axis(self):
        with self.assertRaises(DeclarationError) as caught:
            Declaration(
                behavior=BehaviorDefaults(summarizable=True, embeddable=False),
                node_types=[],
                edge_types=[],
                environment=[],
            ).render()
        self.assertIn("syncable", str(caught.exception))

    def test_an_empty_type_name_and_a_duplicate_are_refused_by_name(self):
        with self.assertRaises(DeclarationError) as caught:
            _declaration_with(node_types=["issue", ""]).render()
        self.assertIn("empty type name", str(caught.exception))
        with self.assertRaises(DeclarationError) as caught:
            _declaration_with(edge_types=["blocks", "blocks"]).render()
        self.assertIn("twice", str(caught.exception))

    def test_one_environment_name_declared_twice_is_refused(self):
        declaration = _full_declaration()
        declaration.environment.append(EnvName("ACME_ROOT", "selector"))
        with self.assertRaises(DeclarationError) as caught:
            declaration.render()
        self.assertIn("ACME_ROOT", str(caught.exception))

    def test_a_family_declaration_naming_no_family_is_refused(self):
        declaration = _full_declaration()
        declaration.context[0].family = ""
        with self.assertRaises(DeclarationError):
            declaration.render()

    def test_the_declaration_names_no_collect_tool_at_all(self):
        """THE ABSENCE IS THE CONTRACT'S. The tool name is the ToolSpec's and the
        listing's; a second spelling inside the declaration would be a value the
        client would have to reconcile against the tool it just listed, so the
        frozen schema carries no such key and this row says so."""
        document = _describe(_Fixture())["structuredContent"]
        self.assertNotIn("tool", document)
        self.assertNotIn("description", document)

    def test_a_collector_whose_describe_returns_nothing_is_refused(self):
        """The declaration carries the vocabulary, and an empty one is a CLAIM
        rather than an absence, so the framework must not fill one in for an
        author who simply had not written it."""

        class _Silent(_Fixture):
            def describe(self):
                return None

        result = _describe(_Silent())
        self.assertIs(result["isError"], True)
        self.assertIn("describe()", result["content"][0]["text"])

    def test_a_collector_that_does_not_implement_describe_at_all_is_refused(self):
        class _Unimplemented(Collector):
            def tool(self):
                return ToolSpec(name="collect")

            def params_schema(self):
                return {"type": "object"}

            def walk(self, collect_id, params, foreign):
                return Result([], [], Complete())

        result = _describe(_Unimplemented())
        self.assertIs(result["isError"], True)
        self.assertIn("describe()", result["content"][0]["text"])


class TriStateTest(unittest.TestCase):
    def test_an_unset_boolean_is_absent_so_the_cascade_inherits_it(self):
        rendered = BehaviorDefaults(summarizable=True).render()
        self.assertEqual(rendered, {"summarizable": True})
        self.assertNotIn("embeddable", rendered)

    def test_false_is_pinned_rather_than_dropped(self):
        self.assertEqual(BehaviorDefaults(embeddable=False).render(), {"embeddable": False})

    def test_a_behaviour_block_that_pins_nothing_is_refused_rather_than_omitted(self):
        """THIS ROW INVERTED WITH THE FROZEN SCHEMA and the inversion is the point.
        An earlier draft omitted an empty behavior block entirely; the contract
        requires it with all three axes set, so an author who pins nothing is a
        collector that never said rather than one that inherits."""
        with self.assertRaises(DeclarationError):
            Declaration(behavior=BehaviorDefaults(), node_types=[], edge_types=[], environment=[]).render()


class SampleCollectorVocabularyTest(unittest.TestCase):
    """Row 11b. The vocabulary the sample DECLARES equals the types its walk
    EMITS, computed from the walk's own output in this test rather than compared
    against a second hand-written list."""

    def test_the_declared_vocabulary_equals_what_the_walk_emits(self):
        collector = sample_collector.DirectoryCollector()
        result = collector.walk("probe", {"path": support.PACKAGE_ROOT}, None)
        emitted_nodes = sorted({node.type for node in result.nodes})
        emitted_edges = sorted({edge.type for edge in result.edges})
        declared = collector.describe().render()
        self.assertEqual(sorted(declared["node_types"]), emitted_nodes)
        self.assertEqual(sorted(declared["edge_types"]), emitted_edges)

    def test_the_control_the_walk_emitted_more_than_nothing(self):
        collector = sample_collector.DirectoryCollector()
        result = collector.walk("probe", {"path": support.PACKAGE_ROOT}, None)
        self.assertGreater(len(result.nodes), 1)
        self.assertGreater(len(result.edges), 0)

    def test_the_samples_declaration_validates_against_the_describe_schema(self):
        validate(sample_collector.DirectoryCollector().describe().render(), json.loads(describe_contract_json()))


if __name__ == "__main__":
    unittest.main()
