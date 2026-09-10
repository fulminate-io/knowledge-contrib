# SPDX-License-Identifier: Apache-2.0

"""Row 25 — the sample collector is driven by the REAL CLIENT BINARY.

`knowledge collector add` DIALS the provider it registers before it writes
anything: it completes the MCP handshake, lists the provider's tools, finds the one
the entry names and runs the client's own schema gate against that tool's
advertised schemas. So this row is the port's speaker and both advertised schemas
measured against the consumer that actually judges them, rather than against this
port's copy of the consumer's rules.

IT NEEDS THE BINARY AND SKIPS BY NAME WITHOUT IT. The CI leg builds the client and
hands the path over in KNOWLEDGE_BIN; a run with no binary reports a named skip
rather than a quieter green.

IT TOUCHES NO SHARED STATE. Every invocation runs under a scratch HOME with an
environment of its own, so the operator's own daemon, configuration and collector
entries are never read or written.
"""

import os
import subprocess
import tempfile
import unittest

from . import support

_SAMPLE = os.path.join(support.PORT_ROOT, "sample_collector.py")
_STUB = os.path.join(support.PORT_ROOT, "conformance_stub.py")


def _client(test):
    binary = os.environ.get("KNOWLEDGE_BIN", "")
    if not binary or not os.path.isfile(binary):
        test.skipTest(
            "KNOWLEDGE_BIN names no client binary (%r). This row dials the port with the real client, so it needs one; "
            "the CI leg builds it and sets the variable, and a local run builds it with "
            "`cd cmd/knowledge && go build -o /tmp/knowledge .`" % binary
        )
    return binary


def _add(test, binary, scratch, name, command, *args, env_pairs=(), tool="collect"):
    """Run `collector add` under a scratch HOME and return the completed process."""
    argv = [binary, "collector", "add", "--tool", tool]
    for pair in env_pairs:
        argv += ["-e", pair]
    argv += [name, "--", command, *args]
    return subprocess.run(
        argv,
        cwd=scratch,
        capture_output=True,
        timeout=120,
        env={"HOME": scratch, "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "TMPDIR": tempfile.gettempdir()},
    )


class ClientRegistrationTest(unittest.TestCase):
    def test_the_client_dials_the_sample_collector_and_writes_the_entry(self):
        binary = _client(self)
        import sys

        with tempfile.TemporaryDirectory() as scratch:
            completed = _add(self, binary, scratch, "sample-py", sys.executable, _SAMPLE)
            self.assertEqual(
                completed.returncode,
                0,
                "collector add failed:\nstdout: %s\nstderr: %s"
                % (completed.stdout.decode("utf-8", "replace"), completed.stderr.decode("utf-8", "replace")),
            )
            entry = _entry(scratch, "sample-py")
            self.assertEqual(entry["type"], "stdio")
            self.assertEqual(entry["command"], sys.executable)
            self.assertIn(_SAMPLE, entry["args"])
            self.assertEqual(entry["tool"], "collect")

    def test_the_dial_is_what_passes_the_clients_schema_gate(self):
        """THE SAME-RUN CONTROL, and it is the whole point of the row: the same
        client refuses a provider whose advertised schemas do not satisfy the
        contract. The conformance stub in bad-output-schema mode is that provider,
        and the refusal names the missing completeness assertion."""
        binary = _client(self)
        import sys

        with tempfile.TemporaryDirectory() as scratch:
            completed = _add(
                self,
                binary,
                scratch,
                "stub-bad",
                sys.executable,
                _STUB,
                env_pairs=("FUL1776_STUB_MODE=bad-output-schema",),
                tool="collect_graph",
            )
            self.assertNotEqual(completed.returncode, 0, "a provider missing walk_complete must be refused")
            message = (completed.stdout + completed.stderr).decode("utf-8", "replace")
            self.assertIn("walk_complete", message)

    def test_the_conforming_stub_passes_the_same_gate(self):
        """And the positive half through the same instrument, so the refusal above
        is a gate that judges rather than one that refuses everything."""
        binary = _client(self)
        import sys

        with tempfile.TemporaryDirectory() as scratch:
            completed = _add(
                self,
                binary,
                scratch,
                "stub-good",
                sys.executable,
                _STUB,
                env_pairs=("FUL1776_STUB_MODE=conforming",),
                tool="collect_graph",
            )
            self.assertEqual(
                completed.returncode,
                0,
                (completed.stdout + completed.stderr).decode("utf-8", "replace"),
            )

    def test_the_client_refuses_a_provider_serving_no_such_tool(self):
        binary = _client(self)
        import sys

        with tempfile.TemporaryDirectory() as scratch:
            completed = _add(
                self,
                binary,
                scratch,
                "stub-missing",
                sys.executable,
                _STUB,
                env_pairs=("FUL1776_STUB_MODE=no-such-tool",),
                tool="collect_graph",
            )
            self.assertNotEqual(completed.returncode, 0)


def _entry(scratch, name):
    """The written entry, read back out of the scratch HOME's collector config."""
    import json

    for relative in (
        os.path.join(".knowledge", "collectors.json"),
        os.path.join(".config", "knowledge", "collectors.json"),
    ):
        path = os.path.join(scratch, relative)
        if os.path.isfile(path):
            document = json.loads(support.read_text(path))
            entries = document.get("collectors", document)
            if name in entries:
                return entries[name]
            for candidate in entries if isinstance(entries, list) else []:
                if candidate.get("name") == name:
                    return candidate
    raise AssertionError(
        "no collector entry named %r was written under %s; the files there are %s"
        % (name, scratch, sorted(_walk_files(scratch)))
    )


def _walk_files(root):
    for dirpath, _, filenames in os.walk(root):
        for name in filenames:
            yield os.path.relpath(os.path.join(dirpath, name), root)


if __name__ == "__main__":
    unittest.main()
