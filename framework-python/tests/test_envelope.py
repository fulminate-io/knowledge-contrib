# SPDX-License-Identifier: Apache-2.0

"""Rows 5, 6, 7 and 8 — the three wire-shaping rules, the four refusals, and the
two pass-through rules a collector must not "fix".

Row 5: empty walk -> [] and never null; incomplete walk -> walk_complete present
       and false; a walk that raises -> isError with text, never an empty
       successful envelope.
Row 6: the four refusals with no schema arm -- empty node type, unasserted
       completeness, incomplete with an empty reason, and the empty collect id.
Row 7: a dangling edge passes through untouched.
Row 8: an edge naming BOTH source_graph and target_graph is emitted as the author
       wrote it and refused by the CLIENT, not silently corrected here.
"""

import unittest

from knowledge_collector import (
    BehaviorDefaults,
    Collector,
    Complete,
    Completeness,
    Declaration,
    Edge,
    EnvelopeError,
    Incomplete,
    Node,
    Result,
    ToolSpec,
    encode_result,
    new_speaker,
)

from . import support


class _Walker(Collector):
    """A collector whose walk is supplied per test."""

    def __init__(self, walk_fn):
        self._walk = walk_fn

    def tool(self):
        return ToolSpec(name="collect")

    def params_schema(self):
        return {"type": "object"}

    def describe(self):
        return Declaration(behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True), node_types=["issue"], edge_types=["blocks"])

    def walk(self, collect_id, params, foreign):
        return self._walk(collect_id, params, foreign)


def _call(walk_fn, arguments=None):
    collector = _Walker(walk_fn)
    responses, _, _ = support.drive_in_process(
        lambda i, o, e: new_speaker(collector, i, o, e),
        [support.call_frame("collect", arguments if arguments is not None else {"id": "probe"})],
    )
    return support.result_of(responses, 3)


class WireShapingTest(unittest.TestCase):
    def test_row5a_an_empty_walk_emits_empty_arrays_and_never_null(self):
        """THE SUBJECT IS THE ENCODER'S NORMALIZATION, and reaching it takes a
        Result whose lists are None WHEN THE ENCODER SEES THEM. The constructor
        coerces None on the way in, so a Result built with None never exercises
        the encoder's arm -- measured: deleting the encoder's normalization left
        this row green until it was written this way. A collector that assigns
        result.nodes = None after building is the reachable path, and it is the one
        that would emit `null` into a contract that declares an array."""
        result = Result(nodes=[], edges=[], complete=Complete())
        result.nodes = None
        result.edges = None
        out = encode_result("collect", result)
        self.assertEqual(out, {"nodes": [], "edges": [], "walk_complete": True})
        self.assertIsInstance(out["nodes"], list)
        self.assertIsInstance(out["edges"], list)

    def test_row5a_the_constructor_coerces_none_too_so_both_arms_are_covered(self):
        """The second half: a walk that returns Result(None, None, ...) is the
        ordinary shape, and it is normalized before the encoder ever sees it."""
        result = Result(nodes=None, edges=None, complete=Complete())
        self.assertEqual(result.nodes, [])
        self.assertEqual(result.edges, [])
        self.assertEqual(encode_result("collect", result), {"nodes": [], "edges": [], "walk_complete": True})

    def test_row5b_an_incomplete_walk_emits_walk_complete_false_never_omitted(self):
        """THE TEST RIDES THE INCOMPLETE ARM DELIBERATELY: a walk asserting
        COMPLETE emits `true` and an omission defect would be invisible."""
        out = encode_result("collect", Result([], [], Incomplete("the source paged out")))
        self.assertIn("walk_complete", out)
        self.assertIs(out["walk_complete"], False)

    def test_row5c_a_walk_that_raises_is_an_isError_result_with_text(self):
        def boom(collect_id, params, foreign):
            raise RuntimeError("the source refused")

        result = _call(boom)
        self.assertIs(result["isError"], True)
        self.assertEqual(result["content"][0]["type"], "text")
        self.assertIn("the source refused", result["content"][0]["text"])
        self.assertNotIn("structuredContent", result, "a failed walk must never become an empty successful envelope")


class RefusalTest(unittest.TestCase):
    def test_row6a_a_node_with_an_empty_type_is_refused_naming_its_index_and_id(self):
        with self.assertRaises(EnvelopeError) as caught:
            encode_result("collect", Result([Node(id="n1", type="")], [], Complete()))
        self.assertIn("node[0]", str(caught.exception))
        self.assertIn("n1", str(caught.exception))

    def test_row6b_an_unasserted_completeness_is_refused(self):
        with self.assertRaises(EnvelopeError) as caught:
            encode_result("collect", Result([], [], Completeness()))
        self.assertIn("unasserted", str(caught.exception))
        self.assertIn("collect", str(caught.exception))

    def test_row6b_control_a_value_that_is_not_a_completeness_at_all_is_refused(self):
        with self.assertRaises(EnvelopeError) as caught:
            encode_result("collect", Result([], [], True))
        self.assertIn("not a Completeness", str(caught.exception))

    def test_row6c_an_incomplete_walk_with_an_empty_reason_is_refused(self):
        with self.assertRaises(EnvelopeError) as caught:
            encode_result("collect", Result([], [], Incomplete("")))
        self.assertIn("no reason", str(caught.exception))

    def test_row6c_control_an_incomplete_walk_with_a_reason_is_admitted(self):
        self.assertIs(encode_result("collect", Result([], [], Incomplete("why")))["walk_complete"], False)

    def test_row6d_an_empty_collect_id_is_refused_before_the_walk(self):
        reached = []

        def walk(collect_id, params, foreign):
            reached.append(collect_id)
            return Result([], [], Complete())

        result = _call(walk, {"id": ""})
        self.assertIs(result["isError"], True)
        self.assertIn("the collect id is empty", result["content"][0]["text"])
        self.assertEqual(reached, [], "the walk must not run for an empty collect id")

    def test_row6d_control_a_non_empty_collect_id_reaches_the_walk(self):
        reached = []

        def walk(collect_id, params, foreign):
            reached.append(collect_id)
            return Result([], [], Complete())

        _call(walk, {"id": "probe"})
        self.assertEqual(reached, ["probe"])


class PassThroughTest(unittest.TestCase):
    def test_row7_a_dangling_edge_passes_through_untouched(self):
        out = encode_result(
            "collect",
            Result([Node(id="n1", type="issue")], [Edge(from_id="n1", to_id="absent", type="blocks")], Complete()),
        )
        self.assertEqual(out["edges"], [{"from_id": "n1", "to_id": "absent", "type": "blocks"}])
        self.assertEqual([n["id"] for n in out["nodes"]], ["n1"])

    def test_row8_an_edge_naming_both_graph_families_is_emitted_as_written(self):
        """The client refuses it, by the contract's own words. The port must not
        silently drop one of the two: correcting a declaration the author meant
        would hide the mistake from the only gate that can name it."""
        out = encode_result(
            "collect",
            Result(
                [Node(id="n1", type="issue")],
                [Edge(from_id="n1", to_id="n2", type="blocks", source_graph="code", target_graph="logs")],
                Complete(),
            ),
        )
        self.assertEqual(out["edges"][0]["source_graph"], "code")
        self.assertEqual(out["edges"][0]["target_graph"], "logs")

    def test_an_edge_naming_neither_family_omits_both_keys(self):
        """The control that makes the row above about a CARRIED value rather than
        about a field that is always written."""
        out = encode_result("collect", Result([], [Edge(from_id="a", to_id="b", type="blocks")], Complete()))
        self.assertEqual(out["edges"][0], {"from_id": "a", "to_id": "b", "type": "blocks"})


class NodeWireShapeTest(unittest.TestCase):
    def test_id_and_type_are_unconditional_and_the_other_fourteen_are_omitted_when_empty(self):
        self.assertEqual(Node(id="n1", type="issue").to_wire(), {"id": "n1", "type": "issue"})

    def test_every_populated_optional_field_reaches_the_wire_under_its_contract_name(self):
        node = Node(
            id="n1",
            type="issue",
            symbol_name="s",
            file_path="f",
            language="go",
            start_line=1,
            end_line=2,
            content="c",
            signature="sig",
            summary="sum",
            description="d",
            source="src",
            status="open",
            keywords="k",
            is_exported=True,
            metadata={"a": "b"},
        )
        self.assertEqual(
            sorted(node.to_wire()),
            sorted(
                [
                    "id",
                    "type",
                    "symbol_name",
                    "file_path",
                    "language",
                    "start_line",
                    "end_line",
                    "content",
                    "signature",
                    "summary",
                    "description",
                    "source",
                    "status",
                    "keywords",
                    "is_exported",
                    "metadata",
                ]
            ),
        )

    def test_a_result_cannot_be_built_without_a_completeness_assertion(self):
        """The port's replacement for the Go type's absent zero value: an author
        who omits the decision gets a TypeError rather than a silent walk."""
        with self.assertRaises(TypeError):
            Result([], [])  # noqa: PLE1120 -- that is the point of the row


if __name__ == "__main__":
    unittest.main()
