# SPDX-License-Identifier: Apache-2.0

"""Rows 16 and 16a — the conformance stub, driven as a REAL CHILD PROCESS in every
one of its sixteen modes.

WHY A CHILD AND NOT AN IMPORT. Two of the sixteen are process exits, one is an
environment report whose subject IS the child's environment, and the harness this
stub exists for spawns it. An in-process drive would prove none of those.

THE ENVIRONMENT EACH CHILD RECEIVES IS THE BLOCK AND ONLY THE BLOCK, exactly as
the daemon spawns a collector from its registration entry. That is what makes the
env-report rows here the collector-side proof of the child-environment property
the client asserts parent-side.

ROW 13a's POSITIVE TELL lives here too: the stub's spawn record. A harness that
falls back to its own in-process stub produces a run IDENTICAL in every count to
one that really spawned this process, so a pass count is evidence for neither.
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import conformance_stub as stub  # noqa: E402

from . import support  # noqa: E402


def _run(mode, frames, extra_env=None, argv=()):
    env = {stub.STUB_MODE_ENV: mode}
    env.update(extra_env or {})
    return support.drive_child("conformance_stub.py", frames, env=env, argv=argv)


_HANDSHAKE = [support.initialize_frame(), support.frame({"jsonrpc": "2.0", "method": "notifications/initialized"})]
_LIST = support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})


def _listing_of(responses):
    return support.result_of(responses, 2)


def _tool(responses):
    return _listing_of(responses)["tools"][0]


class ModeInventoryTest(unittest.TestCase):
    def test_every_mode_is_named_once(self):
        """NO COUNT LITERAL. A number beside the list is a second thing to keep
        true, and it went stale the moment the client's stub gained its describe
        arms: the pin that matters is that this list has no duplicate and that it
        equals the client's, which test_stub_fidelity asserts against the client's
        own source. The floor here is only that the list is not empty."""
        self.assertEqual(len(stub.MODES), len(set(stub.MODES)), "a mode is named twice: %s" % (stub.MODES,))
        self.assertGreater(len(stub.MODES), 1, "the mode inventory is empty; every row below would be vacuous")

    def test_a_child_spawned_with_no_mode_exits_loud_rather_than_guessing_one(self):
        responses, raw, err, code = support.drive_child("conformance_stub.py", [], env={})
        self.assertEqual(code, stub.EXIT_NO_MODE)
        self.assertEqual(raw, b"", "a refusal goes to stderr; stdout is the protocol stream")
        self.assertIn(stub.STUB_MODE_ENV, err)
        self.assertIn("UNSET", err)

    def test_a_child_spawned_with_an_unknown_mode_names_the_modes_it_does_emulate(self):
        responses, raw, err, code = support.drive_child(
            "conformance_stub.py", [], env={stub.STUB_MODE_ENV: "not-a-mode"}
        )
        self.assertEqual(code, stub.EXIT_NO_MODE)
        self.assertIn("not-a-mode", err)
        self.assertIn(stub.MODE_CONFORMING, err)


class ConformingModeTest(unittest.TestCase):
    def test_the_three_node_payload_with_its_absent_versus_present_and_empty_distinction(self):
        responses, _, err, code = _run(
            stub.MODE_CONFORMING, _HANDSHAKE + [support.call_frame(stub.DEFAULT_STUB_TOOL, {"id": "probe"})]
        )
        self.assertEqual(code, 0, err)
        payload = support.result_of(responses, 3)["structuredContent"]
        nodes = {n["id"]: n for n in payload["nodes"]}
        self.assertEqual(sorted(nodes), ["ISSUE-1", "ISSUE-2", "ISSUE-3"])

        # ISSUE-1: every optional field populated, and metadata.priority "high".
        self.assertEqual(nodes["ISSUE-1"]["metadata"], {"priority": "high"})
        self.assertEqual(nodes["ISSUE-1"]["symbol_name"], "Login broken")
        self.assertIs(nodes["ISSUE-1"]["is_exported"], True)

        # ISSUE-2 OMITS its optional fields; ISSUE-3 carries them PRESENT AND
        # EMPTY. Two different inputs, not a duplicate.
        self.assertEqual(sorted(nodes["ISSUE-2"]), ["id", "type"])
        self.assertEqual(nodes["ISSUE-3"]["symbol_name"], "")
        self.assertEqual(nodes["ISSUE-3"]["metadata"], {})
        self.assertEqual(nodes["ISSUE-3"]["start_line"], 0)
        self.assertIs(nodes["ISSUE-3"]["is_exported"], False)

        self.assertEqual(payload["edges"], [{"from_id": "ISSUE-1", "to_id": "ISSUE-2", "type": "blocks"}])
        self.assertIs(payload["walk_complete"], True)

    def test_the_default_tool_is_collect_graph_and_both_schemas_are_the_contract(self):
        responses, _, err, code = _run(stub.MODE_CONFORMING, _HANDSHAKE + [_LIST])
        self.assertEqual(code, 0, err)
        tool = _tool(responses)
        self.assertEqual(tool["name"], "collect_graph")
        self.assertEqual(tool["inputSchema"], json.loads(support.read_bytes(os.path.join(support.CONTRACT_DIR, "collector_input.schema.json"))))
        self.assertEqual(tool["outputSchema"], json.loads(support.read_bytes(os.path.join(support.CONTRACT_DIR, "collector_output.schema.json"))))

    def test_the_tool_name_is_overridable_by_the_harnesss_own_variable(self):
        responses, _, err, _ = _run(stub.MODE_CONFORMING, _HANDSHAKE + [_LIST], extra_env={stub.STUB_TOOL_ENV: "collect"})
        self.assertEqual(_tool(responses)["name"], "collect")


class SchemaBendingModeTest(unittest.TestCase):
    def test_no_such_tool_serves_some_other_tool(self):
        responses, _, _, _ = _run(stub.MODE_NO_SUCH_TOOL, _HANDSHAKE + [_LIST])
        self.assertEqual(_tool(responses)["name"], "some_other_tool")

    def test_no_output_schema_advertises_none_at_all(self):
        responses, _, _, _ = _run(stub.MODE_NO_OUTPUT_SCHEMA, _HANDSHAKE + [_LIST])
        self.assertNotIn("outputSchema", _tool(responses))

    def test_bad_output_schema_drops_the_completeness_assertion(self):
        responses, _, _, _ = _run(stub.MODE_BAD_OUTPUT_SCHEMA, _HANDSHAKE + [_LIST])
        schema = _tool(responses)["outputSchema"]
        self.assertEqual(schema["required"], ["nodes", "edges"])
        self.assertNotIn("walk_complete", schema["properties"])

    def test_bad_input_schema_does_not_require_the_collect_id(self):
        responses, _, _, _ = _run(stub.MODE_BAD_INPUT_SCHEMA, _HANDSHAKE + [_LIST])
        schema = _tool(responses)["inputSchema"]
        self.assertEqual(schema, {"type": "object", "properties": {"params": {"type": "object"}}})

    def test_breaks_own_word_advertises_stricter_then_returns_a_node_without_a_summary(self):
        responses, _, _, _ = _run(
            stub.MODE_BREAKS_OWN_WORD,
            _HANDSHAKE + [_LIST, support.call_frame(stub.DEFAULT_STUB_TOOL, {"id": "probe"})],
        )
        schema = _tool(responses)["outputSchema"]
        self.assertEqual(schema["properties"]["nodes"]["items"]["required"], ["id", "type", "summary"])
        node = support.result_of(responses, 3)["structuredContent"]["nodes"][0]
        self.assertNotIn("summary", node, "the stub must actually break the word it just gave")


class PayloadModeTest(unittest.TestCase):
    def _payload(self, mode):
        responses, _, err, code = _run(mode, _HANDSHAKE + [support.call_frame(stub.DEFAULT_STUB_TOOL, {"id": "probe"})])
        self.assertEqual(code, 0, err)
        return support.result_of(responses, 3)

    def test_tool_error_returns_the_literal_refusal_text(self):
        result = self._payload(stub.MODE_TOOL_ERROR)
        self.assertIs(result["isError"], True)
        self.assertEqual(result["content"], [{"type": "text", "text": "the stub provider refused this collect"}])

    def test_empty_node_type_returns_one_node_with_the_empty_type(self):
        self.assertEqual(
            self._payload(stub.MODE_EMPTY_NODE_TYPE)["structuredContent"]["nodes"], [{"id": "n1", "type": ""}]
        )

    def test_unknown_field_carries_the_misspelled_key(self):
        node = self._payload(stub.MODE_UNKNOWN_FIELD)["structuredContent"]["nodes"][0]
        self.assertEqual(node["summry"], "typo'd key")
        self.assertNotIn("summary", node)

    def test_dangling_edge_names_an_endpoint_the_result_does_not_carry(self):
        payload = self._payload(stub.MODE_DANGLING_EDGE)["structuredContent"]
        self.assertEqual(payload["edges"], [{"from_id": "n1", "to_id": "absent", "type": "blocks"}])
        self.assertEqual([n["id"] for n in payload["nodes"]], ["n1"])

    def test_incomplete_walk_asserts_walk_complete_false(self):
        payload = self._payload(stub.MODE_INCOMPLETE_WALK)["structuredContent"]
        self.assertIs(payload["walk_complete"], False)
        self.assertEqual(payload["nodes"], [{"id": "n1", "type": "issue"}])

    def test_empty_graph_is_both_arrays_empty_and_a_complete_walk(self):
        payload = self._payload(stub.MODE_EMPTY_GRAPH)["structuredContent"]
        self.assertEqual(payload, {"nodes": [], "edges": [], "walk_complete": True})

    def test_over_former_cap_returns_sixty_eight_one_mebibyte_nodes_chained_by_sixty_seven_edges(self):
        payload = self._payload(stub.MODE_OVER_FORMER_CAP)["structuredContent"]
        self.assertEqual(len(payload["nodes"]), 68)
        self.assertEqual(len(payload["edges"]), 67)
        self.assertEqual(len(payload["nodes"][0]["content"]), 1 << 20)
        self.assertEqual(payload["nodes"][0]["type"], "blob")
        self.assertEqual(payload["edges"][0], {"from_id": "big-0", "to_id": "big-1", "type": "NEXT"})


class ProcessExitModeTest(unittest.TestCase):
    def test_exit_before_handshake_exits_three_without_speaking(self):
        responses, raw, err, code = _run(stub.MODE_EXIT_BEFORE_HANDSHAKE, _HANDSHAKE)
        self.assertEqual(code, 3)
        self.assertEqual(raw, b"", "it must not speak at all")
        self.assertIn("before the handshake", err)

    def test_exit_mid_session_boots_answers_the_handshake_and_dies_inside_the_call(self):
        responses, _, err, code = _run(
            stub.MODE_EXIT_MID_SESSION,
            _HANDSHAKE + [_LIST, support.call_frame(stub.DEFAULT_STUB_TOOL, {"id": "probe"})],
        )
        self.assertEqual(code, 7)
        self.assertIsNotNone(support.result_of(responses, 1), "the handshake was answered before the death")
        self.assertIsNotNone(support.result_of(responses, 2), "and so was the tool listing")
        self.assertIsNone(support.result_of(responses, 3), "the call itself is never answered")
        self.assertIn("mid-session", err)


class EnvReportModeTest(unittest.TestCase):
    """Property 15's COLLECTOR-SIDE coverage, from the child's own vantage: the
    environment a spawned collector receives IS the entry's block, entire."""

    def _report(self, names, block):
        responses, _, err, code = _run(
            stub.MODE_ENV_REPORT,
            _HANDSHAKE + [support.call_frame(stub.DEFAULT_STUB_TOOL, {"id": "probe", "params": {"names": names}})],
            extra_env=block,
        )
        self.assertEqual(code, 0, err)
        payload = support.result_of(responses, 3)["structuredContent"]
        return {n["id"]: n["metadata"] for n in payload["nodes"]}

    def test_a_name_in_the_block_arrives_carrying_the_blocks_value(self):
        report = self._report(["DECLARED"], {"DECLARED": "from-the-block"})
        self.assertEqual(report["DECLARED"], {"present": "true", "value": "from-the-block"})

    def test_a_name_absent_from_the_block_is_absent_in_the_child(self):
        """THE SAME-RUN CONTROL is the parent's own environment: this test process
        certainly has a PATH and a HOME, and the child must have neither."""
        self.assertIn("PATH", os.environ, "control: the parent does hold these")
        report = self._report(["PATH", "HOME", "CANARY"], {"DECLARED": "x"})
        for name in ("PATH", "HOME", "CANARY"):
            self.assertEqual(report[name], {"present": "false"}, "%s must not be inherited" % name)

    def test_an_empty_value_arrives_present_and_empty(self):
        """Present-and-empty is a DIFFERENT input from absent, and the report
        distinguishes them."""
        report = self._report(["EMPTY", "MISSING"], {"EMPTY": ""})
        self.assertEqual(report["EMPTY"], {"present": "true", "value": ""})
        self.assertEqual(report["MISSING"], {"present": "false"})

    def test_the_block_value_beats_any_value_the_parent_holds(self):
        self.assertIn("PATH", os.environ)
        report = self._report(["PATH"], {"PATH": "/only-this"})
        self.assertEqual(report["PATH"], {"present": "true", "value": "/only-this"})

    def test_it_reports_only_the_names_asked_about_and_never_the_whole_environment(self):
        """The security property: a stub that dumped its environment into a result
        the collect then admitted would write whatever the process held into a
        graph."""
        report = self._report(["DECLARED"], {"DECLARED": "x", "SECOND": "y", "THIRD": "z"})
        self.assertEqual(sorted(report), ["DECLARED"])

    def test_asking_for_no_names_reports_no_nodes(self):
        report = self._report([], {"DECLARED": "x"})
        self.assertEqual(report, {})


class SpawnRecordTest(unittest.TestCase):
    """Row 13a's positive tell. A count cannot tell a spawned Python child from a
    harness that fell back to its own in-process stub; this file can."""

    def test_the_stub_records_one_line_per_spawn_carrying_the_mode_and_no_environment_value(self):
        with tempfile.TemporaryDirectory() as scratch:
            log = os.path.join(scratch, "spawns.log")
            for mode in (stub.MODE_CONFORMING, stub.MODE_EMPTY_GRAPH, stub.MODE_EXIT_BEFORE_HANDSHAKE):
                _run(mode, _HANDSHAKE, extra_env={"SECRET_LOOKING": "must-not-appear"}, argv=("--spawn-log", log))
            lines = support.read_text(log).splitlines()
            self.assertEqual(len(lines), 3, "one line per spawn, including the one that exits before speaking")
            self.assertIn("mode=conforming", lines[0])
            self.assertNotIn("must-not-appear", support.read_text(log), "the record says a spawn happened, never what the environment held")

    def test_the_equals_spelling_of_the_flag_works_too(self):
        with tempfile.TemporaryDirectory() as scratch:
            log = os.path.join(scratch, "spawns.log")
            _run(stub.MODE_CONFORMING, _HANDSHAKE, argv=("--spawn-log=%s" % log,))
            self.assertEqual(len(support.read_text(log).splitlines()), 1)

    def test_no_flag_writes_no_record_and_the_stub_still_serves(self):
        responses, _, err, code = _run(stub.MODE_CONFORMING, _HANDSHAKE + [_LIST])
        self.assertEqual(code, 0, err)
        self.assertIsNotNone(_listing_of(responses))

    def test_the_flag_parser_reads_the_path_that_follows_it(self):
        self.assertEqual(stub.spawn_log_path(["--spawn-log", "/tmp/x"]), "/tmp/x")
        self.assertEqual(stub.spawn_log_path(["--spawn-log=/tmp/y"]), "/tmp/y")
        self.assertEqual(stub.spawn_log_path(["--other", "/tmp/z"]), "")
        self.assertEqual(stub.spawn_log_path(["--spawn-log"]), "", "a flag with no value names no path")


class StubStdoutDisciplineTest(unittest.TestCase):
    def test_every_line_the_stub_writes_to_stdout_is_a_jsonrpc_message(self):
        for mode in stub.MODES:
            if mode in (stub.MODE_EXIT_BEFORE_HANDSHAKE, stub.MODE_EXIT_MID_SESSION):
                continue
            with self.subTest(mode):
                frames = _HANDSHAKE + [_LIST, support.call_frame(stub.DEFAULT_STUB_TOOL, {"id": "probe", "params": {"names": []}})]
                _, raw, _, code = _run(mode, frames)
                self.assertEqual(code, 0)
                for line in raw.splitlines():
                    if not line.strip():
                        continue
                    document = json.loads(line)
                    self.assertEqual(document["jsonrpc"], "2.0")


if __name__ == "__main__":
    unittest.main()
