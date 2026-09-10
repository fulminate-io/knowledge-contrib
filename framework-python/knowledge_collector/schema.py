# SPDX-License-Identifier: Apache-2.0

"""schema.py — the SCHEMAS THIS FRAMEWORK ADVERTISES, and why they are the
checked-in contract files rather than schemas inferred from Python types.

THE CONTRACT SCHEMAS ARE CHECKED-IN JSON, here as they are on the client side,
because a collector author writing a provider in another language reads the file.
The files under contract/ are a COPY of the client's own pair: that package is
client-internal and unimportable from a published sibling repository in any
language, so a copy is the only shape that ships. The copy is kept honest by a
test that opens the client's files and compares the bytes.

WHY THE OUTPUT SCHEMA IS THE FILE AND NOT AN INFERRED ONE. The client's
registration gate compares the advertised schema against the contract, and an
inferred schema loses that comparison in ways an author cannot predict from their
own source -- the Go framework's own note records a slice inferring as the union
type ["null","array"] and the gate reporting "the tool declares no declared type".
Advertising the contract file verbatim closes that for every collector at once.

WHY THE INPUT SCHEMA IS THE FILE WITH ONE PROPERTY SPLICED IN. The collector's
own params schema has to reach the caller, or nothing validates a collect's
params; but inferring the WHOLE input would put `params` on the advertised
top-level required list, and the client omits params from the call arguments
entirely when a collect carries none -- so a required `params` would refuse every
paramless collect before it was sent. Building the input from the contract file
keeps its required list at ["id"] by construction.
"""

import json
import os

__all__ = [
    "SchemaDeclarationError",
    "CONTRACT_DIR",
    "input_contract_json",
    "output_contract_json",
    "describe_contract_json",
    "advertised_output_schema",
    "advertised_input_schema",
    "params_schema_document",
]

CONTRACT_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "contract")

_INPUT_CONTRACT_FILE = "collector_input.schema.json"
_OUTPUT_CONTRACT_FILE = "collector_output.schema.json"
_DESCRIBE_CONTRACT_FILE = "collector_describe.schema.json"


class SchemaDeclarationError(Exception):
    """A collector's own schema declaration is unusable, or this package's
    checked-in contract copy is corrupt."""


def _read_contract(filename):
    path = os.path.join(CONTRACT_DIR, filename)
    try:
        with open(path, "rb") as handle:
            return handle.read()
    except OSError as exc:
        raise SchemaDeclarationError(
            "framework: this package's checked-in contract schema %s is unreadable: %s" % (filename, exc)
        ) from exc


def input_contract_json():
    """This package's checked-in copy of the collector input contract, VERBATIM
    bytes. A caller gets a fresh bytes object it cannot use to mutate ours."""
    return _read_contract(_INPUT_CONTRACT_FILE)


def output_contract_json():
    """This package's checked-in copy of the collector output contract, VERBATIM
    bytes."""
    return _read_contract(_OUTPUT_CONTRACT_FILE)


def describe_contract_json():
    """This package's checked-in copy of the describe-result contract, VERBATIM
    bytes."""
    return _read_contract(_DESCRIBE_CONTRACT_FILE)


def advertised_output_schema():
    """The contract output schema VERBATIM, as a decoded document.

    It is decoded and re-encoded on the way to the wire rather than spliced in as
    raw bytes, because this speaker builds one JSON document per frame; the
    round trip is value-preserving and the byte-parity test compares the decoded
    documents, which is what the client's comparator reads too.
    """
    return json.loads(output_contract_json())


def params_schema_document(collector):
    """The collector's own params schema, checked.

    IT REFUSES A PARAMS DECLARATION THAT IS NOT AN OBJECT, naming what was
    declared. The contract declares `params` as an object and the client's
    registration gate compares that type, so a collector declaring `string` or
    nothing at all would serve here and be refused at registration with an error
    about a schema it never wrote. A collector with no parameters declares
    {"type": "object"}.
    """
    declared = collector.params_schema()
    if not isinstance(declared, dict):
        raise SchemaDeclarationError(
            "framework: params_schema() returned a %s; the collector contract declares params as an object, "
            "so params_schema() must return a JSON Schema document (an empty collector declares {\"type\": \"object\"})"
            % type(declared).__name__
        )
    declared_type = declared.get("type")
    if declared_type != "object":
        raise SchemaDeclarationError(
            "framework: params_schema() declares the JSON Schema type %r, and the collector contract declares "
            "params as an object; declare {\"type\": \"object\"} with your own properties "
            "(an empty object schema is the shape for a collector with no parameters)" % (declared_type,)
        )
    return declared


def advertised_input_schema(collector):
    """The contract input schema with `properties.params` replaced by the
    collector's own declaration, and NOTHING ELSE TOUCHED.

    The document is decoded into a mutable dict rather than rebuilt: the splice is
    one property replacement, and a dict keeps every keyword the contract file
    carries that neither this package nor the client names.
    """
    params = params_schema_document(collector)
    try:
        doc = json.loads(input_contract_json())
    except ValueError as exc:
        raise SchemaDeclarationError(
            "framework: this package's checked-in input contract schema does not decode: %s" % exc
        ) from exc
    props = doc.get("properties")
    if not isinstance(props, dict):
        raise SchemaDeclarationError(
            "framework: this package's checked-in input contract schema declares no properties object; the copy is corrupt"
        )
    if "params" not in props:
        raise SchemaDeclarationError(
            "framework: this package's checked-in input contract schema declares no params property; the copy is corrupt"
        )
    props["params"] = params
    return doc
