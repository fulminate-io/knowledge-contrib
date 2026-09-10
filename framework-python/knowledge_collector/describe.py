# SPDX-License-Identifier: Apache-2.0

"""describe.py — the DESCRIBE TOOL's declaration: what a collector says about
itself, and the refusals that keep the saying honest.

WHY IT IS A SECOND TOOL RATHER THAN A FIELD ON THE COLLECT RESULT. The client
must be able to ask a provider what it is BEFORE it has been told what to call:
`knowledge collector add` dials the provider and fills the registration entry from
what it finds. That ordering is what makes describe a FIXED-NAME tool -- the
client knows this one name and nothing else about the provider -- while the
collect tool's name stays the operator's, carried in the entry.

WHAT THE DECLARATION CARRIES, in the wire spellings the frozen contract file
uses: `behavior` (the graph-level defaults), `node_type_overrides`, `node_types`
and `edge_types` (the closed vocabularies), `environment` (the names this
collector reads with each name's class) and `context` (the foreign-graph slices
it needs, keyed by family).

IT CARRIES NO TOOL NAME, and the absence is the contract's rather than an
omission here. The collect tool's name is the ToolSpec's, which the served
speaker already knows and the registration entry already carries; a second
spelling of it inside the declaration would be a value the client would then
have to reconcile against the tool it just listed.

THE RENDERED DECLARATION NEVER CARRIES A `reason`. The client's config loader
decodes the entry's foreign-context object with unknown fields refused, so a
rendered reason is refused by name at load time. A FamilyDeclaration may carry a
reason for its author's benefit and for the collector's own README; render() drops
it, and the port's suite pins that it does.
"""

import re

from .jsonschema import ValidationError, validate

__all__ = [
    "DESCRIBE_TOOL_NAME",
    "ENV_CLASSES",
    "DeclarationError",
    "EnvName",
    "BehaviorDefaults",
    "NodeTypeBehavior",
    "FamilyDeclaration",
    "Declaration",
]

# The describe tool's FIXED name. It is a constant on both sides of the wire and
# the two must agree: the client cannot discover a tool it does not already know
# the name of.
DESCRIBE_TOOL_NAME = "describe"

# The closed environment-name class vocabulary, in the contract file's own order.
# The installer's own case statement refuses an unknown class, so the framework
# refuses it first, here, where the collector author can see it.
#
# `not-carried` is the fourth and the one that is easiest to mistake for an
# omission: it is a name the collector READS that an installed entry deliberately
# does not declare, so declaring it says the absence was a decision.
ENV_CLASSES = ("path", "selector", "secret", "not-carried")


# The environment-variable name shape an installer will accept, mirroring the Go
# framework's own pattern.
_ENV_NAME = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


class DeclarationError(Exception):
    """A collector's declaration is not renderable. Every message names the part
    that is wrong."""


def _tri_state(target, key, value):
    """Set a TRI-STATE boolean: None leaves the key absent so the cascade
    inherits, True and False pin it."""
    if value is not None:
        target[key] = bool(value)


def _field_list(target, key, value):
    if value:
        target[key] = list(value)


class EnvName:
    """One environment variable NAME this collector reads, and its class. A VALUE
    never appears in a declaration."""

    __slots__ = ("name", "env_class", "description", "empty_sensitive")

    def __init__(self, name, env_class, description="", empty_sensitive=False):
        self.name = name
        self.env_class = env_class
        self.description = description
        # empty_sensitive declares that this collector tells the name PRESENT AND
        # EMPTY apart from ABSENT: it refuses such a value, branches on the name's
        # presence, or hands it to a dependency that does either.
        #
        # FALSE IS THE DEFAULT AND IT RENDERS NOTHING. A collector that
        # discriminates on nothing -- most of them -- produces the document it
        # always produced.
        #
        # WHAT IT GOVERNS IS A DOCUMENT, NOT AN ENTRY. The class decides what an
        # installer writes; the mark decides whether a worked entry may show
        # `${NAME:-}` for the name. That reference resolves to the empty string in
        # the process serving the collect, so the child receives the name present
        # and empty, which is inert for an unmarked name and a broken collect for
        # a marked one. It is legal on every class, `not-carried` included: a name
        # no installed entry carries can still appear in an example someone
        # copies, and that is the only place the mark can live for it.
        self.empty_sensitive = empty_sensitive

    def render(self):
        # THE NAME IS BOUNDED HERE AND NOT BY THE SCHEMA. The contract file
        # DESCRIBES the shape in prose and carries no `pattern` keyword, so a
        # malformed name validates and is refused later by the installer's own
        # consumption loop, in a run the collector author never sees. The Go
        # framework refuses it at the same place this does, and a port that let it
        # through would ship a collector that installs everywhere but here.
        if not _ENV_NAME.match(self.name or ""):
            raise DeclarationError(
                "the environment name %r is not an environment variable name; a name matches "
                "[A-Za-z_][A-Za-z0-9_]* and an installer refuses anything else" % self.name
            )
        if self.env_class not in ENV_CLASSES:
            raise DeclarationError(
                "the environment name %r declares the class %r, which is not one of %s"
                % (self.name, self.env_class, ", ".join(ENV_CLASSES))
            )
        out = {"name": self.name, "class": self.env_class}
        if self.description:
            out["description"] = self.description
        if self.empty_sensitive:
            out["empty_sensitive"] = True
        return out


class BehaviorDefaults:
    """The graph-level behaviour defaults. Booleans are tri-state: leave one None
    to inherit, set it to pin."""

    __slots__ = ("summarizable", "embeddable", "syncable", "embed_fields", "summarize_fields", "bm25_fields")

    def __init__(
        self,
        summarizable=None,
        embeddable=None,
        syncable=None,
        embed_fields=None,
        summarize_fields=None,
        bm25_fields=None,
    ):
        self.summarizable = summarizable
        self.embeddable = embeddable
        self.syncable = syncable
        self.embed_fields = embed_fields
        self.summarize_fields = summarize_fields
        self.bm25_fields = bm25_fields

    def render(self):
        out = {}
        _tri_state(out, "summarizable", self.summarizable)
        _tri_state(out, "embeddable", self.embeddable)
        _tri_state(out, "syncable", self.syncable)
        _field_list(out, "embed_fields", self.embed_fields)
        _field_list(out, "summarize_fields", self.summarize_fields)
        _field_list(out, "bm25_fields", self.bm25_fields)
        return out


class NodeTypeBehavior:
    """One node type's behaviour override. Any subset; an unset value inherits the
    graph default."""

    __slots__ = ("summarizable", "embeddable", "embed_fields", "summarize_fields", "bm25_fields")

    def __init__(self, summarizable=None, embeddable=None, embed_fields=None, summarize_fields=None, bm25_fields=None):
        self.summarizable = summarizable
        self.embeddable = embeddable
        self.embed_fields = embed_fields
        self.summarize_fields = summarize_fields
        self.bm25_fields = bm25_fields

    def render(self):
        out = {}
        _tri_state(out, "summarizable", self.summarizable)
        _tri_state(out, "embeddable", self.embeddable)
        _field_list(out, "embed_fields", self.embed_fields)
        _field_list(out, "summarize_fields", self.summarize_fields)
        _field_list(out, "bm25_fields", self.bm25_fields)
        return out


class FamilyDeclaration:
    """One foreign graph family this collector needs filled.

    `reason` is for the author and for the collector's README. It is NOT rendered:
    the client's config loader refuses an unknown key in the entry's
    foreign-context object by name.

    NEITHER IS `family`, and for the same reason. The rendered context is an
    OBJECT KEYED BY FAMILY, so the family name is the key rather than a member of
    the value; rendering it inside the value too would be the same unknown key
    the loader refuses.
    """

    __slots__ = ("family", "node_types", "all_node_types", "node_fields", "metadata_keys", "edge_fields", "reason")

    def __init__(
        self,
        family,
        node_types=None,
        all_node_types=False,
        node_fields=None,
        metadata_keys=None,
        edge_fields=None,
        reason="",
    ):
        self.family = family
        self.node_types = list(node_types or [])
        self.all_node_types = all_node_types
        self.node_fields = list(node_fields or [])
        self.metadata_keys = list(metadata_keys or [])
        self.edge_fields = list(edge_fields or [])
        self.reason = reason

    def render(self):
        if not self.family:
            raise DeclarationError("a foreign-context declaration names no family")
        if self.all_node_types and self.node_types:
            raise DeclarationError(
                "the foreign-context declaration for family %r carries all_node_types beside a non-empty "
                "node_types list %s; the two say different things and neither is silently preferred"
                % (self.family, self.node_types)
            )
        out = {}
        if self.node_types:
            out["node_types"] = list(self.node_types)
        if self.all_node_types:
            out["all_node_types"] = True
        _field_list(out, "node_fields", self.node_fields)
        _field_list(out, "metadata_keys", self.metadata_keys)
        _field_list(out, "edge_fields", self.edge_fields)
        return out


class Declaration:
    """The whole describe result, before it is rendered.

    THE FIELD NAMES ARE THE CONTRACT'S. `node_types` is the VOCABULARY -- every
    node type this collector emits -- and the per-type behaviour overrides live
    under `node_type_overrides`, which is the pair a reader of the schema file
    meets in that order. An author who reaches for `node_types` expecting the
    override map gets a type error from the renderer rather than a declaration
    that validates and says something else.
    """

    __slots__ = ("behavior", "node_type_overrides", "node_types", "edge_types", "environment", "context")

    def __init__(
        self,
        behavior=None,
        node_type_overrides=None,
        node_types=None,
        edge_types=None,
        environment=None,
        context=None,
    ):
        self.behavior = behavior
        self.node_type_overrides = dict(node_type_overrides or {})
        self.node_types = list(node_types or [])
        self.edge_types = list(edge_types or [])
        self.environment = list(environment or [])
        self.context = list(context or [])

    def render(self):
        """Render the declaration into the document the describe tool returns.

        THE FOUR REQUIRED KEYS ARE ALWAYS EMITTED, empty or not: the schema
        requires behavior, node_types, edge_types and environment, and a collector
        that emits no edges declares an empty array rather than saying nothing.
        The distinction is the whole point of the closed vocabulary -- an absent
        key would be read as "this collector never said" and a family registered
        that way keeps accepting anything.

        THE VOCABULARY IS THE AUTHORITY FOR EVERY TYPE NAME THE DECLARATION USES.
        A per-node-type override naming a type outside `node_types` declares
        behaviour for a node this collector never emits, which is a declaration
        that can never take effect; it is refused by name rather than rendered and
        ignored downstream.
        """
        if self.behavior is None:
            raise DeclarationError(
                "the declaration carries no behavior; summarizable, embeddable and syncable are required and "
                "an omitted one is not a default, it is a collector that never said"
            )
        for axis in ("summarizable", "embeddable", "syncable"):
            if getattr(self.behavior, axis) is None:
                raise DeclarationError(
                    "the declaration's behavior leaves %s unset; the schema requires summarizable, embeddable and "
                    "syncable explicitly (the two LLM axes are a suggestion the operator's flags override)" % axis
                )

        for label, types in (("node_types", self.node_types), ("edge_types", self.edge_types)):
            seen = set()
            for name in types:
                if not name or not name.strip():
                    raise DeclarationError("the declaration's %s carries an empty type name" % label)
                if name in seen:
                    raise DeclarationError("the declaration's %s names %r twice" % (label, name))
                seen.add(name)

        vocabulary = set(self.node_types)
        for node_type in sorted(self.node_type_overrides):
            if node_type not in vocabulary:
                raise DeclarationError(
                    "the per-node-type behaviour override names the node type %r, which is not in this "
                    "collector's node vocabulary %s; an override for a type the walk never emits can never "
                    "take effect" % (node_type, sorted(vocabulary))
                )

        seen_names = set()
        for name in self.environment:
            if name.name in seen_names:
                raise DeclarationError(
                    "the declaration's environment names %r twice; one name has one class" % name.name
                )
            seen_names.add(name.name)

        out = {
            "behavior": self.behavior.render(),
            "node_types": list(self.node_types),
            "edge_types": list(self.edge_types),
            "environment": [name.render() for name in self.environment],
        }
        if self.node_type_overrides:
            out["node_type_overrides"] = {
                name: behavior.render() for name, behavior in sorted(self.node_type_overrides.items())
            }
        if self.context:
            out["context"] = {family.family: family.render() for family in self.context}
        return out


def validate_declaration(document, schema):
    """Validate a rendered declaration against the describe contract schema.

    Split out so the port's own suite validates through the same instrument the
    served tool uses, rather than through a second reading of the same file.
    """
    try:
        validate(document, schema)
    except ValidationError as exc:
        raise DeclarationError("the rendered declaration does not satisfy the describe contract schema: %s" % exc) from exc
