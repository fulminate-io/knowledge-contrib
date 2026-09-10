# SPDX-License-Identifier: Apache-2.0

"""framework.py — the AUTHOR SURFACE: the tool spec, the collector protocol, and
the Result / Node / Edge model that becomes the contract envelope.

THE DIVISION OF LABOR IS THE POINT, exactly as it is in the Go framework. Nothing
about MCP, JSON Schema or the envelope is visible to a collector: it implements
`Collector` and calls `serve_stdio`. A collector module that carries MCP speaking
code or envelope-encoding code of its own has re-derived something this package
already settled.

THIS PACKAGE IS NOT THE CLIENT'S. The knowledge client's own consumer side
lives inside the client and is unimportable from a published sibling repository;
the contract between the two is the checked-in JSON schema pair this package
carries a copy of, never a shared package. See schema.py for what that costs and
how the copy is kept honest.
"""

from .completeness import Complete, Completeness, Incomplete
from .context import ForeignContext

__all__ = [
    "DEFAULT_TOOL_NAME",
    "ToolSpec",
    "Collector",
    "Result",
    "Node",
    "Edge",
    "NODE_WIRE_FIELDS",
    "EDGE_WIRE_FIELDS",
]

# The tool name a collector serves when its ToolSpec names none. It matches the
# `tool` value in the config-file entry's own worked example.
DEFAULT_TOOL_NAME = "collect"


class ToolSpec:
    """Names the single MCP tool a collector serves.

    The name is the collector's, not a constant: the config-file entry that
    registers a collector carries a `tool` field naming the one tool the daemon
    calls, and an operator is free to serve `collect_logs` beside another
    provider's `collect`.
    """

    __slots__ = ("name", "description")

    def __init__(self, name="", description=""):
        # Empty means DEFAULT_TOOL_NAME. Empty description is allowed; a
        # collector that means to be installed by a human should write one.
        self.name = name
        self.description = description

    def __repr__(self):
        return "ToolSpec(name=%r, description=%r)" % (self.name, self.description)


class Collector:
    """The whole surface a collector author implements.

    A subclass declares its params schema, names its tool, and walks. The
    framework advertises the params schema inside the contract's input document
    and hands the walk an ALREADY-VALIDATED params value -- a call whose params do
    not satisfy the advertised schema is refused before walk() is reached.

    THE `id` walk() RECEIVES IS THE COLLECT ID, which names the graph INSTANCE the
    result lands in. It is NOT the graph family: the family is the registration
    name, which the client derives on its own side and never sends, so a
    collector cannot choose the graph type it writes into.
    """

    def tool(self):
        """Name the MCP tool this collector serves. Returns a ToolSpec."""
        raise NotImplementedError("a collector must implement tool()")

    def params_schema(self):
        """The JSON Schema of this collector's params, as a decoded document.

        IT IS DECLARED RATHER THAN INFERRED, and that is the one place this port
        deliberately differs from the Go framework's shape. Go infers the schema
        from a generic params type because it has one to reflect over; Python's
        equivalent would be an inferred schema built from annotations, which is
        the shape the client's registration gate refuses on the Go side for a
        different reason and which no reader of the entry could predict. A
        collector with no parameters returns {"type": "object"}.
        """
        raise NotImplementedError("a collector must implement params_schema()")

    def walk(self, collect_id, params, foreign):
        """Enumerate the source and return what was found, together with the
        completeness assertion for this walk.

        A raised exception becomes a tool-call error naming the collector and the
        cause; it never becomes an empty successful result.

        `foreign` is the DECLARED FOREIGN-GRAPH CONTEXT: the cloud resources or
        code-graph nodes this collector's registration entry declared it needs,
        read out of the operator's own graphs by the client and sent with the
        call. It is the EMPTY block for a collector whose entry declares nothing.

        IT IS A THIRD PARAMETER RATHER THAN A FIELD ON A REQUEST OBJECT, and the
        choice is worth stating because it is the one this seam had. A request
        object would let the framework add inputs later without moving this
        signature; a parameter makes the block impossible to receive by accident
        and impossible to ignore by omission.
        """
        raise NotImplementedError("a collector must implement walk()")

    def describe(self):
        """The collector's DECLARATION, as a `Declaration` from describe.py.

        IT IS REQUIRED, not optional, and the reason is that its absence has no
        honest rendering. The declaration carries the node and edge vocabulary the
        walk emits, and an empty vocabulary is a real declaration meaning "this
        collector emits nothing" -- so a framework that filled in an empty one for
        an author who simply had not written it would turn a missing declaration
        into a claim the author never made. Every collector implements it.
        """
        raise NotImplementedError("a collector must implement describe()")


# The node wire vocabulary, in the contract's own field order. `id` and `type`
# are unconditional; the other fourteen are omitted when empty, which is what
# makes a node written by this port byte-comparable with one written by the Go
# framework.
NODE_WIRE_FIELDS = (
    "id",
    "type",
    "symbol_name",
    "file_path",
    "language",
    "start_line",
    "end_line",
    "content",
    "signature",
    "summary",
    "description",
    "source",
    "status",
    "keywords",
    "is_exported",
    "metadata",
)

# The edge wire vocabulary. Endpoints and type are unconditional; the other six
# are omitted when empty, so an edge that names no foreign family is
# byte-identical to one written before those fields existed.
EDGE_WIRE_FIELDS = (
    "from_id",
    "to_id",
    "type",
    "weight",
    "confidence",
    "method",
    "evidence",
    "source_graph",
    "target_graph",
)


class Node:
    """One node a walk produced.

    Server-owned bookkeeping fields (created / updated / tombstoned stamps, the
    collect epoch) are deliberately absent: the collect-write path stamps them, so
    a collector cannot set them. Anything beyond the typed fields rides in
    `metadata`, exactly as the built-in collectors do.
    """

    __slots__ = NODE_WIRE_FIELDS

    def __init__(
        self,
        id,  # noqa: A002 -- the contract's own field name
        type,  # noqa: A002 -- the contract's own field name
        symbol_name="",
        file_path="",
        language="",
        start_line=0,
        end_line=0,
        content="",
        signature="",
        summary="",
        description="",
        source="",
        status="",
        keywords="",
        is_exported=False,
        metadata=None,
    ):
        self.id = id
        self.type = type
        self.symbol_name = symbol_name
        self.file_path = file_path
        self.language = language
        self.start_line = start_line
        self.end_line = end_line
        self.content = content
        self.signature = signature
        self.summary = summary
        self.description = description
        self.source = source
        self.status = status
        self.keywords = keywords
        self.is_exported = is_exported
        self.metadata = metadata if metadata is not None else {}

    def to_wire(self):
        """The node as the contract carries it: `id` and `type` always, every
        other field only when it holds something."""
        out = {"id": self.id, "type": self.type}
        for field in NODE_WIRE_FIELDS[2:]:
            value = getattr(self, field)
            if value:
                out[field] = value
        return out

    def __repr__(self):
        return "Node(id=%r, type=%r)" % (self.id, self.type)


class Edge:
    """One edge a walk produced. Endpoints are referenced by node id.

    AN IN-GRAPH EDGE IS PASSED THROUGH UNTOUCHED, including one naming an endpoint
    this result does not carry. That is not an oversight and a collector must not
    "fix" it: the client converts a dangling edge deliberately, and the write path
    resolves no endpoint -- the server stores from_id and to_id verbatim, with no
    lookup and no proxy materialized on its side.

    AN EDGE INTO ANOTHER GRAPH IS THE ONE THING THAT IS NOT PASSED THROUGH. Set
    `target_graph` and the client resolves the endpoint against that graph family,
    materializes a proxy, and links the edge into the LINKAGE graph; leave it
    empty and the edge is an ordinary edge of this collect's own graph.

    EITHER END MAY BE THE FOREIGN ONE. `source_graph` is `target_graph`'s mirror:
    it names the family `from_id` lives in, for a relationship whose far endpoint
    is the source. SET AT MOST ONE OF THE TWO. An edge naming BOTH is REFUSED by
    the collect, because one resolution reaches one foreign family -- so an edge
    foreign at both ends names something this contract cannot resolve. The port
    emits such an edge AS THE AUTHOR WROTE IT and lets the client refuse it,
    rather than silently correcting a declaration the author meant.
    """

    __slots__ = EDGE_WIRE_FIELDS

    def __init__(
        self,
        from_id,
        to_id,
        type,  # noqa: A002 -- the contract's own field name
        weight=0.0,
        confidence=0.0,
        method="",
        evidence="",
        source_graph="",
        target_graph="",
    ):
        self.from_id = from_id
        self.to_id = to_id
        self.type = type
        self.weight = weight
        self.confidence = confidence
        self.method = method
        self.evidence = evidence
        self.source_graph = source_graph
        self.target_graph = target_graph

    def to_wire(self):
        """The edge as the contract carries it: the endpoints and the type
        always, every other field only when it holds something."""
        out = {"from_id": self.from_id, "to_id": self.to_id, "type": self.type}
        for field in EDGE_WIRE_FIELDS[3:]:
            value = getattr(self, field)
            if value:
                out[field] = value
        return out

    def __repr__(self):
        return "Edge(from_id=%r, to_id=%r, type=%r)" % (self.from_id, self.to_id, self.type)


class Result:
    """One walk's output.

    `complete` HAS NO DEFAULT. That is the port's replacement for the Go type's
    absent zero value: an author who omits the completeness decision gets a
    TypeError from the constructor rather than a silently incomplete walk. The
    bare `Completeness()` remains buildable and is refused loudly by the encoder,
    which is the residual completeness.py states.
    """

    __slots__ = ("nodes", "edges", "complete")

    def __init__(self, nodes, edges, complete):
        self.nodes = list(nodes) if nodes is not None else []
        self.edges = list(edges) if edges is not None else []
        self.complete = complete

    def __repr__(self):
        return "Result(nodes=%d, edges=%d, complete=%r)" % (len(self.nodes), len(self.edges), self.complete)


# Re-exported so a collector author imports one module.
_ = (Complete, Incomplete, Completeness, ForeignContext)
