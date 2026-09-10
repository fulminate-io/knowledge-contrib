# SPDX-License-Identifier: Apache-2.0

"""Row 16 — THE CONFORMANCE STUB IS A TRANSCRIPTION, AND THIS IS WHAT PINS IT.

The eleven provider-dialing tests assert the client stub's EXACT bytes and
strings, not protocol behaviour, so this port's stub is only useful to them while
it is byte-faithful. A drift on the client side would otherwise surface as eleven
distant failures in another module with no message pointing here.

EVERY EXPECTATION BELOW IS READ OUT OF THE CLIENT'S OWN STUB SOURCE, never
transcribed into this file a second time. That is the difference between a pin and
a duplicate: a duplicate agrees with itself forever.

IT SKIPS BY NAME OUTSIDE THIS REPOSITORY, where the client's source is not
present.
"""

import re
import unittest

import conformance_stub as stub

from . import support


def _go_string_consts(source):
    """Every `name = "value"` binding in the file, whatever block it sits in."""
    return dict(re.findall(r'^\s*(\w+)\s*=\s*"((?:[^"\\]|\\.)*)"', source, re.MULTILINE))


class StubFidelityTest(unittest.TestCase):
    def setUp(self):
        path = support.client_stub_source()
        if path is None:
            self.skipTest(
                "the knowledge client's stubprovider_test.go is out of reach from %s; this port is running "
                "outside the repository, so there is no authority to pin the transcription against" % support.PORT_ROOT
            )
        self.source = support.read_text(path)
        self.consts = _go_string_consts(self.source)

    def test_the_control_the_client_stub_source_really_was_read(self):
        """A ZERO NEEDS A CONTROL: without this row every assertion below would
        pass over an empty regex result."""
        self.assertGreater(len(self.consts), 20, "the const extraction found almost nothing; the matcher is broken")
        self.assertIn("defaultStubTool", self.consts)

    def test_the_mode_strings_are_the_clients_modes(self):
        """The subject is the client's `// Stub modes.` const block, sliced out by
        its own comment: the switch variable is also named stubMode-something and a
        prefix match over the whole file would sweep it in.

        THE ASSERTION IS SET EQUALITY AND CARRIES NO COUNT. It used to pin a
        hand-written sixteen beside the set comparison, and when the client's stub
        gained its four describe arms that literal is what went red -- with a
        message about a number rather than about the four modes this stub does not
        emulate. A count beside a set is a second thing to keep true and it is the
        one that rots; the control below is what keeps the comparison from passing
        over an empty extraction."""
        block = _slice_between(self.source, "// Stub modes.", "\n)\n")
        client_modes = sorted(v for k, v in _go_string_consts(block).items() if k.startswith("stubMode"))
        self.assertGreater(
            len(client_modes), 1, "the client's stub-mode block yielded %s; the slice or the matcher is broken" % client_modes
        )
        missing = sorted(set(client_modes) - set(stub.MODES))
        extra = sorted(set(stub.MODES) - set(client_modes))
        self.assertEqual(
            (missing, extra),
            ([], []),
            "this stub does not emulate %s and emulates %s the client does not have" % (missing, extra),
        )

    def test_the_default_tool_and_the_other_tool_are_the_clients(self):
        self.assertEqual(stub.DEFAULT_STUB_TOOL, self.consts["defaultStubTool"])
        self.assertIn('name = "some_other_tool"', self.source)
        self.assertEqual(stub.OTHER_TOOL, "some_other_tool")

    def test_the_switch_variable_names_are_the_clients(self):
        """PENDING-PIN NEIGHBOUR. The mode reaches a spawned collector through the
        entry's env block, which is the child's whole environment, so this port's
        stub reads the name the client's harness sets. When that harness renames
        it, this row reds by name rather than eleven distant tests going quiet."""
        self.assertEqual(stub.STUB_MODE_ENV, self.consts["stubModeEnv"])
        self.assertEqual(stub.STUB_TOOL_ENV, self.consts["stubToolEnv"])

    def test_the_tool_error_literal_is_the_clients(self):
        self.assertIn(stub.TOOL_ERROR_TEXT, self.source)
        self.assertEqual(stub.TOOL_ERROR_TEXT, "the stub provider refused this collect")

    def test_the_conforming_payload_carries_the_clients_three_node_ids(self):
        payload = stub.conforming_payload()
        for node_id in ("ISSUE-1", "ISSUE-2", "ISSUE-3"):
            self.assertIn('"%s"' % node_id, self.source)
        self.assertEqual([n["id"] for n in payload["nodes"]], ["ISSUE-1", "ISSUE-2", "ISSUE-3"])

    def test_the_conforming_payloads_metadata_priority_is_the_clients(self):
        self.assertIn('"priority": "high"', self.source)
        self.assertEqual(stub.conforming_payload()["nodes"][0]["metadata"], {"priority": "high"})

    def test_every_populated_optional_field_of_issue_one_is_the_clients(self):
        """The whole first node, field by field, against the client's own literal
        for it -- so a value changed on that side is a named red here."""
        block = _slice_between(self.source, '"id": "ISSUE-1"', '"id": "ISSUE-2"')
        node = stub.conforming_payload()["nodes"][0]
        for field, value in node.items():
            if field in ("id", "type", "metadata", "is_exported"):
                continue
            with self.subTest(field):
                self.assertIn('"%s"' % field, block)
                self.assertIn(str(value) if not isinstance(value, str) else '"%s"' % value, block)

    def test_the_dangling_edge_endpoint_and_the_misspelled_key_are_the_clients(self):
        self.assertIn('"to_id": "absent"', self.source)
        self.assertIn('"summry": "typo\'d key"', self.source)
        self.assertEqual(stub.stub_payload(stub.MODE_DANGLING_EDGE, None)["edges"][0]["to_id"], "absent")
        self.assertEqual(stub.stub_payload(stub.MODE_UNKNOWN_FIELD, None)["nodes"][0]["summry"], "typo'd key")

    def test_the_env_report_node_shape_is_the_clients(self):
        self.assertIn('"type": "env_var"', self.source)
        self.assertIn('"present"', self.source)
        self.assertIn('meta["value"] = value', self.source)

    def test_the_two_process_exit_codes_are_the_clients(self):
        self.assertIn("os.Exit(%d)" % stub.EXIT_BEFORE_HANDSHAKE_CODE, self.source)
        self.assertIn("os.Exit(%d)" % stub.EXIT_MID_SESSION_CODE, self.source)
        self.assertEqual((stub.EXIT_BEFORE_HANDSHAKE_CODE, stub.EXIT_MID_SESSION_CODE), (3, 7))

    def test_the_over_former_cap_dimensions_are_the_clients(self):
        self.assertIn("bodyLen  = 1 << 20", self.source)
        self.assertIn("nodeRows = 68", self.source)
        self.assertEqual(stub.OVER_FORMER_CAP_BODY_LEN, 1 << 20)
        self.assertEqual(stub.OVER_FORMER_CAP_NODE_ROWS, 68)

    def test_the_no_mode_exit_code_is_the_clients(self):
        """The client stub exits 9 when a marked child receives no mode, rather
        than running the suite. This stub takes the same code for the same
        reason."""
        self.assertIn("os.Exit(9)", self.source)
        self.assertEqual(stub.EXIT_NO_MODE, 9)

    def test_the_stub_family_and_the_bad_input_schema_shape_are_the_clients(self):
        self.assertIn('"properties": map[string]any{"params": map[string]any{"type": "object"}}', self.source)
        self.assertEqual(
            stub.stub_input_schema(stub.MODE_BAD_INPUT_SCHEMA),
            {"type": "object", "properties": {"params": {"type": "object"}}},
        )

    def test_the_breaks_own_word_widening_is_the_clients(self):
        self.assertIn('items["required"] = []any{"id", "type", "summary"}', self.source)
        self.assertEqual(
            stub.stub_output_schema(stub.MODE_BREAKS_OWN_WORD)["properties"]["nodes"]["items"]["required"],
            ["id", "type", "summary"],
        )

    def test_the_bad_output_schema_omission_is_the_clients(self):
        self.assertIn('out["required"] = []any{"nodes", "edges"}', self.source)
        schema = stub.stub_output_schema(stub.MODE_BAD_OUTPUT_SCHEMA)
        self.assertEqual(schema["required"], ["nodes", "edges"])
        self.assertNotIn("walk_complete", schema["properties"])


def _slice_between(source, start_marker, end_marker):
    start = source.index(start_marker)
    end = source.index(end_marker, start)
    return source[start:end]


if __name__ == "__main__":
    unittest.main()
