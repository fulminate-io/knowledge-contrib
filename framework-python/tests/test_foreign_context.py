# SPDX-License-Identifier: Apache-2.0

"""The DECLARED FOREIGN-GRAPH CONTEXT, field by field.

WHY THIS FILE EXISTS AND WHAT IT REPLACES. The block reached a walk through one
test that asserted a single node id, and eight of the ten fields the contract
names travelled through code no row observed: dropping symbol_name, file_path,
content, metadata, graph_name, every edge, the sorted family order, both bad-input
refusals and the whole excluding() helper all left the suite green. R1 names the
foreign context as part of the author surface this port must provide, and a
surface nothing observes is a surface that has not been built.

THE CONTRACT IS THE AUTHORITY FOR THE FIELD SET, not this file's opinion of it.
The client's checked-in input schema describes the block: each key holds an array
of {graph_name, nodes[], edges[]}; a node carries the declared subset of
{id, type, symbol_name, file_path, content, metadata} and an edge the declared
subset of {from_id, to_id}. One row reads that description out of the schema and
asserts this port's decoder carries every field it names, so a field added on the
client side reds here rather than arriving as a silent zero in someone's walk.

THE SORT IS LOAD-BEARING, by the module's own words: a collector that walks
families and emits edges from them would otherwise emit them in whatever order the
wire happened to carry, which makes one unchanged input produce a different result
each run. That is the byte-idempotence property R3 asks for. It cannot be observed
on a fixture carrying one family, so every ordering row here carries three whose
wire order is not sorted order.

THE TWO REFUSALS ARE REACHABLE, not dead. The contract declares context as a bare
object with no inner constraints, so the params gate admits a block whose family
is a string and it reaches the decoder, whose refusal becomes the tool error. That
is the repository's bad-input invariant applied to the block.
"""

import json
import unittest

from knowledge_collector import (
    BehaviorDefaults,
    Collector,
    Complete,
    Declaration,
    FAMILY_CODE,
    ForeignContext,
    Result,
    ToolSpec,
    input_contract_json,
    new_speaker,
)

from . import support


# THE WIRE ORDER IS NOT SORTED ORDER, deliberately: "logs" before "code" before
# "acme-aws" is what makes an ordering assertion able to fail.
_THREE_FAMILIES = {
    "logs": [{"graph_name": "prod", "nodes": [], "edges": []}],
    "code": [{"graph_name": "knowledge", "nodes": [], "edges": []}],
    "acme-aws": [{"graph_name": "acct", "nodes": [], "edges": []}],
}

# One node carrying EVERY field the contract names for a node, and one edge
# carrying both endpoints.
_FULL_GRAPH = {
    "graph_name": "knowledge",
    "nodes": [
        {
            "id": "pkg/thing.go:Thing.Method",
            "type": "method",
            "symbol_name": "Method",
            "file_path": "pkg/thing.go",
            "content": "func (t Thing) Method() {}",
            "metadata": {"package": "thing", "receiver": "Thing"},
        }
    ],
    "edges": [{"from_id": "pkg/thing.go:Thing.Method", "to_id": "pkg/thing.go:Thing"}],
}


class _Walker(Collector):
    """A collector that records the block its walk received."""

    def __init__(self):
        self.blocks = []

    def tool(self):
        return ToolSpec(name="collect")

    def params_schema(self):
        return {"type": "object"}

    def describe(self):
        return Declaration(behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True), node_types=[], edge_types=[])

    def walk(self, collect_id, params, foreign):
        self.blocks.append(foreign)
        return Result([], [], Complete())


def _through_the_wire(context):
    """Drive one collect carrying this context block and return (block, result).

    IT GOES THROUGH THE SPEAKER rather than calling the decoder directly, because
    the question every row here asks is what reaches A WALK -- the params gate, the
    decode and the handler are all on that path.
    """
    collector = _Walker()
    responses, _, _ = support.drive_in_process(
        lambda i, o, e: new_speaker(collector, i, o, e),
        [support.call_frame("collect", {"id": "probe", "context": context})],
    )
    result = support.result_of(responses, 3)
    return (collector.blocks[0] if collector.blocks else None), result


class NodeFieldTest(unittest.TestCase):
    """Every field of a foreign node reaches the walk under its contract name."""

    def setUp(self):
        block, result = _through_the_wire({FAMILY_CODE: [_FULL_GRAPH]})
        self.assertIsNotNone(block, "the walk was never reached: %r" % (result,))
        self.node = block.graphs(FAMILY_CODE)[0].nodes[0]

    def test_id(self):
        self.assertEqual(self.node.id, "pkg/thing.go:Thing.Method")

    def test_type(self):
        self.assertEqual(self.node.type, "method")

    def test_symbol_name(self):
        self.assertEqual(self.node.symbol_name, "Method")

    def test_file_path(self):
        self.assertEqual(self.node.file_path, "pkg/thing.go")

    def test_content(self):
        self.assertEqual(self.node.content, "func (t Thing) Method() {}")

    def test_metadata(self):
        self.assertEqual(self.node.metadata, {"package": "thing", "receiver": "Thing"})

    def test_a_field_the_entry_did_not_declare_arrives_as_its_empty_value(self):
        """The boundary column: absent is not an error, it is 'never sent'."""
        block, _ = _through_the_wire({FAMILY_CODE: [{"graph_name": "g", "nodes": [{"id": "a"}], "edges": []}]})
        node = block.graphs(FAMILY_CODE)[0].nodes[0]
        self.assertEqual(node.id, "a")
        self.assertEqual(node.type, "")
        self.assertEqual(node.symbol_name, "")
        self.assertEqual(node.file_path, "")
        self.assertEqual(node.content, "")
        self.assertEqual(node.metadata, {})


class GraphAndEdgeFieldTest(unittest.TestCase):
    def setUp(self):
        block, result = _through_the_wire({FAMILY_CODE: [_FULL_GRAPH]})
        self.assertIsNotNone(block, "the walk was never reached: %r" % (result,))
        self.graph = block.graphs(FAMILY_CODE)[0]

    def test_graph_name_reaches_the_walk(self):
        """It is always carried, even where the declaration asks for no nodes: a
        predicate that is a membership test over graph names needs the names and
        nothing else."""
        self.assertEqual(self.graph.graph_name, "knowledge")

    def test_the_edges_reach_the_walk_with_both_endpoints(self):
        self.assertEqual(len(self.graph.edges), 1)
        self.assertEqual(self.graph.edges[0].from_id, "pkg/thing.go:Thing.Method")
        self.assertEqual(self.graph.edges[0].to_id, "pkg/thing.go:Thing")

    def test_a_graph_asking_for_names_only_carries_its_name_and_no_nodes(self):
        block, _ = _through_the_wire({FAMILY_CODE: [{"graph_name": "names-only"}]})
        graph = block.graphs(FAMILY_CODE)[0]
        self.assertEqual(graph.graph_name, "names-only")
        self.assertEqual(graph.nodes, [])
        self.assertEqual(graph.edges, [])


class ContractFieldSetTest(unittest.TestCase):
    """The field set is the CONTRACT's, read out of the checked-in schema rather
    than transcribed here. A field the client adds reds this row instead of
    arriving as a silent zero in a walk."""

    def _context_description(self):
        return json.loads(input_contract_json())["properties"]["context"]["description"]

    def test_every_node_field_the_contract_names_is_carried_by_the_decoder(self):
        from knowledge_collector.context import ForeignNode

        description = self._context_description()
        declared = [f for f in ("id", "type", "symbol_name", "file_path", "content", "metadata") if f in description]
        self.assertEqual(len(declared), 6, "the control: the contract's description names all six")
        for field in declared:
            with self.subTest(field):
                self.assertIn(field, ForeignNode.__slots__)

    def test_every_edge_field_the_contract_names_is_carried_by_the_decoder(self):
        from knowledge_collector.context import ForeignEdge

        description = self._context_description()
        for field in ("from_id", "to_id"):
            with self.subTest(field):
                self.assertIn(field, description, "the control: the contract names it")
                self.assertIn(field, ForeignEdge.__slots__)

    def test_the_graph_shape_the_contract_names_is_the_decoders(self):
        from knowledge_collector.context import ForeignGraph

        description = self._context_description()
        self.assertIn("{graph_name, nodes[], edges[]}", description)
        self.assertEqual(sorted(ForeignGraph.__slots__), ["edges", "graph_name", "nodes"])

    def test_family_code_is_the_contracts_own_spelling(self):
        self.assertEqual(FAMILY_CODE, "code")
        self.assertIn("`code`", self._context_description())


class OrderingTest(unittest.TestCase):
    """THREE FAMILIES, whose wire order is not sorted order. A sort cannot be
    observed on a fixture carrying one."""

    def setUp(self):
        self.block, _ = _through_the_wire(_THREE_FAMILIES)
        self.assertIsNotNone(self.block)

    def test_the_fixture_control_the_wire_order_is_not_the_sorted_order(self):
        self.assertNotEqual(list(_THREE_FAMILIES), sorted(_THREE_FAMILIES))

    def test_families_comes_back_sorted(self):
        self.assertEqual(self.block.families(), ["acme-aws", "code", "logs"])

    def test_excluding_returns_the_rest_in_sorted_family_order(self):
        graphs = self.block.excluding("code")
        self.assertEqual([g.graph_name for g in graphs], ["acct", "prod"])

    def test_excluding_nothing_returns_every_family_flattened_in_sorted_order(self):
        # acme-aws -> acct, code -> knowledge, logs -> prod.
        self.assertEqual([g.graph_name for g in self.block.excluding()], ["acct", "knowledge", "prod"])

    def test_excluding_every_family_returns_nothing(self):
        self.assertEqual(self.block.excluding("acme-aws", "code", "logs"), [])

    def test_excluding_a_family_that_was_never_declared_changes_nothing(self):
        self.assertEqual(
            [g.graph_name for g in self.block.excluding("never-declared")],
            [g.graph_name for g in self.block.excluding()],
        )


class DeclaredVersusAbsentTest(unittest.TestCase):
    """A declared family that matched no graph and an undeclared one are DIFFERENT
    FACTS, and a collector is entitled to distinguish them."""

    def test_a_declared_but_empty_family_is_declared_and_carries_no_graphs(self):
        block, _ = _through_the_wire({"code": [], "logs": [{"graph_name": "prod"}]})
        self.assertTrue(block.declared("code"))
        self.assertEqual(block.graphs("code"), [])
        self.assertFalse(block.declared("never-asked"))
        self.assertEqual(block.graphs("never-asked"), [])

    def test_a_declared_but_null_family_is_declared_and_carries_no_graphs(self):
        """null is the wire's second spelling of an empty declaration, and it is a
        declaration rather than a silent drop."""
        block, _ = _through_the_wire({"code": None, "logs": [{"graph_name": "prod"}]})
        self.assertTrue(block.declared("code"))
        self.assertEqual(block.graphs("code"), [])
        self.assertEqual(block.families(), ["code", "logs"])

    def test_a_collector_whose_entry_declares_nothing_receives_the_empty_block(self):
        block, _ = _through_the_wire({})
        self.assertTrue(block.is_empty())
        self.assertEqual(block.families(), [])

    def test_the_control_a_block_carrying_a_family_is_not_empty(self):
        block, _ = _through_the_wire({"code": []})
        self.assertFalse(block.is_empty())


class RefusalTest(unittest.TestCase):
    """Both refusals are reachable OVER THE WIRE: the contract declares context as
    a bare object with no inner constraints, so the params gate admits these and
    the decoder is what refuses them."""

    def test_a_block_that_is_not_an_object_is_refused_by_the_params_gate(self):
        """THE LAYER IS NAMED because it is not the one a reader would guess. The
        contract declares `context` as an object, so a block that is an ARRAY is
        refused by the params gate before the decoder is reached, and the message
        names the path rather than the decoder. The decoder's own arm for the same
        shape is the row below, driven directly."""
        block, result = _through_the_wire(["code"])
        self.assertIsNone(block, "the walk must not run on a block that is not an object")
        self.assertIs(result["isError"], True)
        self.assertIn("context", result["content"][0]["text"])

    def test_the_decoders_own_non_object_arm_is_driven_directly(self):
        """The gate above masks this one over the wire, so it is driven at the
        API a collector author can also reach. Without this row the decoder's
        refusal could be deleted and the suite would stay green on the gate's
        message alone."""
        for bad in (["code"], "code", 7):
            with self.subTest(bad):
                with self.assertRaises(ValueError) as caught:
                    ForeignContext.from_wire(bad)
                self.assertIn("context block", str(caught.exception))
                self.assertIn(type(bad).__name__, str(caught.exception))

    def test_a_family_whose_value_is_not_an_array_is_refused_naming_the_family(self):
        block, result = _through_the_wire({"code": "not an array"})
        self.assertIsNone(block)
        self.assertIs(result["isError"], True)
        self.assertIn("'code'", result["content"][0]["text"])
        self.assertIn("str", result["content"][0]["text"])

    def test_the_control_a_well_formed_block_reaches_the_walk(self):
        block, result = _through_the_wire({FAMILY_CODE: [_FULL_GRAPH]})
        self.assertIsNotNone(block)
        self.assertNotIn("isError", result)

    def test_the_refusal_is_raised_by_the_decoder_itself(self):
        with self.assertRaises(ValueError) as caught:
            ForeignContext.from_wire({"code": 7})
        self.assertIn("code", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
