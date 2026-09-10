# SPDX-License-Identifier: Apache-2.0

"""jsonschema.py — the JSON Schema subset this framework validates with, and the
CONTRACT COMPARATOR built on the same reader.

WHY A VALIDATOR LIVES HERE AT ALL. This port takes no runtime dependency, so
there is no `jsonschema` package to lean on, and two obligations need one. The
first is the collect call's params: the framework advertises the collector's own
params schema inside the contract's input document, and something has to hold
the caller to it before the walk runs -- the Python MCP SDK does not, measured,
so a port that delegated this would validate nothing. The second is the
comparator, which is the same question asked of a SCHEMA rather than of a
document: does an advertised schema declare at least what the contract declares.

IT RAISES ON A KEYWORD IT DOES NOT IMPLEMENT, and that is the design rather than
an unfinished edge. A partial validator that ignored an assertion keyword would
silently under-validate exactly the collector whose author reached for that
keyword, and a silently weaker gate is worse than none because it reads as one.
ANNOTATIONS are different and are ignored by name: `title`, `description` and
their friends assert nothing about an instance in any implementation.

THE SUBSET IS DRAFT 2020-12's ASSERTION VOCABULARY, less the four families this
framework has never needed: conditional application (`if`/`then`/`else`,
`dependentSchemas`, `dependentRequired`), unevaluated-location tracking
(`unevaluatedProperties`, `unevaluatedItems`), pattern-keyed properties
(`patternProperties`, `propertyNames`) and `contains`. Each of those RAISES with
its own name, so a collector author reaching for one is told which keyword this
framework does not implement rather than shipping a schema that quietly does
nothing.
"""

import json
import re

__all__ = [
    "SchemaError",
    "ValidationError",
    "validate",
    "satisfies",
    "ANNOTATION_KEYWORDS",
    "ASSERTION_KEYWORDS",
]


class SchemaError(Exception):
    """The SCHEMA document is unusable: a keyword this framework does not
    implement, a malformed sub-schema, an unresolvable reference."""


class ValidationError(Exception):
    """The INSTANCE does not satisfy the schema. The message names the JSON
    pointer-ish path so the caller knows which value to fix."""


# Keywords that assert NOTHING about an instance. Ignoring one is correct in
# every implementation, so they are ignored here by name rather than by falling
# through a default.
ANNOTATION_KEYWORDS = frozenset(
    {
        "$schema",
        "$id",
        "$anchor",
        "$comment",
        "$defs",
        "$vocabulary",
        "definitions",
        "title",
        "description",
        "default",
        "examples",
        "deprecated",
        "readOnly",
        "writeOnly",
        # `format` is annotation-only by default in draft 2020-12. Treating it as
        # an assertion would refuse instances every conforming validator admits.
        "format",
        "contentMediaType",
        "contentEncoding",
    }
)

# Keywords this validator implements. Anything outside the union of this set and
# the annotations above raises SchemaError.
ASSERTION_KEYWORDS = frozenset(
    {
        "$ref",
        "type",
        "enum",
        "const",
        "required",
        "properties",
        "additionalProperties",
        "minProperties",
        "maxProperties",
        "items",
        "prefixItems",
        "minItems",
        "maxItems",
        "uniqueItems",
        "minimum",
        "maximum",
        "exclusiveMinimum",
        "exclusiveMaximum",
        "multipleOf",
        "minLength",
        "maxLength",
        "pattern",
        "allOf",
        "anyOf",
        "oneOf",
        "not",
    }
)

_JSON_TYPES = ("null", "boolean", "object", "array", "number", "integer", "string")


def _is_type(value, want):
    """Report whether a decoded JSON value has the named JSON type.

    THE BOOL CHECK IS FIRST ON EVERY NUMERIC ARM. Python's bool is a subclass of
    int, so `isinstance(True, int)` is true and a naive integer arm would admit
    `true` where a schema says `integer`.
    """
    if want == "null":
        return value is None
    if want == "boolean":
        return isinstance(value, bool)
    if want == "object":
        return isinstance(value, dict)
    if want == "array":
        return isinstance(value, list)
    if want == "integer":
        if isinstance(value, bool):
            return False
        if isinstance(value, int):
            return True
        return isinstance(value, float) and value.is_integer()
    if want == "number":
        return not isinstance(value, bool) and isinstance(value, (int, float))
    if want == "string":
        return isinstance(value, str)
    raise SchemaError("unknown JSON Schema type %r; the types are %s" % (want, ", ".join(_JSON_TYPES)))


def _type_of(value):
    """Name the JSON type of a decoded value, for an error message."""
    for name in ("null", "boolean", "string", "array", "object", "integer", "number"):
        if _is_type(value, name):
            return name
    return type(value).__name__


def _check_keywords(schema, path):
    """Refuse a schema carrying a keyword this framework does not implement."""
    for keyword in schema:
        if keyword in ANNOTATION_KEYWORDS or keyword in ASSERTION_KEYWORDS:
            continue
        raise SchemaError(
            "%s: the schema keyword %r is not implemented by this framework's validator. "
            "It is ignored by no arm here on purpose: a keyword silently skipped would "
            "under-validate exactly the caller who reached for it. The implemented "
            "assertion keywords are: %s" % (path or "#", keyword, ", ".join(sorted(ASSERTION_KEYWORDS)))
        )


def _resolve(schema, root, path):
    """Resolve a LOCAL `$ref`. Remote references are refused by name."""
    ref = schema["$ref"]
    if not isinstance(ref, str):
        raise SchemaError("%s: $ref must be a string, got %s" % (path, _type_of(ref)))
    if ref == "#":
        return root
    if not ref.startswith("#/"):
        raise SchemaError(
            "%s: $ref %r is not a local reference; this framework resolves only "
            "'#' and '#/<pointer>' inside the same document, because a collector "
            "schema that reached the network to validate a collect would be a "
            "network call inside the walk's gate" % (path, ref)
        )
    node = root
    for token in ref[2:].split("/"):
        token = token.replace("~1", "/").replace("~0", "~")
        if not isinstance(node, dict) or token not in node:
            raise SchemaError("%s: $ref %r resolves to nothing in this document" % (path, ref))
        node = node[token]
    if not isinstance(node, dict):
        raise SchemaError("%s: $ref %r resolves to a %s, not a schema object" % (path, ref, _type_of(node)))
    return node


def validate(instance, schema, path="#", root=None):
    """Validate one decoded JSON value against one decoded JSON Schema.

    Raises ValidationError when the instance does not satisfy the schema, and
    SchemaError when the schema itself is unusable. Returns None on success --
    there is no boolean form, because a boolean return invites a caller who
    forgets to read it.
    """
    if root is None:
        root = schema
    if isinstance(schema, bool):
        # The boolean schema form: true admits everything, false admits nothing.
        if schema:
            return
        raise ValidationError("%s: this location admits no value (the schema is `false`)" % path)
    if not isinstance(schema, dict):
        raise SchemaError("%s: a schema must be an object or a boolean, got %s" % (path, _type_of(schema)))

    _check_keywords(schema, path)

    if "$ref" in schema:
        validate(instance, _resolve(schema, root, path), path, root)

    if "type" in schema:
        want = schema["type"]
        wants = want if isinstance(want, list) else [want]
        if not any(_is_type(instance, w) for w in wants):
            raise ValidationError(
                "%s: expected type %s, got %s" % (path, " or ".join(str(w) for w in wants), _type_of(instance))
            )

    if "enum" in schema:
        if not any(_json_equal(instance, option) for option in schema["enum"]):
            raise ValidationError("%s: %s is not one of the enumerated values %s" % (path, json.dumps(instance), json.dumps(schema["enum"])))

    if "const" in schema:
        if not _json_equal(instance, schema["const"]):
            raise ValidationError("%s: expected the constant %s, got %s" % (path, json.dumps(schema["const"]), json.dumps(instance)))

    if isinstance(instance, dict):
        _validate_object(instance, schema, path, root)
    if isinstance(instance, list):
        _validate_array(instance, schema, path, root)
    if isinstance(instance, str):
        _validate_string(instance, schema, path)
    if isinstance(instance, (int, float)) and not isinstance(instance, bool):
        _validate_number(instance, schema, path)

    for keyword in ("allOf", "anyOf", "oneOf"):
        if keyword in schema:
            _validate_combinator(instance, schema[keyword], keyword, path, root)
    if "not" in schema:
        try:
            validate(instance, schema["not"], path + "/not", root)
        except ValidationError:
            pass
        else:
            raise ValidationError("%s: the value satisfies a schema the `not` keyword forbids" % path)


def _json_equal(a, b):
    """JSON equality, with the bool/int distinction Python's `==` loses."""
    if isinstance(a, bool) != isinstance(b, bool):
        return False
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(_json_equal(a[k], b[k]) for k in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(_json_equal(x, y) for x, y in zip(a, b))
    return a == b


def _validate_object(instance, schema, path, root):
    for key in schema.get("required", []):
        if key not in instance:
            raise ValidationError("%s: the required property %r is missing" % (path, key))
    props = schema.get("properties", {})
    if props and not isinstance(props, dict):
        raise SchemaError("%s/properties: must be an object" % path)
    for name, sub in props.items():
        if name in instance:
            validate(instance[name], sub, "%s/%s" % (path, name), root)
    if "additionalProperties" in schema:
        extra = schema["additionalProperties"]
        for name, value in instance.items():
            if name in props:
                continue
            if extra is False:
                raise ValidationError("%s: the property %r is not declared and additional properties are refused" % (path, name))
            if extra is not True:
                validate(value, extra, "%s/%s" % (path, name), root)
    if "minProperties" in schema and len(instance) < schema["minProperties"]:
        raise ValidationError("%s: %d properties, the schema requires at least %d" % (path, len(instance), schema["minProperties"]))
    if "maxProperties" in schema and len(instance) > schema["maxProperties"]:
        raise ValidationError("%s: %d properties, the schema allows at most %d" % (path, len(instance), schema["maxProperties"]))


def _validate_array(instance, schema, path, root):
    prefix = schema.get("prefixItems", [])
    for index, sub in enumerate(prefix):
        if index < len(instance):
            validate(instance[index], sub, "%s[%d]" % (path, index), root)
    if "items" in schema:
        for index in range(len(prefix), len(instance)):
            validate(instance[index], schema["items"], "%s[%d]" % (path, index), root)
    if "minItems" in schema and len(instance) < schema["minItems"]:
        raise ValidationError("%s: %d items, the schema requires at least %d" % (path, len(instance), schema["minItems"]))
    if "maxItems" in schema and len(instance) > schema["maxItems"]:
        raise ValidationError("%s: %d items, the schema allows at most %d" % (path, len(instance), schema["maxItems"]))
    if schema.get("uniqueItems"):
        for i in range(len(instance)):
            for j in range(i + 1, len(instance)):
                if _json_equal(instance[i], instance[j]):
                    raise ValidationError("%s: items %d and %d are equal and the schema requires unique items" % (path, i, j))


def _validate_string(instance, schema, path):
    if "minLength" in schema and len(instance) < schema["minLength"]:
        raise ValidationError("%s: %d characters, the schema requires at least %d" % (path, len(instance), schema["minLength"]))
    if "maxLength" in schema and len(instance) > schema["maxLength"]:
        raise ValidationError("%s: %d characters, the schema allows at most %d" % (path, len(instance), schema["maxLength"]))
    if "pattern" in schema:
        try:
            matcher = re.compile(schema["pattern"])
        except re.error as exc:
            raise SchemaError("%s/pattern: %r is not a usable regular expression: %s" % (path, schema["pattern"], exc)) from exc
        if matcher.search(instance) is None:
            raise ValidationError("%s: %r does not match the pattern %r" % (path, instance, schema["pattern"]))


def _validate_number(instance, schema, path):
    if "minimum" in schema and instance < schema["minimum"]:
        raise ValidationError("%s: %s is below the minimum %s" % (path, instance, schema["minimum"]))
    if "maximum" in schema and instance > schema["maximum"]:
        raise ValidationError("%s: %s is above the maximum %s" % (path, instance, schema["maximum"]))
    if "exclusiveMinimum" in schema and instance <= schema["exclusiveMinimum"]:
        raise ValidationError("%s: %s is not above the exclusive minimum %s" % (path, instance, schema["exclusiveMinimum"]))
    if "exclusiveMaximum" in schema and instance >= schema["exclusiveMaximum"]:
        raise ValidationError("%s: %s is not below the exclusive maximum %s" % (path, instance, schema["exclusiveMaximum"]))
    if "multipleOf" in schema:
        divisor = schema["multipleOf"]
        if divisor <= 0:
            raise SchemaError("%s/multipleOf: must be greater than zero" % path)
        quotient = instance / divisor
        if abs(quotient - round(quotient)) > 1e-9:
            raise ValidationError("%s: %s is not a multiple of %s" % (path, instance, divisor))


def _validate_combinator(instance, schemas, keyword, path, root):
    if not isinstance(schemas, list):
        raise SchemaError("%s/%s: must be an array of schemas" % (path, keyword))
    passed = 0
    first_failure = None
    for index, sub in enumerate(schemas):
        try:
            validate(instance, sub, "%s/%s[%d]" % (path, keyword, index), root)
        except ValidationError as exc:
            if first_failure is None:
                first_failure = exc
            continue
        passed += 1
    if keyword == "allOf" and passed != len(schemas):
        raise ValidationError("%s: the value does not satisfy every allOf branch: %s" % (path, first_failure))
    if keyword == "anyOf" and passed == 0:
        raise ValidationError("%s: the value satisfies no anyOf branch: %s" % (path, first_failure))
    if keyword == "oneOf" and passed != 1:
        raise ValidationError("%s: the value satisfies %d oneOf branches, and exactly one is required" % (path, passed))


def satisfies(contract, advertised, path):
    """Report whether an ADVERTISED schema declares at least what the CONTRACT
    declares, at every location the contract names.

    THIS IS THE PORT'S COPY OF THE CLIENT'S COMPARATOR, and it exists so a
    collector author finds out at their own test time rather than at the
    operator's registration. It is deliberately the same rule: the same type,
    every required key the contract requires, every property the contract
    declares, and the item schema of every array. It says nothing about keywords
    the contract does not name -- a provider may be STRICTER, never looser.

    AN OPTIONAL PROPERTY THE TOOL DOES NOT DECLARE IS ADMITTED, and the
    discriminator is the CONTRACT's own required list rather than the advertised
    one. A provider written before an optional property existed is admitted
    unchanged; a provider that DOES declare an optional property still has it
    compared, so declaring it with the wrong type is refused rather than waved
    through.
    """
    if contract is None:
        return
    if advertised is None:
        raise ValidationError("%s: the contract declares this and the tool does not" % path)
    if not isinstance(contract, dict) or not isinstance(advertised, dict):
        raise SchemaError("%s: both the contract and the advertised schema must be objects" % path)

    contract_type = contract.get("type", "")
    if contract_type != "":
        advertised_type = advertised.get("type", "")
        if advertised_type != contract_type:
            got = advertised_type if advertised_type != "" else "no declared type"
            raise ValidationError('%s: the contract requires type "%s", the tool declares %s' % (path, contract_type, got))

    advertised_required = advertised.get("required", [])
    for required in contract.get("required", []):
        if required not in advertised_required:
            raise ValidationError(
                '%s: the contract requires "%s" to be a required property, the tool\'s required list is %s'
                % (path, required, list(advertised_required))
            )

    contract_required = contract.get("required", [])
    for name, sub in contract.get("properties", {}).items():
        advertised_props = advertised.get("properties", {})
        if name not in advertised_props:
            if name not in contract_required:
                continue
            raise ValidationError('%s: the contract declares the property "%s" and the tool does not' % (path, name))
        satisfies(sub, advertised_props[name], "%s.%s" % (path, name))

    if contract.get("items") is not None:
        if advertised.get("items") is None:
            raise ValidationError("%s: the contract declares an item schema and the tool does not" % path)
        satisfies(contract["items"], advertised["items"], path + "[]")
