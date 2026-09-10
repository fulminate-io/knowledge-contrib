# SPDX-License-Identifier: Apache-2.0

"""Row 12 — NOTHING IS WRITTEN TO STDOUT BUT PROTOCOL.

This is the port's highest-consequence discipline. On stdio, stdout IS the
JSON-RPC stream: a stray write corrupts the framing and surfaces to an operator as
an opaque handshake failure with nothing in it pointing at the print that caused
it.

THE OBSERVABLE IS THE WHOLE PROCESS'S STDOUT UNDER A REAL RUN, not a syntactic
scan of this package's own source. That is deliberate and it is the stronger
instrument: it catches a module this port imports printing, which no pattern over
these files could see. The Go framework pins the same behaviour the same way.

A SOURCE SCAN RIDES BESIDE IT ANYWAY, as the cheap half: every `print(` in the
shipped package must carry `file=`. Two instruments, one rule, and the run is the
one that decides.
"""

import ast as pyast
import json
import os
import re
import unittest

from . import support


def _shipped_python_files():
    """Every .py file this port ships, excluding its own test suite."""
    found = []
    for dirpath, dirnames, filenames in os.walk(support.PORT_ROOT):
        dirnames[:] = [d for d in dirnames if d not in ("tests", "__pycache__")]
        for name in sorted(filenames):
            if name.endswith(".py"):
                found.append(os.path.join(dirpath, name))
    return sorted(found)


class NormalRunWritesNothingToStdoutTest(unittest.TestCase):
    def test_a_normal_sample_collector_run_writes_only_protocol_to_stdout(self):
        frames = [
            support.initialize_frame(),
            support.frame({"jsonrpc": "2.0", "method": "notifications/initialized"}),
            support.frame({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
            support.call_frame("collect", {"id": "probe", "params": {"path": support.PACKAGE_ROOT}}),
        ]
        responses, raw, stderr, code = support.drive_child("sample_collector.py", frames, env={})
        self.assertEqual(code, 0)
        self.assertEqual(len(responses), 3, "one answer per request and none for the notification")
        for line in raw.splitlines():
            self.assertEqual(json.loads(line)["jsonrpc"], "2.0")
        self.assertEqual(
            raw.count(b"\n"), 3, "exactly one newline per message; a stray print would add one that does not parse"
        )

    def test_the_diagnostics_the_run_did_produce_went_to_stderr(self):
        """THE SAME-RUN KNOWN POSITIVE. Without it, an empty stdout would be
        equally consistent with a speaker that logs nothing at all, and the row
        above would pass for a process that never spoke."""
        frames = [support.initialize_frame(), support.frame({"jsonrpc": "2.0", "method": "notifications/initialized"})]
        _, raw, stderr, _ = support.drive_child("sample_collector.py", frames, env={})
        self.assertIn("handshake complete", stderr)
        self.assertEqual(len(raw.splitlines()), 1, "and none of it reached stdout")

    def test_a_failing_walks_diagnostic_also_stays_off_stdout(self):
        frames = [
            support.initialize_frame(),
            support.call_frame("collect", {"id": "probe", "params": {"path": "/no/such/directory/here"}}),
        ]
        responses, raw, stderr, code = support.drive_child("sample_collector.py", frames, env={})
        self.assertIs(support.result_of(responses, 3)["isError"], True)
        self.assertIn("is not a directory", stderr, "the failure is logged")
        for line in raw.splitlines():
            json.loads(line)

    def test_a_malformed_frames_diagnostic_also_stays_off_stdout(self):
        responses, raw, stderr, _ = support.drive_child("sample_collector.py", [b"not json at all\n"], env={})
        self.assertEqual(len(responses), 1)
        self.assertEqual(responses[0]["error"]["code"], -32700)
        self.assertIn("not JSON", stderr)


def _prints(source):
    """Every `print(...)` CALL in a source file, as (line, has_file_keyword).

    IT PARSES RATHER THAN GREPS, and the reason is measured: this port's own
    diagnostics are multi-line calls whose `file=sys.stderr` sits several lines
    below the `print(`, so a line-based matcher reports every one of them as an
    offender. A parse sees the call.
    """
    tree = pyast.parse(source)
    found = []
    for node in pyast.walk(tree):
        if not isinstance(node, pyast.Call):
            continue
        target = node.func
        if not (isinstance(target, pyast.Name) and target.id == "print"):
            continue
        found.append((node.lineno, any(kw.arg == "file" for kw in node.keywords)))
    return found


class SourceScanTest(unittest.TestCase):
    """The cheap half. It is a proxy for the run above, and it is labelled as one:
    a `print(` with no `file=` in shipped source is the shape that breaks framing,
    but the run is what decides."""

    def test_every_print_in_the_shipped_package_writes_to_a_named_stream(self):
        offenders = []
        for path in _shipped_python_files():
            for line, has_file in _prints(support.read_text(path)):
                if not has_file:
                    offenders.append("%s:%d" % (os.path.relpath(path, support.PORT_ROOT), line))
        self.assertEqual(offenders, [], "a print with no file= writes to stdout, which is the protocol stream")

    def test_the_control_the_scan_found_files_and_found_prints_in_them(self):
        """A ZERO NEEDS A CONTROL: an empty offender list is equally consistent
        with a walk that opened nothing."""
        files = _shipped_python_files()
        self.assertGreater(len(files), 5, "the walk found almost no source: %s" % files)
        prints = sum(len(_prints(support.read_text(path))) for path in files)
        self.assertGreater(prints, 0, "there are prints in this port; the matcher must be seeing them")

    def test_the_matcher_fires_on_the_bad_shape_and_is_silent_on_the_good_one(self):
        self.assertEqual(_prints('print("diagnostic")'), [(1, False)])
        self.assertEqual(_prints('print("diagnostic", file=sys.stderr)'), [(1, True)])
        self.assertEqual(_prints('print(\n    "diagnostic",\n    file=sys.stderr,\n)'), [(1, True)])


class NoNetworkTest(unittest.TestCase):
    """Row 23 — the tests make no network call.

    A NEGATIVE NEEDS A POSITIVE, so this is two assertions rather than a green
    run: the shipped package imports no networking module, and it carries no URL.
    The suite's own inputs are this repository's files and a temporary directory.
    """

    NETWORK_IMPORTS = ("socket", "http.client", "urllib", "ssl", "ftplib", "smtplib", "telnetlib", "asyncio")
    URL = re.compile(r"https?://")

    def test_the_shipped_package_imports_no_networking_module(self):
        offenders = []
        for path in _shipped_python_files():
            source = support.read_text(path)
            for module in self.NETWORK_IMPORTS:
                if re.search(r"^\s*(?:import|from)\s+%s\b" % re.escape(module), source, re.MULTILINE):
                    offenders.append("%s imports %s" % (os.path.relpath(path, support.PORT_ROOT), module))
        self.assertEqual(offenders, [])

    def test_the_shipped_package_carries_no_url_outside_the_contract_schemas(self):
        """The two contract files carry the JSON Schema `$schema` URI, which is an
        identifier rather than a fetch: this port's validator resolves only local
        references and refuses a remote one by name."""
        offenders = []
        for path in _shipped_python_files():
            for number, line in enumerate(support.read_text(path).splitlines(), start=1):
                if self.URL.search(line):
                    offenders.append("%s:%d" % (os.path.relpath(path, support.PORT_ROOT), number))
        self.assertEqual(offenders, [])

    def test_the_control_the_url_matcher_does_fire(self):
        self.assertTrue(self.URL.search("https://json-schema.org/draft/2020-12/schema"))

    def test_a_remote_schema_reference_is_refused_by_name_rather_than_fetched(self):
        from knowledge_collector.jsonschema import SchemaError, validate

        with self.assertRaises(SchemaError) as caught:
            validate({}, {"$ref": "https://example.invalid/schema.json"})
        self.assertIn("local reference", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
