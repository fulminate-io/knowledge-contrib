# SPDX-License-Identifier: Apache-2.0

"""Row 10 — THE PARAMS VALIDATION IS THIS PORT'S OWN AND IT IS LOAD-BEARING.

The settlement that shaped this ticket credited the MCP SDK with validating call
arguments against the advertised input schema before the handler. Measured, the
Python low-level server does not: a handler advertising the contract input
document verbatim was reached with arguments carrying no `id` at all, and with
`params` declared an object and sent as a string. So nothing else in the system
catches an absent validation here until a collector author debugs a walk on
garbage.

THE MUTATION THAT MUST TURN A NAMED TEST RED: delete the validate() call in
serve.py's collect handler and
`test_row10a_a_call_omitting_the_collect_id_never_reaches_the_walk` goes red.

THE OUTPUT HALF IS DELIBERATELY NOT HERE. The client validates every result it
receives, so a pre-emit output validator in the server would duplicate a check the
consumer already performs -- and would then have to be bypassable for a conformance
stub that must be able to break its own word.
"""

import unittest

from knowledge_collector import BehaviorDefaults, Collector, Complete, Declaration, Result, ToolSpec, new_speaker

from . import support


class _RecordingCollector(Collector):
    def __init__(self, params_schema):
        self._params_schema = params_schema
        self.reached = []

    def tool(self):
        return ToolSpec(name="collect")

    def params_schema(self):
        return self._params_schema

    def describe(self):
        return Declaration(behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True), node_types=["issue"], edge_types=[])

    def walk(self, collect_id, params, foreign):
        self.reached.append((collect_id, params))
        return Result([], [], Complete())


_PROJECT_PARAMS = {
    "type": "object",
    "required": ["project"],
    "properties": {"project": {"type": "string"}, "depth": {"type": "integer", "minimum": 0}},
}


def _call(collector, arguments):
    responses, _, _ = support.drive_in_process(
        lambda i, o, e: new_speaker(collector, i, o, e), [support.call_frame("collect", arguments)]
    )
    return support.result_of(responses, 3)


class ParamsValidationTest(unittest.TestCase):
    def test_row10a_a_call_omitting_the_collect_id_never_reaches_the_walk(self):
        collector = _RecordingCollector(_PROJECT_PARAMS)
        result = _call(collector, {"params": {"project": "acme"}})
        self.assertIs(result["isError"], True)
        self.assertIn("id", result["content"][0]["text"])
        self.assertEqual(collector.reached, [], "the walk must not run on arguments the advertised schema refuses")

    def test_row10a2_a_collect_id_of_the_wrong_type_is_caught_by_the_schema_gate_ALONE(self):
        """THE DISCRIMINATING INPUT CLASS FOR THE SCHEMA GATE, and it is the row
        that measures it.

        Row 10a's own class -- a call omitting `id` -- is caught TWICE: by the
        schema gate, and by the empty-collect-id refusal that follows it, because
        `arguments.get("id", "")` is the empty string for an absent key. Measured:
        deleting the validate() call left row 10a green. A collect id that is
        PRESENT and is a NUMBER passes the empty-string test and is refused only by
        the schema, so this row goes red the moment the gate is removed.
        """
        collector = _RecordingCollector({"type": "object"})
        result = _call(collector, {"id": 123})
        self.assertIs(result["isError"], True)
        self.assertIn("advertised input schema", result["content"][0]["text"])
        self.assertIn("id", result["content"][0]["text"])
        self.assertEqual(collector.reached, [], "the walk must not run on an id the advertised schema refuses")

    def test_row10a3_a_required_params_key_the_collector_declared_is_the_required_loops_own_class(self):
        """The second discriminator, for the validator's REQUIRED loop rather than
        for the gate as a whole: a params object missing a key the collector's own
        schema requires is caught by nothing else in the handler."""
        collector = _RecordingCollector(_PROJECT_PARAMS)
        result = _call(collector, {"id": "probe", "params": {}})
        self.assertIs(result["isError"], True)
        self.assertIn("project", result["content"][0]["text"])
        self.assertEqual(collector.reached, [])

    def test_row10b_params_violating_the_collectors_declared_schema_never_reach_the_walk(self):
        for arguments, expected in (
            ({"id": "probe", "params": "a string"}, "params"),
            ({"id": "probe", "params": {}}, "project"),
            ({"id": "probe", "params": {"project": 7}}, "project"),
            ({"id": "probe", "params": {"project": "acme", "depth": -1}}, "depth"),
            ({"id": "probe", "params": {"project": "acme", "depth": "deep"}}, "depth"),
        ):
            with self.subTest(arguments):
                collector = _RecordingCollector(_PROJECT_PARAMS)
                result = _call(collector, arguments)
                self.assertIs(result["isError"], True)
                self.assertIn(expected, result["content"][0]["text"])
                self.assertEqual(collector.reached, [])

    def test_row10c_a_conforming_call_does_reach_the_walk(self):
        """THE SAME-RUN CONTROL through the same instrument: without it the rows
        above would pass for a gate that refused everything."""
        collector = _RecordingCollector(_PROJECT_PARAMS)
        result = _call(collector, {"id": "probe", "params": {"project": "acme", "depth": 2}})
        self.assertNotIn("isError", result)
        self.assertEqual(collector.reached, [("probe", {"project": "acme", "depth": 2})])

    def test_a_paramless_collect_is_admitted_because_params_is_not_required(self):
        """The reason the input schema is the contract file with only params
        spliced in: the client omits params entirely when a collect carries none,
        and a required `params` would refuse every such collect before it was
        sent."""
        collector = _RecordingCollector({"type": "object"})
        result = _call(collector, {"id": "probe"})
        self.assertNotIn("isError", result)
        self.assertEqual(collector.reached, [("probe", None)])

    def test_a_declared_context_block_of_the_wrong_type_is_refused(self):
        collector = _RecordingCollector({"type": "object"})
        result = _call(collector, {"id": "probe", "context": "not an object"})
        self.assertIs(result["isError"], True)
        self.assertIn("context", result["content"][0]["text"])
        self.assertEqual(collector.reached, [])

    def test_the_walk_receives_the_declared_foreign_context_as_the_block_type(self):
        collector = _RecordingCollector({"type": "object"})
        seen = []

        def walk(collect_id, params, foreign):
            seen.append(foreign)
            return Result([], [], Complete())

        collector.walk = walk
        _call(
            collector,
            {
                "id": "probe",
                "context": {"code": [{"graph_name": "knowledge", "nodes": [{"id": "a", "type": "function"}], "edges": []}]},
            },
        )
        self.assertEqual(len(seen), 1)
        block = seen[0]
        self.assertFalse(block.is_empty())
        self.assertEqual(block.families(), ["code"])
        self.assertTrue(block.declared("code"))
        self.assertFalse(block.declared("logs"))
        self.assertEqual(block.graphs("code")[0].nodes[0].id, "a")

    def test_a_collector_whose_entry_declares_nothing_receives_the_empty_block(self):
        collector = _RecordingCollector({"type": "object"})
        seen = []

        def walk(collect_id, params, foreign):
            seen.append(foreign)
            return Result([], [], Complete())

        collector.walk = walk
        _call(collector, {"id": "probe"})
        self.assertTrue(seen[0].is_empty())
        self.assertEqual(seen[0].families(), [])


if __name__ == "__main__":
    unittest.main()
