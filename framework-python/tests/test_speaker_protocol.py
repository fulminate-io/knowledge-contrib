# SPDX-License-Identifier: Apache-2.0

"""Rows 9a-9d — the protocol obligations the hand-rolled speaker creates.

An SDK would have supplied the framing and the handshake. This port supplies its
own, so the what-to-test list moves with them:

9a. The handshake answers this port's own protocol version and NEGOTIATES rather
    than echoing what it was sent.
9b. `notifications/initialized` produces NO RESPONSE AT ALL -- it is a
    notification, and a response to it is a protocol error rather than a harmless
    extra.
9c. The framing is newline-delimited with NO LENGTH BOUND in either direction: a
    68 MiB result is ONE line on the wire. THIS IS THE ROW AN IMPLEMENTER WOULD
    FAIL BY WRITING A FIXED-SIZE READ OR A LINE-LENGTH CAP, and it fails nowhere
    else -- every other row here passes with a bounded reader.
9d. A malformed frame is refused LOUDLY, never skipped. Four input classes, one
    arm each.

AND TWO CLASSES ROW 9d DOES NOT NAME, added after they were measured. Row 9d stops
at the FRAME level; a well-formed JSON-RPC request object can still carry a
malformed MEMBER, and before those arms existed a `params` that was an array, a
string or a number -- or a `params.name` that was unhashable -- raised out of
serve() and ENDED THE SESSION with zero bytes on stdout. MalformedMemberTest is
that class, and every one of its rows carries a follow-on frame in the same
stream, because the property at stake is that a refusal is a response rather than
a disconnect.
"""

import json
import unittest

from knowledge_collector import (
    BehaviorDefaults,
    Collector,
    Complete,
    Declaration,
    Node,
    PROTOCOL_VERSION,
    Result,
    Speaker,
    ToolDefinition,
    ToolSpec,
    new_speaker,
)

from . import support


class _Echo(Collector):
    def __init__(self, walk_fn=None):
        self._walk = walk_fn

    def tool(self):
        return ToolSpec(name="collect")

    def params_schema(self):
        return {"type": "object"}

    def describe(self):
        return Declaration(behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True), node_types=["blob"], edge_types=["NEXT"])

    def walk(self, collect_id, params, foreign):
        if self._walk is not None:
            return self._walk(collect_id, params, foreign)
        return Result([], [], Complete())


def _drive(frames, collector=None):
    collector = collector if collector is not None else _Echo()
    return support.drive_in_process(lambda i, o, e: new_speaker(collector, i, o, e), frames)


class HandshakeTest(unittest.TestCase):
    def test_row9a_the_answered_protocol_version_is_this_ports_constant(self):
        responses, _, _ = _drive([support.initialize_frame(requested_version="2026-07-28")])
        self.assertEqual(support.result_of(responses, 1)["protocolVersion"], PROTOCOL_VERSION)
        self.assertEqual(PROTOCOL_VERSION, "2025-06-18", "the floor the requirement names")

    def test_row9a_a_different_requested_version_gets_the_same_answer(self):
        """NEGOTIATION, NOT ECHO. A speaker that mirrored the request would claim
        to support whatever it was handed."""
        for requested in ("2024-11-05", "2025-06-18", "2026-07-28", "not-a-version"):
            with self.subTest(requested):
                responses, _, _ = _drive([support.initialize_frame(requested_version=requested)])
                self.assertEqual(support.result_of(responses, 1)["protocolVersion"], PROTOCOL_VERSION)

    def test_the_handshake_declares_the_tools_capability(self):
        responses, _, _ = _drive([support.initialize_frame()])
        self.assertIn("tools", support.result_of(responses, 1)["capabilities"])

    def test_row9b_notifications_initialized_produces_no_bytes_at_all(self):
        responses, raw, stderr = _drive([support.frame({"jsonrpc": "2.0", "method": "notifications/initialized"})])
        self.assertEqual(responses, [])
        self.assertEqual(raw, b"", "a notification is answered with nothing; a response to one is a protocol error")
        self.assertIn("handshake complete", stderr, "and the fact that it arrived reaches stderr")

    def test_row9b_control_the_same_frame_carrying_an_id_is_answered(self):
        """The control that makes the row above about the ABSENT id rather than
        about an unrecognized method name."""
        responses, raw, _ = _drive(
            [support.frame({"jsonrpc": "2.0", "id": 7, "method": "notifications/initialized"})]
        )
        self.assertNotEqual(raw, b"")
        self.assertIsNotNone(support.error_of(responses, 7))

    def test_a_notification_naming_any_other_method_is_also_answered_with_nothing(self):
        responses, raw, stderr = _drive([support.frame({"jsonrpc": "2.0", "method": "notifications/cancelled"})])
        self.assertEqual(responses, [])
        self.assertEqual(raw, b"")
        self.assertIn("notifications/cancelled", stderr)


class FramingTest(unittest.TestCase):
    def test_row9c_a_68_mib_result_round_trips_as_one_newline_terminated_line(self):
        """THE UNBOUNDED-FRAMING ROW. It round-trips the client stub's
        over-former-cap payload -- 68 nodes of 1 MiB each -- through this port's own
        reader and writer. A fixed-size read or a line-length cap fails here and
        nowhere else."""
        body = "x" * (1 << 20)
        rows = 68

        def walk(collect_id, params, foreign):
            nodes = [Node(id="big-%d" % i, type="blob", content=body) for i in range(rows)]
            return Result(nodes, [], Complete())

        # THE INBOUND HALF IS THE SAME SIZE AS THE OUTBOUND ONE, on purpose. A 1
        # MiB frame would clear a naive buffer and still fail nothing under the 4
        # MiB default the Go framework's own transport had to disable, so the
        # inbound line has to cross the same 68 MiB the result does.
        big_argument = "y" * (rows << 20)
        frames = [
            support.initialize_frame(),
            support.call_frame("collect", {"id": "probe", "params": {"padding": big_argument}}),
        ]
        self.assertGreater(len(frames[1]), rows << 20, "the call frame this speaker must READ is over 68 MiB")

        responses, raw, _ = _drive(frames, collector=_Echo(walk))
        result = support.result_of(responses, 3)
        self.assertEqual(len(result["structuredContent"]["nodes"]), rows)
        self.assertEqual(len(result["structuredContent"]["nodes"][0]["content"]), 1 << 20)

        # THE OUTBOUND HALF: the whole result is ONE line, not chunked.
        lines = [line for line in raw.split(b"\n") if line]
        self.assertEqual(len(lines), 2, "one handshake answer and one result, each a single line")
        self.assertGreater(len(lines[1]), rows * (1 << 20))

    def test_two_frames_on_one_read_are_two_messages(self):
        responses, _, _ = _drive(
            [
                support.initialize_frame(request_id=1),
                support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
            ]
        )
        self.assertEqual([r["id"] for r in responses], [1, 2])


class MalformedFrameTest(unittest.TestCase):
    """Row 9d. Four input classes, one arm each. THE MUTATION THAT MUST TURN A
    NAMED TEST RED: replace any one of these refusals with a `continue` and the
    matching arm below goes red."""

    def test_row9d1_a_non_json_line_is_refused_with_a_parse_error(self):
        responses, _, stderr = _drive([b"this is not json\n"])
        self.assertEqual(len(responses), 1)
        self.assertEqual(responses[0]["error"]["code"], -32700)
        self.assertIsNone(responses[0]["id"])
        self.assertIn("not JSON", stderr)

    def test_row9d1b_an_empty_line_is_refused_rather_than_skipped(self):
        responses, _, stderr = _drive([b"\n"])
        self.assertEqual(len(responses), 1)
        self.assertEqual(responses[0]["error"]["code"], -32700)
        self.assertIn("no JSON-RPC message", stderr)

    def test_row9d2_json_that_is_not_a_jsonrpc_request_object_is_refused(self):
        for payload in (b'[1,2,3]\n', b'"a string"\n', b'{"foo":1}\n', b'{"method":42}\n'):
            with self.subTest(payload):
                responses, _, _ = _drive([payload])
                self.assertEqual(len(responses), 1)
                self.assertEqual(responses[0]["error"]["code"], -32600)

    def test_row9d3_an_unknown_method_answers_minus32601_naming_the_method(self):
        responses, _, _ = _drive([support.frame({"jsonrpc": "2.0", "id": 5, "method": "resources/list"})])
        error = support.error_of(responses, 5)
        self.assertEqual(error["code"], -32601)
        self.assertIn("resources/list", error["message"])

    def test_row9d4_a_request_with_no_id_is_a_notification_and_is_dropped(self):
        responses, raw, stderr = _drive([support.frame({"jsonrpc": "2.0", "method": "resources/list"})])
        self.assertEqual(raw, b"")
        self.assertEqual(responses, [])
        self.assertIn("resources/list", stderr, "dropped, but never silently")

    def test_a_malformed_frame_does_not_end_the_session(self):
        """The refusal is a RESPONSE, not a disconnect: the next real frame is
        still answered."""
        responses, _, _ = _drive([b"garbage\n", support.initialize_frame(request_id=11)])
        self.assertIsNotNone(support.result_of(responses, 11))


class MalformedMemberTest(unittest.TestCase):
    """The FIFTH and SIXTH malformed-input classes: a well-formed JSON-RPC request
    object carrying a malformed MEMBER.

    THESE ARE A DIFFERENT CLASS FROM THE FOUR ABOVE and they were measured, not
    imagined. Before the arms existed, each input below raised out of serve() and
    ended the session with ZERO bytes on stdout: no error for the offending frame,
    and no answer for the well-formed frame behind it. The four frame-level arms
    are all upstream of the member that actually broke.

    EVERY ROW CARRIES A FOLLOW-ON FRAME IN THE SAME STREAM, and that is what
    separates this class from the four: the assertion is not only that an error
    came back, it is that the SESSION SURVIVED to answer the next request. A row
    that checked the error alone would pass against a speaker that then died.
    """

    FOLLOW_ON_ID = 99

    def _drive_with_follow_on(self, frame):
        return _drive([frame, support.frame({"jsonrpc": "2.0", "id": self.FOLLOW_ON_ID, "method": "ping"})])

    def _assert_refused_and_alive(self, frame):
        responses, raw, _ = self._drive_with_follow_on(frame)
        self.assertNotEqual(raw, b"", "the session died: nothing was written at all")
        self.assertEqual(
            support.result_of(responses, self.FOLLOW_ON_ID),
            {},
            "the follow-on frame in the same stream was not answered, so the refusal was a disconnect",
        )
        return responses

    def test_params_that_is_an_array_is_refused_and_the_session_survives(self):
        responses = self._assert_refused_and_alive(
            support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": ["collect", {}]})
        )
        error = support.error_of(responses, 3)
        self.assertEqual(error["code"], -32602)
        self.assertIn("list", error["message"])

    def test_params_that_is_a_string_is_refused_and_the_session_survives(self):
        responses = self._assert_refused_and_alive(
            support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": "collect"})
        )
        error = support.error_of(responses, 3)
        self.assertEqual(error["code"], -32602)
        self.assertIn("str", error["message"])

    def test_params_that_is_a_number_is_refused_and_the_session_survives(self):
        responses = self._assert_refused_and_alive(
            support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": 7})
        )
        error = support.error_of(responses, 3)
        self.assertEqual(error["code"], -32602)
        self.assertIn("int", error["message"])

    def test_a_non_object_params_is_refused_on_every_method_not_only_on_tools_call(self):
        """The refusal is at the FRAME level, so a method whose handler happens to
        ignore params does not quietly admit one that is malformed."""
        for method in ("initialize", "tools/list", "ping"):
            with self.subTest(method):
                responses = self._assert_refused_and_alive(
                    support.frame({"jsonrpc": "2.0", "id": 3, "method": method, "params": [1, 2]})
                )
                self.assertEqual(support.error_of(responses, 3)["code"], -32602)

    def test_a_tool_name_that_is_an_array_is_refused_and_the_session_survives(self):
        """AN UNHASHABLE NAME IS ITS OWN ARM: a list or a dict raises a TypeError
        out of the tool-table lookup rather than an AttributeError out of the
        params access, so it is one level further in and needs its own refusal."""
        responses, _, _ = self._drive_with_follow_on(
            support.frame(
                {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": ["collect"], "arguments": {}}}
            )
        )
        result = support.result_of(responses, 3)
        self.assertIs(result["isError"], True)
        self.assertIn("list", result["content"][0]["text"])
        self.assertEqual(support.result_of(responses, self.FOLLOW_ON_ID), {})

    def test_a_tool_name_that_is_an_object_is_refused_and_the_session_survives(self):
        responses, _, _ = self._drive_with_follow_on(
            support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": {"a": 1}}})
        )
        result = support.result_of(responses, 3)
        self.assertIs(result["isError"], True)
        self.assertIn("dict", result["content"][0]["text"])
        self.assertEqual(support.result_of(responses, self.FOLLOW_ON_ID), {})

    def test_a_tool_name_that_is_a_number_is_refused_by_the_same_arm(self):
        responses, _, _ = self._drive_with_follow_on(
            support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": 7}})
        )
        result = support.result_of(responses, 3)
        self.assertIs(result["isError"], True)
        self.assertIn("int", result["content"][0]["text"])

    def test_the_control_absent_and_null_params_are_admitted_as_no_params(self):
        """THE SAME-RUN CONTROL. The member MAY be omitted, and a caller that
        sends null has omitted it in a second spelling; neither is malformed, and
        without this row the arms above would pass for a speaker that refused
        every frame."""
        for frame in (
            support.frame({"jsonrpc": "2.0", "id": 4, "method": "ping"}),
            support.frame({"jsonrpc": "2.0", "id": 4, "method": "ping", "params": None}),
            support.frame({"jsonrpc": "2.0", "id": 4, "method": "ping", "params": {}}),
        ):
            with self.subTest(frame):
                responses, _, _ = _drive([frame])
                self.assertEqual(support.result_of(responses, 4), {})

    def test_null_params_on_a_method_that_READS_params_is_the_empty_object(self):
        """THE ROW THAT MEASURES THE NORMALIZATION rather than the tolerance. On a
        method that ignores params -- ping -- ANY empty value passes, so that
        control cannot tell an empty object from an empty list. `tools/call` reads
        params by name, so a normalization to the wrong empty type reaches a
        `.get` on a list and takes the session down."""
        responses, raw, _ = self._drive_with_follow_on(
            support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": None})
        )
        self.assertNotEqual(raw, b"", "the session died on a null params")
        result = support.result_of(responses, 3)
        self.assertIs(result["isError"], True, "a call naming no tool is a tool error, not a crash")
        self.assertIn("NoneType", result["content"][0]["text"])
        self.assertEqual(support.result_of(responses, self.FOLLOW_ON_ID), {})

    def test_the_control_a_well_formed_tools_call_still_reaches_the_tool(self):
        responses, _, _ = _drive([support.call_frame("collect", {"id": "probe"})])
        self.assertNotIn("isError", support.result_of(responses, 3))

    def test_a_malformed_member_leaves_nothing_on_stderr_but_a_named_diagnostic(self):
        _, _, stderr = _drive([support.frame({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": "s"})])
        self.assertIn("params is a str", stderr)

    def test_ping_is_answered_because_it_is_the_base_protocol(self):
        responses, _, _ = _drive([support.frame({"jsonrpc": "2.0", "id": 4, "method": "ping"})])
        self.assertEqual(support.result_of(responses, 4), {})


class ToolListingTest(unittest.TestCase):
    def test_a_tool_with_no_output_schema_omits_the_key_rather_than_sending_null(self):
        definition = ToolDefinition("t", "d", {"type": "object"}, None, lambda a: {})
        self.assertNotIn("outputSchema", definition.listing())

    def test_a_tools_call_naming_a_tool_this_provider_does_not_serve_is_an_error_result(self):
        responses, _, _ = _drive([support.call_frame("nope", {"id": "x"})])
        result = support.result_of(responses, 3)
        self.assertIs(result["isError"], True)
        self.assertIn("nope", result["content"][0]["text"])

    def test_a_speaker_writes_compact_json_with_no_embedded_newline(self):
        """The framing depends on it: a pretty-printed document would be many
        frames, each of them malformed."""
        speaker_frames = [support.initialize_frame()]
        _, raw, _ = _drive(speaker_frames)
        self.assertEqual(raw.count(b"\n"), 1)
        self.assertEqual(json.loads(raw)["id"], 1)


class SpeakerConstructionTest(unittest.TestCase):
    def test_the_speaker_defaults_its_streams_to_the_process_streams(self):
        """A collector main constructs one with no streams at all, so the default
        has to be the process's own."""
        speaker = Speaker("n", "v", [])
        self.assertIsNotNone(speaker._stdin)
        self.assertIsNotNone(speaker._stdout)


if __name__ == "__main__":
    unittest.main()
