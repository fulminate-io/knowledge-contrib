# SPDX-License-Identifier: Apache-2.0

"""serve.py — the ENTRY POINT: one collector, two tools, one speaker.

A collector main calls `serve_stdio(MyCollector())` and never sees anything else
in this package. `new_speaker` is exported beside it because a test, and a host
that owns its own streams, needs the value rather than the loop.

WHY THE PARAMS VALIDATION LIVES HERE AND IS LOAD-BEARING. The Go framework hands
its arguments to an SDK that validates them against the advertised input schema
before the handler runs. The Python MCP SDK does not -- measured: a low-level
server advertising the contract input document verbatim was reached with
arguments carrying no `id` at all and with `params` declared an object and sent as
a string, both without complaint. So the port validates its own, against the same
spliced document it advertised, before the walk runs. Delete that call and a
collector author debugs a walk on garbage.

STDOUT IS THE PROTOCOL STREAM. Every diagnostic goes to stderr; see speaker.py.
"""

import sys

from .context import ForeignContext
from .describe import DESCRIBE_TOOL_NAME, validate_declaration
from .envelope import encode_result
from .framework import DEFAULT_TOOL_NAME, ToolSpec
from .jsonschema import ValidationError, validate
from .schema import advertised_input_schema, advertised_output_schema, describe_contract_json
from .speaker import Speaker, ToolDefinition

import json

__all__ = ["IMPLEMENTATION_VERSION", "resolved_tool_name", "new_speaker", "serve_stdio"]

# Identifies this framework in the MCP handshake. A consumer logs it, so it names
# the serving layer rather than the package path.
IMPLEMENTATION_VERSION = "v1"


def resolved_tool_name(spec):
    """The tool name this collector serves: its own, or the default when it names
    none."""
    if spec is None or not getattr(spec, "name", ""):
        return DEFAULT_TOOL_NAME
    return spec.name


def new_speaker(collector, stdin=None, stdout=None, stderr=None):
    """Build the speaker one collector serves: the collect tool carrying the
    contract's advertised schemas with this collector's params spliced in, and the
    fixed-name describe tool carrying this collector's declaration.

    It is exported because a collector with its own transport story (a test, an
    embedding host) needs the value; a collector main calls serve_stdio.
    """
    if collector is None:
        raise ValueError("framework: no collector was supplied")

    spec = collector.tool()
    if spec is None:
        spec = ToolSpec()
    name = resolved_tool_name(spec)
    input_schema = advertised_input_schema(collector)
    output_schema = advertised_output_schema()

    collect_tool = ToolDefinition(
        name=name,
        description=spec.description,
        input_schema=input_schema,
        output_schema=output_schema,
        handler=_collect_handler(collector, name, input_schema),
    )
    describe_tool = ToolDefinition(
        name=DESCRIBE_TOOL_NAME,
        description="Describe this collector: its behaviour defaults, its per-node-type overrides, the node and edge vocabulary it emits, the environment names it reads with each name's class, and the foreign-graph context it needs.",
        input_schema={"type": "object", "properties": {}},
        output_schema=json.loads(describe_contract_json()),
        handler=_describe_handler(collector, name),
    )

    # serverInfo.name is the RESOLVED TOOL NAME, which is what an operator wrote
    # in their entry and therefore the one identity that helps them when a dial
    # fails.
    return Speaker(name, IMPLEMENTATION_VERSION, [collect_tool, describe_tool], stdin=stdin, stdout=stdout, stderr=stderr)


def _collect_handler(collector, name, input_schema):
    def handle(arguments):
        # (1) THE PARAMS GATE. It runs against the same document this tool
        # advertised, so a caller is held to the collector's own declaration and
        # not to a second transcription of it.
        try:
            validate(arguments, input_schema, "arguments")
        except ValidationError as exc:
            raise ValueError("%s collector: the call arguments do not satisfy this tool's advertised input schema: %s" % (name, exc)) from exc

        # (2) The schema requires the id to be PRESENT and a string; it cannot
        # require it to be non-empty. An empty collect id names no graph instance,
        # so it is refused here rather than walked and discarded downstream.
        collect_id = arguments.get("id", "")
        if collect_id == "":
            raise ValueError("%s collector: the collect id is empty; it names the graph instance this result lands in" % name)

        foreign = ForeignContext.from_wire(arguments.get("context"))
        result = collector.walk(collect_id, arguments.get("params"), foreign)
        return {"content": [], "structuredContent": encode_result(name, result)}

    return handle


def _describe_handler(collector, name):
    def handle(_arguments):
        declaration = collector.describe()
        if declaration is None:
            raise ValueError(
                "%s collector: describe() returned nothing; the declaration carries the node and edge vocabulary "
                "this collector emits, and an empty one is a claim rather than an absence" % name
            )
        # THE DECLARATION NAMES NO TOOL, so there is nothing here to reconcile
        # against the served name. The entry's `tool` field is filled from the
        # tool the client LISTED, which is the only spelling that cannot disagree
        # with what is actually served.
        document = declaration.render()
        validate_declaration(document, json.loads(describe_contract_json()))
        return {"content": [], "structuredContent": document}

    return handle


def serve_stdio(collector):
    """Serve this collector over MCP on stdin/stdout and block until the session
    ends.

    It is the entry point for a collector installed as a `type: stdio` config
    entry, which is the shape a daemon spawns. There is no `--version` switch: the
    knowledge client never reads a collector's banner, so a port that answered one
    would be answering nobody.
    """
    new_speaker(collector).serve()
    return 0


def main(collector):
    """A collector main's one line: `sys.exit(main(MyCollector()))`."""
    try:
        return serve_stdio(collector)
    except KeyboardInterrupt:
        return 130
    except Exception as exc:  # noqa: BLE001 -- the diagnostic must reach stderr, not stdout
        print("collector: %s" % exc, file=sys.stderr)
        return 1
