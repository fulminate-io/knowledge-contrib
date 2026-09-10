# SPDX-License-Identifier: Apache-2.0

"""context.py — the DECLARED FOREIGN-GRAPH CONTEXT a collect may carry, as the
Python shape a walk receives it in.

IT IS A COPY OF THE CLIENT'S SHAPE AND THAT IS THE DESIGN, not an oversight. The
client's own types live in a client-internal package no published sibling
repository can import in any language; the contract between the two sides is the
checked-in JSON schema pair this package carries a copy of, exactly as the Node
and Edge shapes on the output side are. The wire names below are the contract's
spellings and they are what keep the two halves the same object.

A COLLECTOR NEVER ASKS FOR THIS BLOCK AT RUN TIME. It is DECLARED, once, in the
operator's config entry -- which graph families the collector needs, which node
types, which fields, which metadata keys -- and the client fills it from its own
graphs before the call. A collector whose entry declares nothing receives the
empty block.

WHAT A COLLECTOR CAN CONCLUDE FROM AN EMPTY BLOCK: nothing about the operator's
graphs unless its own entry declared the family. The block carries what was
declared and only what was declared, so an absent family means "not declared",
never "not present in the graph".

IT IS KEYED BY GRAPH-TYPE NAME, NOT BY A FIXED SET OF FIELDS. The families a
client can supply are `code` plus whatever graph types the operator has
REGISTERED, which is not a set any class in this module could enumerate. A
collector reads the arm its own entry declared, by the name it declared it under.
"""

__all__ = [
    "FAMILY_CODE",
    "ForeignContext",
    "ForeignGraph",
    "ForeignNode",
    "ForeignEdge",
]

# The one family name this module can name as a constant: it is the only
# supplyable family that is not an operator-registered graph type.
FAMILY_CODE = "code"


class ForeignNode:
    """One node of a declared slice, carrying the declared fields and no others.
    A field the entry did not declare arrives as its empty value, because it was
    never sent."""

    __slots__ = ("id", "type", "symbol_name", "file_path", "content", "metadata")

    def __init__(self, id="", type="", symbol_name="", file_path="", content="", metadata=None):  # noqa: A002
        self.id = id
        self.type = type
        self.symbol_name = symbol_name
        self.file_path = file_path
        self.content = content
        self.metadata = dict(metadata or {})

    @classmethod
    def from_wire(cls, doc):
        return cls(
            id=doc.get("id", ""),
            type=doc.get("type", ""),
            symbol_name=doc.get("symbol_name", ""),
            file_path=doc.get("file_path", ""),
            content=doc.get("content", ""),
            metadata=doc.get("metadata") or {},
        )

    def __repr__(self):
        return "ForeignNode(id=%r, type=%r)" % (self.id, self.type)


class ForeignEdge:
    """One edge of a declared slice. Endpoints are node ids in the same graph."""

    __slots__ = ("from_id", "to_id")

    def __init__(self, from_id="", to_id=""):
        self.from_id = from_id
        self.to_id = to_id

    @classmethod
    def from_wire(cls, doc):
        return cls(from_id=doc.get("from_id", ""), to_id=doc.get("to_id", ""))

    def __repr__(self):
        return "ForeignEdge(from_id=%r, to_id=%r)" % (self.from_id, self.to_id)


class ForeignGraph:
    """One graph's declared slice.

    `graph_name` is always carried, even where the declaration asks for no nodes:
    a predicate that is a membership test over graph names needs the names and
    nothing else, and that is a legitimate declaration rather than an empty one.
    """

    __slots__ = ("graph_name", "nodes", "edges")

    def __init__(self, graph_name="", nodes=None, edges=None):
        self.graph_name = graph_name
        self.nodes = list(nodes or [])
        self.edges = list(edges or [])

    @classmethod
    def from_wire(cls, doc):
        return cls(
            graph_name=doc.get("graph_name", ""),
            nodes=[ForeignNode.from_wire(n) for n in (doc.get("nodes") or [])],
            edges=[ForeignEdge.from_wire(e) for e in (doc.get("edges") or [])],
        )

    def __repr__(self):
        return "ForeignGraph(graph_name=%r, nodes=%d, edges=%d)" % (self.graph_name, len(self.nodes), len(self.edges))


class ForeignContext:
    """The collect input's context block: the foreign-graph slices this
    collector's registration declared it needs, keyed by graph-type name.

    A DECLARED FAMILY IS ALWAYS PRESENT, even when the operator's store held no
    graph of that type: its value is an empty list rather than a missing key. A
    missing key means the entry never asked, which is a different fact and one a
    collector is entitled to distinguish -- `graphs()` collapses the two, and
    `declared()` is how a collector tells them apart.
    """

    __slots__ = ("_families",)

    def __init__(self, families=None):
        self._families = dict(families or {})

    @classmethod
    def from_wire(cls, doc):
        """Build the block from the call's decoded `context` argument. A caller
        that sent no block at all gets the empty one."""
        if not doc:
            return cls()
        if not isinstance(doc, dict):
            raise ValueError(
                "the collect's context block is a %s; the contract declares it as an object keyed by graph-type name"
                % type(doc).__name__
            )
        families = {}
        for family, graphs in doc.items():
            if graphs is None:
                families[family] = []
                continue
            if not isinstance(graphs, list):
                raise ValueError(
                    "the collect's context block declares family %r as a %s; the contract declares each key as an array of graphs"
                    % (family, type(graphs).__name__)
                )
            families[family] = [ForeignGraph.from_wire(g) for g in graphs]
        return cls(families)

    def is_empty(self):
        """Report whether this block carries no family at all, which is what a
        collector whose entry declares nothing receives."""
        return len(self._families) == 0

    def declared(self, family):
        """Report whether the entry DECLARED this family at all. A declared
        family that matched no graph carries an empty list; an undeclared one is
        absent, and the two are different facts."""
        return family in self._families

    def graphs(self, family):
        """The slice declared under one family name, or the empty list when the
        entry did not declare it. Use declared() when the difference matters."""
        return self._families.get(family, [])

    def families(self):
        """Every declared family name, SORTED.

        Sorted because a collector that walks families and emits edges from them
        would otherwise emit them in whatever order the wire happened to carry,
        which makes one unchanged input produce a different result each run.
        """
        return sorted(self._families)

    def excluding(self, *families):
        """Every declared family's graphs EXCEPT those named, flattened, in
        sorted family order.

        IT EXISTS FOR THE COLLECTORS THAT CORRELATE ACROSS PROVIDERS. A log
        collector matches its streams against resource metadata and does not care
        which provider's graph a resource came from -- it declares every provider
        family it might correlate with, and wants them as one set. Naming the
        families to EXCLUDE rather than to include is what keeps that working when
        an operator registers a provider the collector's author never heard of.

        The name is `excluding` rather than the Go framework's `Except` because
        `except` is a Python keyword; the behaviour is the same.
        """
        excluded = set(families)
        out = []
        for name in self.families():
            if name in excluded:
                continue
            out.extend(self._families[name])
        return out

    def __repr__(self):
        return "ForeignContext(families=%r)" % (self.families(),)
