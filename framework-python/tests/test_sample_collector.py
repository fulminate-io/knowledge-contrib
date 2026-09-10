# SPDX-License-Identifier: Apache-2.0

"""Rows 17 to 21 — the sample collector, and the arms R3 drives end to end.

Row 19: BYTE-IDEMPOTENCE. Two walks over one unchanged tree produce BYTE-IDENTICAL
        result documents, compared as encoded bytes rather than as node and edge
        counts. Counts are a weaker claim and this row does not make it.
Row 20: an Incomplete walk carries its reason; a walk that raises becomes an
        isError result with text, never an empty envelope.
Row 21: DERIVED VALUES GET PRODUCER-INDEPENDENT EXPECTATIONS. Every node id this
        collector emits is a path, so the expectation is computed from the raw
        fixture with os.listdir rather than taken from the collector's own output.

The end-to-end half of R3 — an isolated client and server pair, a real collect, and
the graph read back through search, query and traverse — is a live confirmation on
two real binaries rather than a unit test, and is reported as one.
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import sample_collector  # noqa: E402

from knowledge_collector import encode_result, new_speaker  # noqa: E402

from . import support  # noqa: E402


def _fixture(scratch):
    """A directory whose contents this test KNOWS, because it wrote them."""
    os.makedirs(os.path.join(scratch, "sub"), exist_ok=True)
    for name in ("alpha.txt", "beta.txt", "gamma.txt"):
        with open(os.path.join(scratch, name), "w", encoding="utf-8") as handle:
            handle.write(name)
    return scratch


def _walk(path, **params):
    collector = sample_collector.DirectoryCollector()
    arguments = {"path": path}
    arguments.update(params)
    return collector.walk("probe", arguments, None)


class SampleWalkTest(unittest.TestCase):
    def test_row21_the_expectation_is_computed_from_the_raw_fixture_not_from_the_collector(self):
        with tempfile.TemporaryDirectory() as scratch:
            root = _fixture(scratch)
            # THE INDEPENDENT EXPECTATION: the ids are paths, so os.listdir is the
            # producer-independent source for them.
            expected_children = sorted(os.path.join(os.path.abspath(root), name) for name in os.listdir(root))
            expected_ids = [os.path.abspath(root)] + expected_children

            result = _walk(root)
            self.assertEqual([node.id for node in result.nodes], expected_ids)
            self.assertEqual(
                [(edge.from_id, edge.to_id) for edge in result.edges],
                [(os.path.abspath(root), child) for child in expected_children],
            )

    def test_the_root_is_a_directory_node_and_the_children_are_file_nodes(self):
        with tempfile.TemporaryDirectory() as scratch:
            result = _walk(_fixture(scratch))
            self.assertEqual(result.nodes[0].type, "directory")
            self.assertEqual({node.type for node in result.nodes[1:]}, {"file"})
            self.assertEqual({edge.type for edge in result.edges}, {"contains"})

    def test_the_child_metadata_distinguishes_a_directory_from_a_file(self):
        with tempfile.TemporaryDirectory() as scratch:
            result = _walk(_fixture(scratch))
            by_name = {os.path.basename(node.id): node for node in result.nodes[1:]}
            self.assertEqual(by_name["sub"].metadata["is_dir"], "true")
            self.assertEqual(by_name["alpha.txt"].metadata["is_dir"], "false")

    def test_row19_a_second_walk_over_an_unchanged_tree_is_byte_identical(self):
        """BYTES, NOT COUNTS. Two walks are encoded through the same envelope
        encoder and the resulting documents are compared as serialized bytes, so a
        field that varied between runs would red here and a count comparison would
        not."""
        with tempfile.TemporaryDirectory() as scratch:
            root = _fixture(scratch)
            first = json.dumps(encode_result("collect", _walk(root)), sort_keys=True).encode("utf-8")
            second = json.dumps(encode_result("collect", _walk(root)), sort_keys=True).encode("utf-8")
            self.assertEqual(first, second)
            self.assertGreater(len(first), 200, "the control: the documents compared are not empty")

    def test_row19_control_a_changed_tree_is_not_byte_identical(self):
        """Without this the row above would pass for an encoder that emitted a
        constant."""
        with tempfile.TemporaryDirectory() as scratch:
            root = _fixture(scratch)
            first = json.dumps(encode_result("collect", _walk(root)), sort_keys=True)
            with open(os.path.join(root, "delta.txt"), "w", encoding="utf-8") as handle:
                handle.write("delta")
            second = json.dumps(encode_result("collect", _walk(root)), sort_keys=True)
            self.assertNotEqual(first, second)

    def test_row20a_a_walk_that_hits_the_limit_asserts_incomplete_with_its_reason(self):
        with tempfile.TemporaryDirectory() as scratch:
            result = _walk(_fixture(scratch), limit=2)
            self.assertFalse(result.complete.is_complete())
            self.assertIn("limit of 2", result.complete.reason())
            self.assertEqual(len(result.nodes), 3, "the root plus the two entries the limit admitted")
            self.assertIs(encode_result("collect", result)["walk_complete"], False)

    def test_row20a_control_a_limit_the_tree_does_not_reach_asserts_complete(self):
        with tempfile.TemporaryDirectory() as scratch:
            result = _walk(_fixture(scratch), limit=99)
            self.assertTrue(result.complete.is_complete())
            self.assertEqual(result.complete.reason(), "")

    def test_row20b_a_walk_whose_source_is_missing_raises_rather_than_returning_empty(self):
        with self.assertRaises(ValueError) as caught:
            _walk("/no/such/directory/at/all")
        self.assertIn("is not a directory", str(caught.exception))

    def test_row20b_over_the_wire_that_raise_is_an_isError_result_with_text(self):
        collector = sample_collector.DirectoryCollector()
        responses, _, _ = support.drive_in_process(
            lambda i, o, e: new_speaker(collector, i, o, e),
            [support.call_frame("collect", {"id": "probe", "params": {"path": "/no/such/directory/at/all"}})],
        )
        result = support.result_of(responses, 3)
        self.assertIs(result["isError"], True)
        self.assertIn("is not a directory", result["content"][0]["text"])
        self.assertNotIn("structuredContent", result, "never an empty successful envelope")

    def test_an_empty_directory_is_a_complete_walk_with_one_node_and_no_edges(self):
        """A walk that FOUND NOTHING is complete and empty, which is a different
        verdict from a walk that gave up."""
        with tempfile.TemporaryDirectory() as scratch:
            result = _walk(scratch)
            self.assertTrue(result.complete.is_complete())
            self.assertEqual(len(result.nodes), 1)
            self.assertEqual(result.edges, [])
            self.assertEqual(encode_result("collect", result)["edges"], [])

    def test_the_environment_default_is_read_only_when_the_call_sends_no_path(self):
        with tempfile.TemporaryDirectory() as scratch:
            root = _fixture(scratch)
            os.environ["SAMPLE_PY_ROOT"] = root
            try:
                self.assertEqual(_walk("").nodes[0].id, os.path.abspath(root))
                other = tempfile.mkdtemp()
                self.assertEqual(_walk(other).nodes[0].id, os.path.abspath(other), "an explicit path wins")
            finally:
                del os.environ["SAMPLE_PY_ROOT"]

    def test_a_walk_with_no_path_and_no_default_raises_naming_both_ways_to_supply_one(self):
        os.environ.pop("SAMPLE_PY_ROOT", None)
        with self.assertRaises(ValueError) as caught:
            _walk("")
        self.assertIn("path", str(caught.exception))
        self.assertIn("SAMPLE_PY_ROOT", str(caught.exception))

    def test_the_collect_id_does_not_reach_the_emitted_ids(self):
        """The collect id names the graph INSTANCE, not the family and not the
        nodes. A collector that folded it into its ids would make the same source
        produce different graphs per instance."""
        with tempfile.TemporaryDirectory() as scratch:
            root = _fixture(scratch)
            collector = sample_collector.DirectoryCollector()
            first = collector.walk("instance-a", {"path": root}, None)
            second = collector.walk("instance-b", {"path": root}, None)
            self.assertEqual([n.id for n in first.nodes], [n.id for n in second.nodes])


class SampleAsAChildTest(unittest.TestCase):
    def test_it_serves_a_full_session_as_a_real_child_process(self):
        with tempfile.TemporaryDirectory() as scratch:
            root = _fixture(scratch)
            frames = [
                support.initialize_frame(),
                support.frame({"jsonrpc": "2.0", "method": "notifications/initialized"}),
                support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
                support.call_frame("collect", {"id": "probe", "params": {"path": root}}),
            ]
            responses, _, stderr, code = support.drive_child("sample_collector.py", frames, env={})
            self.assertEqual(code, 0, stderr)
            self.assertEqual(support.result_of(responses, 1)["serverInfo"]["name"], "collect")
            self.assertEqual(
                sorted(t["name"] for t in support.result_of(responses, 2)["tools"]), ["collect", "describe"]
            )
            payload = support.result_of(responses, 3)["structuredContent"]
            self.assertEqual(len(payload["nodes"]), 1 + len(os.listdir(root)))
            self.assertIs(payload["walk_complete"], True)

    def test_the_child_receives_no_environment_at_all_and_still_speaks(self):
        """The entry's env block is the child's whole environment, and an empty
        block is a real registration: no HOME, no PATH. The port must not need
        one to speak the protocol."""
        responses, _, stderr, code = support.drive_child("sample_collector.py", [support.initialize_frame()], env={})
        self.assertEqual(code, 0, stderr)
        self.assertIsNotNone(support.result_of(responses, 1))


if __name__ == "__main__":
    unittest.main()
