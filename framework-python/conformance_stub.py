#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""conformance_stub.py — the PYTHON CONFORMANCE STUB the knowledge client's own
provider-dialing contract tests drive.

WHAT IT IS FOR. The client carries a stub MCP provider of its own, re-execed out
of its test binary, and eleven of its tests dial that stub and assert the exact
bytes it returns. This file is that stub, in Python, so those eleven can be
pointed at a real non-Go collector process and prove the client's gates against
one. IT IS NOT THIS PORT'S SAMPLE COLLECTOR: the sample walks a directory and is
the README's worked example; this answers with fixed payloads and exists only to
be refused and admitted in named ways.

IT IS A TRANSCRIPTION AND IT IS PINNED AS ONE. Every payload, literal and exit
code below is the client stub's, and the port's own suite reads the client's stub
source and compares -- so a change on that side turns a named test red here rather
than turning eleven distant tests into a mystery.

HOW THE MODE ARRIVES. The client stub switches on an environment variable, and
the registration entry's env block is the child's WHOLE environment, so that
variable is the one channel guaranteed to reach a spawned collector. This stub
reads the same name. A child that is spawned with no mode EXITS LOUD rather than
guessing one.

THE SPAWN RECORD. `--spawn-log <path>` appends one line per spawn. It exists
because a harness that falls back to its own in-process stub when an external
command is unset or unreachable produces a run that is IDENTICAL in every count to
a run that really spawned this process -- so the pass count is evidence for
neither, and this file is the positive tell that separates them. The line carries
the mode and the process id and NO ENVIRONMENT VALUE: what it records is that a
spawn happened, never what the environment held.

THE DIAGNOSTICS ALL GO TO STDERR. stdout is the protocol stream; a stray print
corrupts the framing and surfaces to an operator as an opaque handshake failure.
"""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from knowledge_collector.describe import DESCRIBE_TOOL_NAME  # noqa: E402
from knowledge_collector.schema import (  # noqa: E402 -- the path insert above is what makes this importable uninstalled
    describe_contract_json,
    input_contract_json,
    output_contract_json,
)
from knowledge_collector.speaker import Speaker, ToolDefinition  # noqa: E402

# The switch variable, and the tool-name override beside it. Both are the client
# stub's own spellings: this stub is spawned by the client's harness, so the names
# are that harness's to choose and this file's to match.
STUB_MODE_ENV = "FUL1776_STUB_MODE"
STUB_TOOL_ENV = "FUL1776_STUB_TOOL"

DEFAULT_STUB_TOOL = "collect_graph"
OTHER_TOOL = "some_other_tool"

# The one environment NAME the stub's declaration declares, and the client stub's
# own spelling of it. It is a name and never a value: what the declaration says
# is that this collector reads it, which is what an installer acts on.
DECLARED_ENV = "FUL1776_DECLARED"

# The modes. Each is one shape the contract must be proven against. THE COUNT IS
# DELIBERATELY NOT WRITTEN HERE OR IN THE PIN THAT READS IT: the client's own
# const block is the authority, and a count beside a list is a second thing to
# keep true.
MODE_CONFORMING = "conforming"
MODE_ENV_REPORT = "env-report"
MODE_NO_OUTPUT_SCHEMA = "no-output-schema"
MODE_BAD_OUTPUT_SCHEMA = "bad-output-schema"
MODE_BAD_INPUT_SCHEMA = "bad-input-schema"
MODE_BREAKS_OWN_WORD = "breaks-own-word"
MODE_TOOL_ERROR = "tool-error"
MODE_EMPTY_NODE_TYPE = "empty-node-type"
MODE_UNKNOWN_FIELD = "unknown-field"
MODE_DANGLING_EDGE = "dangling-edge"
MODE_INCOMPLETE_WALK = "incomplete-walk"
MODE_EMPTY_GRAPH = "empty-graph"
MODE_OVER_FORMER_CAP = "over-former-cap"
MODE_EXIT_BEFORE_HANDSHAKE = "exit-before-handshake"
MODE_EXIT_MID_SESSION = "exit-mid-session"
MODE_NO_SUCH_TOOL = "no-such-tool"
# The four DESCRIBE arms. The describe tool is REQUIRED, so each of these is one
# way a provider fails the requirement rather than a variation on serving it.
MODE_NO_DESCRIBE = "no-describe"
MODE_BAD_DESCRIBE_SCHEMA = "bad-describe-schema"
MODE_BAD_DECLARATION = "bad-declaration"
MODE_DESCRIBE_ERROR = "describe-error"

MODES = (
    MODE_CONFORMING,
    MODE_ENV_REPORT,
    MODE_NO_OUTPUT_SCHEMA,
    MODE_BAD_OUTPUT_SCHEMA,
    MODE_BAD_INPUT_SCHEMA,
    MODE_BREAKS_OWN_WORD,
    MODE_TOOL_ERROR,
    MODE_EMPTY_NODE_TYPE,
    MODE_UNKNOWN_FIELD,
    MODE_DANGLING_EDGE,
    MODE_INCOMPLETE_WALK,
    MODE_EMPTY_GRAPH,
    MODE_OVER_FORMER_CAP,
    MODE_EXIT_BEFORE_HANDSHAKE,
    MODE_EXIT_MID_SESSION,
    MODE_NO_SUCH_TOOL,
    MODE_NO_DESCRIBE,
    MODE_BAD_DESCRIBE_SCHEMA,
    MODE_BAD_DECLARATION,
    MODE_DESCRIBE_ERROR,
)

# The literal a refused collect carries, transcribed from the client stub.
TOOL_ERROR_TEXT = "the stub provider refused this collect"

# The two process exits, transcribed from the client stub.
EXIT_BEFORE_HANDSHAKE_CODE = 3
EXIT_MID_SESSION_CODE = 7
# The exit a child spawned with no mode takes, rather than guessing one.
EXIT_NO_MODE = 9

# The over-former-cap body: 68 nodes of 1 MiB each, chained by 67 edges. It exists
# because the client retired a 64 MiB bound and the only way to prove a bound is
# gone is to cross it with real bytes.
OVER_FORMER_CAP_BODY_LEN = 1 << 20
OVER_FORMER_CAP_NODE_ROWS = 68


def conforming_payload():
    """The reference result: three nodes, one edge, a complete walk.

    ISSUE-1 carries every optional field populated; ISSUE-2 OMITS them; ISSUE-3
    carries them PRESENT AND EMPTY. The last two are different inputs rather than
    a duplicate: absent and empty-valued reach the JSON Schema validation and the
    strict decode differently, and the client runs both over this payload.
    """
    return {
        "nodes": [
            {
                "id": "ISSUE-1",
                "type": "issue",
                "symbol_name": "Login broken",
                "file_path": "src/login.go",
                "language": "go",
                "start_line": 10,
                "end_line": 20,
                "content": "body",
                "signature": "sig",
                "summary": "one line",
                "description": "longer",
                "source": "stub",
                "status": "open",
                "keywords": "login auth",
                "is_exported": True,
                "metadata": {"priority": "high"},
            },
            {"id": "ISSUE-2", "type": "issue"},
            {
                "id": "ISSUE-3",
                "type": "issue",
                "symbol_name": "",
                "file_path": "",
                "language": "",
                "content": "",
                "signature": "",
                "summary": "",
                "description": "",
                "source": "",
                "status": "",
                "keywords": "",
                "start_line": 0,
                "end_line": 0,
                "is_exported": False,
                "metadata": {},
            },
        ],
        "edges": [{"from_id": "ISSUE-1", "to_id": "ISSUE-2", "type": "blocks"}],
        "walk_complete": True,
    }


def over_former_cap_payload():
    body = "x" * OVER_FORMER_CAP_BODY_LEN
    nodes = []
    edges = []
    for index in range(OVER_FORMER_CAP_NODE_ROWS):
        nodes.append({"id": "big-%d" % index, "type": "blob", "content": body})
        if index > 0:
            edges.append({"from_id": "big-%d" % (index - 1), "to_id": "big-%d" % index, "type": "NEXT"})
    return {"nodes": nodes, "edges": edges, "walk_complete": True}


def env_report_payload(arguments):
    """Answer the environment probe.

    It reports ONLY the names the caller asked about, one node each, carrying
    whether the variable is present in this child's environment and -- for a
    present one -- its value.

    IT NEVER SERIALIZES THE WHOLE ENVIRONMENT. os.environ holds everything, so a
    stub that dumped it into a result the collect then admitted would write
    whatever the process held into a graph -- the very shape the child-environment
    contract exists to close, reproduced inside its own test.
    """
    names = []
    params = (arguments or {}).get("params")
    if isinstance(params, dict) and isinstance(params.get("names"), list):
        names = [name for name in params["names"] if isinstance(name, str)]
    nodes = []
    for name in names:
        present = name in os.environ
        metadata = {"present": "true" if present else "false"}
        if present:
            metadata["value"] = os.environ[name]
        nodes.append({"id": name, "type": "env_var", "metadata": metadata})
    return {"nodes": nodes, "edges": [], "walk_complete": True}


def stub_payload(mode, arguments):
    if mode == MODE_ENV_REPORT:
        return env_report_payload(arguments)
    if mode == MODE_EMPTY_NODE_TYPE:
        return {"nodes": [{"id": "n1", "type": ""}], "edges": [], "walk_complete": True}
    if mode == MODE_UNKNOWN_FIELD:
        return {
            "nodes": [{"id": "n1", "type": "issue", "summry": "typo'd key"}],
            "edges": [],
            "walk_complete": True,
        }
    if mode == MODE_DANGLING_EDGE:
        return {
            "nodes": [{"id": "n1", "type": "issue"}],
            "edges": [{"from_id": "n1", "to_id": "absent", "type": "blocks"}],
            "walk_complete": True,
        }
    if mode == MODE_INCOMPLETE_WALK:
        return {"nodes": [{"id": "n1", "type": "issue"}], "edges": [], "walk_complete": False}
    if mode == MODE_EMPTY_GRAPH:
        return {"nodes": [], "edges": [], "walk_complete": True}
    if mode == MODE_OVER_FORMER_CAP:
        return over_former_cap_payload()
    if mode == MODE_BREAKS_OWN_WORD:
        # No summary, which the schema this stub advertised requires.
        return {"nodes": [{"id": "n1", "type": "issue"}], "edges": [], "walk_complete": True}
    return conforming_payload()


def stub_input_schema(mode):
    if mode == MODE_BAD_INPUT_SCHEMA:
        # Type object, but it does not require the collect id -- the one thing the
        # contract's input side insists on.
        return {"type": "object", "properties": {"params": {"type": "object"}}}
    return json.loads(input_contract_json())


def stub_output_schema(mode):
    if mode == MODE_NO_OUTPUT_SCHEMA:
        return None
    if mode == MODE_BAD_OUTPUT_SCHEMA:
        # Conforming but for the completeness assertion, which is exactly the
        # omission the contract exists to refuse.
        out = json.loads(output_contract_json())
        out["required"] = ["nodes", "edges"]
        del out["properties"]["walk_complete"]
        return out
    if mode == MODE_BREAKS_OWN_WORD:
        # STRICTER than the contract: every node must also carry a summary. The
        # handler then returns one that does not.
        out = json.loads(output_contract_json())
        out["properties"]["nodes"]["items"]["required"] = ["id", "type", "summary"]
        return out
    return json.loads(output_contract_json())


def stub_handler(mode, log):
    def handle(arguments):
        if mode == MODE_EXIT_MID_SESSION:
            # Dies WITH THE CALL IN FLIGHT. An immediate process exit rather than
            # a raised exception: the arm under test is a provider that stops
            # existing, not one that reports a failure, and those reach different
            # code in the client.
            log("exiting mid-session, deliberately")
            sys.stderr.flush()
            os._exit(EXIT_MID_SESSION_CODE)
        if mode == MODE_TOOL_ERROR:
            return {"content": [{"type": "text", "text": TOOL_ERROR_TEXT}], "isError": True}
        return {"content": [], "structuredContent": stub_payload(mode, arguments)}

    return handle


def stub_describe_schema(mode):
    """The describe output schema this mode advertises.

    THE BAD ARM IS CONFORMING BUT FOR THE NODE VOCABULARY, which is the half the
    ingest refusal reads: a provider that does not declare it declares nothing the
    server can refuse against. It is the client stub's own shape, transcribed the
    way every other expectation in this file is.
    """
    document = json.loads(describe_contract_json())
    if mode == MODE_BAD_DESCRIBE_SCHEMA:
        document["required"] = ["behavior", "edge_types", "environment"]
        document["properties"].pop("node_types", None)
    return document


def stub_declaration_document():
    """The conforming declaration every mode but the two describe-failure arms
    returns."""
    return {
        "behavior": {
            "summarizable": True,
            "embeddable": False,
            "syncable": True,
            "embed_fields": ["summary"],
        },
        "node_types": ["issue", "epic"],
        "edge_types": ["blocks"],
        "environment": [{"name": DECLARED_ENV, "class": "selector"}],
    }


def stub_describe_handler(mode):
    """Answer the describe tool for one mode.

    IT GOES THROUGH THE RAW SPEAKER for the same reason the collect handler does:
    the framework's own serve path validates the declaration it returns, which
    would make the two break-its-own-word arms untestable.
    """

    def handle(_arguments):
        if mode == MODE_DESCRIBE_ERROR:
            return {
                "content": [{"type": "text", "text": "the collector cannot describe itself right now"}],
                "isError": True,
            }
        document = stub_declaration_document()
        if mode == MODE_BAD_DECLARATION:
            document.pop("behavior")
        return {"content": [], "structuredContent": document}

    return handle


def build_speaker(mode, tool_name="", stdin=None, stdout=None, stderr=None):
    """The stub's speaker for one mode.

    THE RAW SHAPE IS THE POINT. Four of the sixteen modes must advertise a schema
    and then break it, so the stub goes to the speaker directly rather than
    through the framework's serve path, which validates the call's params against
    the schema it advertised. The Go framework needs an unexported escape from its
    SDK's generic handler for exactly this; a hand-rolled speaker IS that escape by
    construction and needs no second code path.
    """
    name = tool_name or DEFAULT_STUB_TOOL
    if mode == MODE_NO_SUCH_TOOL:
        name = OTHER_TOOL
    tools = [
        ToolDefinition(
            name=name,
            description="stub custom collector",
            input_schema=stub_input_schema(mode),
            output_schema=stub_output_schema(mode),
            handler=None,
        )
    ]
    # MODE_NO_DESCRIBE SERVES THE COLLECT TOOL ALONE: it is every provider written
    # before the describe tool was required, and the arm that proves the
    # requirement is a requirement.
    if mode != MODE_NO_DESCRIBE:
        tools.append(
            ToolDefinition(
                name=DESCRIBE_TOOL_NAME,
                description="stub declaration",
                input_schema={"type": "object", "properties": {}},
                output_schema=stub_describe_schema(mode),
                handler=stub_describe_handler(mode),
            )
        )
    speaker = Speaker("ful1776-stub", "v1", tools, stdin=stdin, stdout=stdout, stderr=stderr)
    speaker.tools[name].handler = stub_handler(mode, speaker.log)
    return speaker


def spawn_log_path(argv):
    """Read `--spawn-log <path>` out of argv. Returns "" when it is absent."""
    for index, arg in enumerate(argv):
        if arg == "--spawn-log" and index + 1 < len(argv):
            return argv[index + 1]
        if arg.startswith("--spawn-log="):
            return arg.split("=", 1)[1]
    return ""


def record_spawn(path, mode):
    """Append ONE line recording that this process ran, carrying the mode and the
    pid and NO ENVIRONMENT VALUE."""
    if not path:
        return
    with open(path, "a", encoding="utf-8") as handle:
        handle.write("spawn mode=%s pid=%d\n" % (mode, os.getpid()))


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    mode = os.environ.get(STUB_MODE_ENV, "")
    if mode == "":
        print(
            "conformance stub: spawned with %s UNSET -- the entry's env block did not reach this process. "
            "Exiting rather than guessing a mode." % STUB_MODE_ENV,
            file=sys.stderr,
        )
        return EXIT_NO_MODE
    if mode not in MODES:
        print(
            "conformance stub: %s is %r, which is not one of the %d modes this stub emulates: %s"
            % (STUB_MODE_ENV, mode, len(MODES), ", ".join(MODES)),
            file=sys.stderr,
        )
        return EXIT_NO_MODE

    record_spawn(spawn_log_path(argv), mode)

    if mode == MODE_EXIT_BEFORE_HANDSHAKE:
        print("conformance stub: exiting before the handshake, deliberately", file=sys.stderr)
        return EXIT_BEFORE_HANDSHAKE_CODE

    build_speaker(mode, os.environ.get(STUB_TOOL_ENV, "")).serve()
    return 0


if __name__ == "__main__":
    sys.exit(main())
