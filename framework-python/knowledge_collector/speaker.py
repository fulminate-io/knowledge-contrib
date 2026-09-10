# SPDX-License-Identifier: Apache-2.0

"""speaker.py — the MCP STDIO SPEAKER, hand-rolled on the standard library.

WHY THIS FILE EXISTS AT ALL, because it is the one part of the port with no
counterpart in the Go framework. The Go framework hands its server to an SDK and
the SDK owns the framing, the handshake and the malformed-frame refusals. This
port takes NO RUNTIME DEPENDENCY, so those obligations are its own, and they are
tested as its own: the protocol-version floor, the initialized notification, the
unbounded newline framing, and four malformed-frame arms.

THE SURFACE IS FOUR MESSAGES AND IT IS FIXED BY A CHECKED-IN CONTRACT. The
knowledge client dials a collector, lists its tools, and calls one:

    initialize                 -> the handshake, answering this speaker's version
    notifications/initialized  -> a NOTIFICATION; it is answered with NOTHING
    tools/list                 -> the advertised input and output schemas
    tools/call                 -> the walk

`ping` is answered too, because it is part of the base protocol and costs one
branch. Anything else is method-not-found, by name.

STDOUT IS THE PROTOCOL STREAM. A collector that prints to stdout corrupts the
JSON-RPC framing and surfaces to an operator as an opaque handshake failure;
every diagnostic this package writes goes to stderr, and the port's suite pins
that with a real child process rather than with a reading of this comment.

THE FRAMING IS NEWLINE-DELIMITED JSON WITH NO LENGTH BOUND IN EITHER DIRECTION.
Collector traffic carries no size cap: a result crosses 64 MiB on a real account
long before it is interesting, and a declared foreign-context block crosses 4 MiB
on an ordinary one. `readline()` on a buffered binary stream is unbounded, and one
result is written as ONE newline-terminated line with no chunking of its own. A
fixed-size read or a line-length cap here is the defect the port's own suite
exists to catch, because it fails nowhere else.
"""

import json
import sys

__all__ = [
    "PROTOCOL_VERSION",
    "JSONRPC_VERSION",
    "PARSE_ERROR",
    "INVALID_REQUEST",
    "METHOD_NOT_FOUND",
    "INVALID_PARAMS",
    "INTERNAL_ERROR",
    "ToolDefinition",
    "Speaker",
]

# The protocol revision this speaker ANSWERS. It is a constant of the port, not
# an echo of what the caller asked for: the client requests its own newest
# revision and accepts a range below it, so a speaker that mirrored the request
# would claim to support whatever it was handed.
PROTOCOL_VERSION = "2025-06-18"

JSONRPC_VERSION = "2.0"

# The JSON-RPC 2.0 error codes this speaker emits.
PARSE_ERROR = -32700
INVALID_REQUEST = -32600
METHOD_NOT_FOUND = -32601
INVALID_PARAMS = -32602
INTERNAL_ERROR = -32603


class ToolDefinition:
    """One tool this speaker serves: what `tools/list` advertises and what
    `tools/call` dispatches to.

    `handler` takes the call's decoded `arguments` object and returns the tool
    result document (a dict with any of `content`, `structuredContent`,
    `isError`). It raises to signal a walk failure, which becomes an isError
    result rather than a JSON-RPC error, because the client treats a tool error
    as a refused collect that writes nothing and a transport error as a broken
    provider.
    """

    __slots__ = ("name", "description", "input_schema", "output_schema", "handler")

    def __init__(self, name, description, input_schema, output_schema, handler):
        self.name = name
        self.description = description
        self.input_schema = input_schema
        self.output_schema = output_schema
        self.handler = handler

    def listing(self):
        """The tool as `tools/list` carries it.

        AN ABSENT OUTPUT SCHEMA IS OMITTED RATHER THAN SENT AS null. A provider
        that advertises no output schema is a real shape the client refuses by
        name, and it is reachable here by passing None; sending `"outputSchema":
        null` would be a third thing that is neither.
        """
        out = {"name": self.name, "description": self.description, "inputSchema": self.input_schema}
        if self.output_schema is not None:
            out["outputSchema"] = self.output_schema
        return out


class Speaker:
    """Serves a set of tools over MCP on newline-delimited JSON.

    The streams are parameters rather than module globals so the port's own suite
    can drive a speaker in-process over byte buffers AND as a real child process,
    and get the same code path both times.
    """

    def __init__(self, server_name, server_version, tools, stdin=None, stdout=None, stderr=None):
        self.server_name = server_name
        self.server_version = server_version
        self.tools = {tool.name: tool for tool in tools}
        self._stdin = stdin if stdin is not None else sys.stdin.buffer
        self._stdout = stdout if stdout is not None else sys.stdout.buffer
        self._stderr = stderr if stderr is not None else sys.stderr

    # ---------------------------------------------------------------- the loop --

    def serve(self):
        """Read frames until end of input, answering each. Returns on EOF."""
        while True:
            line = self._stdin.readline()
            if not line:
                return
            self.handle_frame(line)

    def handle_frame(self, line):
        """Answer ONE newline-terminated frame.

        SIX MALFORMED-INPUT ARMS, each its own branch so a mutation that replaces
        one with a skip turns exactly one test red. The repository's hard
        invariant is that bad input always errors with no silent coercion,
        default or degrade, and this reader is the one place in this package that
        reads untrusted bytes.

        THE ARMS ARE AT TWO LEVELS AND THE SECOND LEVEL IS THE ONE THAT BITES.
        Four are about the FRAME -- not JSON, not a request object, an unknown
        method, no id. The fifth is about a MEMBER of a well-formed frame: a
        request object whose `params` is an array, a string or a number. Measured
        on the shipped speaker before this arm existed, each of those raised an
        AttributeError out of serve() and ENDED THE SESSION with zero bytes on
        stdout -- no error for the offending frame and no answer to the
        well-formed frame behind it. A refusal is a RESPONSE, not a disconnect,
        and that property was false for exactly the classes no row drove.
        """
        text = line.decode("utf-8", errors="replace").strip()

        # (1) A frame that is not JSON at all -- including an empty one, which
        # carries no message and is a framing fault rather than a quiet nothing.
        try:
            message = json.loads(text) if text else None
        except ValueError as exc:
            self.log("refusing a frame that is not JSON: %s" % exc)
            self._write(self._error_envelope(None, PARSE_ERROR, "the frame is not JSON: %s" % exc))
            return
        if message is None:
            self.log("refusing an empty frame; a newline carries no JSON-RPC message")
            self._write(self._error_envelope(None, PARSE_ERROR, "the frame carries no JSON-RPC message"))
            return

        # (2) JSON that is not a JSON-RPC request object.
        if not isinstance(message, dict) or "method" not in message or not isinstance(message.get("method"), str):
            self.log("refusing a frame that is not a JSON-RPC request object")
            self._write(
                self._error_envelope(
                    message.get("id") if isinstance(message, dict) else None,
                    INVALID_REQUEST,
                    "the frame is not a JSON-RPC request object; it declares no string `method`",
                )
            )
            return

        method = message["method"]
        has_id = "id" in message and message["id"] is not None

        # (3) A `params` MEMBER that is not an object. JSON-RPC allows params to
        # be by-position (an array) or by-name (an object); MCP is by-name only,
        # so an array, a string or a number here is a caller that cannot be
        # served. It is refused with INVALID_PARAMS naming what arrived, rather
        # than reaching a `.get` on a list and taking the session down with it.
        #
        # ABSENT AND null BOTH MEAN "no params" and are the empty object: the
        # member MAY be omitted, and a caller that sends null has omitted it in a
        # second spelling rather than sent a malformed one.
        params = message.get("params")
        if params is None:
            params = {}
        elif not isinstance(params, dict):
            self.log("refusing a frame whose params is a %s" % type(params).__name__)
            self._write(
                self._error_envelope(
                    message.get("id"),
                    INVALID_PARAMS,
                    "the request's `params` is a %s; this protocol passes parameters BY NAME, so params must be an object"
                    % type(params).__name__,
                )
            )
            return

        # (4) A frame with no id is a NOTIFICATION: it is answered with nothing,
        # which is the protocol rather than a skip, and it is logged so a silent
        # drop is not what an operator sees.
        if not has_id:
            self._handle_notification(method)
            return

        request_id = message["id"]
        if method == "initialize":
            self._write(self._result_envelope(request_id, self._initialize_result()))
            return
        if method == "ping":
            self._write(self._result_envelope(request_id, {}))
            return
        if method == "tools/list":
            self._write(self._result_envelope(request_id, {"tools": [t.listing() for t in self.tools.values()]}))
            return
        if method == "tools/call":
            self._write(self._result_envelope(request_id, self._call_tool(params)))
            return

        # (5) A method this speaker does not serve, named in the refusal.
        self.log("refusing an unknown method %r" % method)
        self._write(self._error_envelope(request_id, METHOD_NOT_FOUND, "unknown method %r" % method))

    def _handle_notification(self, method):
        if method == "notifications/initialized":
            self.log("handshake complete")
            return
        self.log("ignoring the notification %r; a notification carries no id and is answered with nothing" % method)

    # ------------------------------------------------------------- the answers --

    def _initialize_result(self):
        """The handshake answer.

        `serverInfo.name` IS THE TOOL NAME on a single-tool collector, which is
        what the framework's entry point passes in: the client logs it, and a name
        that matched nothing an operator configured would be the one diagnostic
        that helps them least.
        """
        return {
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {"tools": {}},
            "serverInfo": {"name": self.server_name, "version": self.server_version},
        }

    def _call_tool(self, params):
        """Dispatch one `tools/call`, returning the CallToolResult document."""
        name = params.get("name")

        # (6) A tool NAME that is not a string. It is refused before it reaches
        # the tool table, because an unhashable value -- an array, an object --
        # raises a TypeError out of a dict lookup and takes the session with it,
        # which is the same class as the params arm above one level further in.
        if not isinstance(name, str):
            self.log("refusing a tools/call whose name is a %s" % type(name).__name__)
            return self._tool_error(
                "the tools/call `name` is a %s; it names the tool to call and must be a string" % type(name).__name__
            )

        tool = self.tools.get(name)
        if tool is None:
            return self._tool_error("this provider serves no tool named %r; it serves %s" % (name, ", ".join(sorted(self.tools))))
        arguments = params.get("arguments")
        if arguments is None:
            arguments = {}
        try:
            result = tool.handler(arguments)
        except Exception as exc:  # noqa: BLE001 -- a walk failure is a TOOL error
            # A walk failure is a TOOL error, never an empty successful result:
            # the client treats an isError result as a refused collect that writes
            # nothing, while an empty complete result would instead assert a
            # successful walk that found nothing.
            self.log("the tool %r failed: %s" % (name, exc))
            return self._tool_error(str(exc))
        return result

    @staticmethod
    def _tool_error(text):
        return {"content": [{"type": "text", "text": text}], "isError": True}

    # ------------------------------------------------------------ the framing --

    def _result_envelope(self, request_id, result):
        return {"jsonrpc": JSONRPC_VERSION, "id": request_id, "result": result}

    def _error_envelope(self, request_id, code, message):
        return {"jsonrpc": JSONRPC_VERSION, "id": request_id, "error": {"code": code, "message": message}}

    def _write(self, document):
        """Write ONE document as ONE newline-terminated line.

        No chunking of its own and no length bound: a 68 MiB result is one line on
        the wire, exactly as the client's reader expects it.
        """
        payload = json.dumps(document, separators=(",", ":")).encode("utf-8")
        self._stdout.write(payload)
        self._stdout.write(b"\n")
        self._stdout.flush()

    def log(self, message):
        """Every diagnostic goes to STDERR. stdout is the protocol stream."""
        print("%s: %s" % (self.server_name, message), file=self._stderr)
