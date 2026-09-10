# SPDX-License-Identifier: Apache-2.0

"""support.py — locating the artifacts the suite reads, and driving a speaker.

TWO LOCATORS AND WHY EACH HAS TWO CANDIDATES. This port ships as SOURCE into a
published repository that carries none of the knowledge client's tree, so a test
that byte-compares against the client's checked-in files has to find them in two
different layouts and say so when it can find them in neither:

  * IN THIS REPOSITORY the client's contract directory sits at
    cmd/knowledge/internal/externalcollector/contract, found by walking up.
  * IN THE PUBLISHED REPOSITORY the Go framework's own publish step materializes
    the client's two schema files inside its testdata, at
    framework/testdata/cmd/knowledge/internal/externalcollector/contract, and
    re-points its symlink at them. That copy is a real authoritative one -- the
    publish refuses to stage it when it has drifted -- so this port reads it there
    rather than shipping a second staging arrangement of its own.

NO testdata SYMLINK, deliberately. The Go framework carries one because a Go test
reading a file outside its own module gets a CACHED PASS after that file changes,
and the symlink is what makes the input visible to the build cache. Python has no
test cache and therefore no such blindness, and a symlink under this port would
dangle in the published tree, where nothing re-points it.
"""

import io
import json
import os
import subprocess
import sys

PORT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PACKAGE_ROOT = os.path.join(PORT_ROOT, "knowledge_collector")
CONTRACT_DIR = os.path.join(PACKAGE_ROOT, "contract")

# The client's contract path, spelled once. Both candidates below end in it.
_CLIENT_CONTRACT_RELATIVE = os.path.join("cmd", "knowledge", "internal", "externalcollector", "contract")


def repo_root():
    """The knowledge repository root, or None when this port is running from a
    published copy that does not carry one."""
    path = PORT_ROOT
    while True:
        if os.path.isdir(os.path.join(path, _CLIENT_CONTRACT_RELATIVE)):
            return path
        parent = os.path.dirname(path)
        if parent == path:
            return None
        path = parent


def client_contract_dir():
    """The authoritative copy of the client's contract schemas, or None.

    Candidate 1 is this repository. Candidate 2 is the published layout, where the
    Go framework's publish step materializes the same files inside its testdata.
    """
    root = repo_root()
    if root is not None:
        return os.path.join(root, _CLIENT_CONTRACT_RELATIVE)
    published = os.path.join(os.path.dirname(PORT_ROOT), "framework", "testdata", _CLIENT_CONTRACT_RELATIVE)
    if os.path.isdir(published):
        return published
    return None


def client_stub_source():
    """The client's own stub provider source, or None when it is out of reach.

    It is the authority for every payload, literal and exit code the port's
    conformance stub transcribes.
    """
    root = repo_root()
    if root is None:
        return None
    path = os.path.join(root, "cmd", "knowledge", "internal", "externalcollector", "stubprovider_test.go")
    return path if os.path.isfile(path) else None


def require_client_contract(test):
    """Return the client's contract directory or SKIP BY NAME, saying which two
    layouts were looked in."""
    found = client_contract_dir()
    if found is None:
        test.skipTest(
            "the knowledge client's contract directory is out of reach from %s: neither an ancestor carrying %s "
            "nor the published layout's ../framework/testdata/%s exists. This copy of the port cannot byte-compare "
            "against the client's files." % (PORT_ROOT, _CLIENT_CONTRACT_RELATIVE, _CLIENT_CONTRACT_RELATIVE)
        )
    return found


def require_repo_root(test):
    """Return the repository root or SKIP BY NAME."""
    root = repo_root()
    if root is None:
        test.skipTest(
            "this port is running outside the knowledge repository (no ancestor of %s carries %s), so the "
            "in-repository artifact this test reads is not present" % (PORT_ROOT, _CLIENT_CONTRACT_RELATIVE)
        )
    return root


def read_bytes(path):
    with open(path, "rb") as handle:
        return handle.read()


def read_text(path):
    with open(path, "r", encoding="utf-8") as handle:
        return handle.read()


def frame(document):
    """One newline-terminated JSON-RPC frame."""
    return json.dumps(document).encode("utf-8") + b"\n"


def initialize_frame(request_id=1, requested_version="2026-07-28"):
    return frame(
        {
            "jsonrpc": "2.0",
            "id": request_id,
            "method": "initialize",
            "params": {"protocolVersion": requested_version, "capabilities": {}, "clientInfo": {"name": "t", "version": "v"}},
        }
    )


def call_frame(tool, arguments, request_id=3):
    return frame({"jsonrpc": "2.0", "id": request_id, "method": "tools/call", "params": {"name": tool, "arguments": arguments}})


def drive_in_process(speaker_factory, frames):
    """Drive a speaker over byte buffers in this process and return
    (responses, stdout_bytes, stderr_text).

    The speaker takes its streams as parameters, so this is the same code path a
    real child runs, not an approximation of it.
    """
    stdin = io.BytesIO(b"".join(frames))
    stdout = io.BytesIO()
    stderr = io.StringIO()
    speaker = speaker_factory(stdin, stdout, stderr)
    speaker.serve()
    raw = stdout.getvalue()
    responses = [json.loads(line) for line in raw.splitlines() if line.strip()]
    return responses, raw, stderr.getvalue()


def drive_child(script, frames, env=None, argv=(), timeout=120):
    """Drive a real child process over stdio and return
    (responses, stdout_bytes, stderr_text, returncode).

    THE ENVIRONMENT IS THE BLOCK AND ONLY THE BLOCK, exactly as the daemon spawns
    a collector: a child receives what the entry declared and nothing else. The
    interpreter path is this one, so no PATH lookup is needed.
    """
    process = subprocess.Popen(
        [sys.executable, os.path.join(PORT_ROOT, script), *argv],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=dict(env or {}),
        cwd=PORT_ROOT,
    )
    out, err = process.communicate(b"".join(frames), timeout=timeout)
    responses = [json.loads(line) for line in out.splitlines() if line.strip()]
    return responses, out, err.decode("utf-8", errors="replace"), process.returncode


def result_of(responses, request_id):
    """The `result` of the response carrying this id, or None."""
    for response in responses:
        if response.get("id") == request_id and "result" in response:
            return response["result"]
    return None


def error_of(responses, request_id):
    """The `error` of the response carrying this id, or None."""
    for response in responses:
        if response.get("id") == request_id and "error" in response:
            return response["error"]
    return None
