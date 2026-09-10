// schema.ts — the SCHEMAS THIS PACKAGE ADVERTISES, and why they are the
// checked-in contract files rather than schemas built inline.
//
// THE CONTRACT SCHEMAS ARE CHECKED-IN JSON, here as they are on the client side,
// because a collector author writing a provider in another language reads the
// file. The three files under contract/ are a COPY of the client's
// cmd/knowledge/internal/externalcollector/contract set: that package is
// client-internal and unreachable from a published tree, so a copy is the only
// shape that ships. The copy is kept honest by a test that compares its bytes
// against the Go framework's copy — which sits beside this package in the
// published layout and in this repository alike — and, in this repository only,
// against the client's own files.
//
// WHY THE OUTPUT SCHEMA IS THE FILE AND NOT A CONSTRUCTED ONE. The client's
// comparator reads a scalar `type` keyword, so any generator that emits a union
// type for an array (`["null","array"]` is what a nullable list infers to) is
// refused at registration with "the tool declares no declared type". Advertising
// the contract file verbatim closes that for every collector at once.
//
// WHY THE INPUT SCHEMA IS THE FILE WITH ONE PROPERTY SPLICED IN. The collector's
// own params schema has to reach the caller, or nothing validates a collect's
// params; but building the WHOLE input from the collector's declaration puts
// `params` on the advertised top-level required list, and the client omits params
// from the call arguments entirely when a collect carries none — so a required
// `params` would refuse every paramless collect before it was sent. Building the
// input from the contract file keeps its required list at ["id"] by construction.

import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import type { JsonSchema } from "./framework.js";

/**
 * The absolute path of this package's root, wherever it was installed from.
 *
 * IT WALKS UP TO THE package.json rather than counting `..` segments from this
 * module's own URL, because the compiled module sits one directory deeper than
 * the source and a counted path is right in exactly one of the two layouts.
 */
export function packageRoot(): string {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (;;) {
    if (existsSync(join(dir, "package.json"))) return dir;
    const parent = dirname(dir);
    if (parent === dir) {
      throw new Error("framework: no package.json above " + import.meta.url);
    }
    dir = parent;
  }
}

/** Reads one checked-in contract file, verbatim. */
function contractFile(name: string): string {
  return readFileSync(join(packageRoot(), "contract", name), "utf8");
}

/** This package's checked-in copy of the collector input contract schema, verbatim. */
export function inputContractJSON(): string {
  return contractFile("collector_input.schema.json");
}

/** This package's checked-in copy of the collector output contract schema, verbatim. */
export function outputContractJSON(): string {
  return contractFile("collector_output.schema.json");
}

/** This package's checked-in copy of the describe contract schema, verbatim. */
export function describeContractJSON(): string {
  return contractFile("collector_describe.schema.json");
}

/**
 * The describe contract schema VERBATIM, parsed. It is what the describe tool
 * advertises as its output schema and what its result is validated against, on
 * the same terms as the collect tool's pair.
 */
export function advertisedDescribeSchema(): JsonSchema {
  return JSON.parse(describeContractJSON()) as JsonSchema;
}

/**
 * The contract output schema VERBATIM, parsed. Every key the file carries reaches
 * the wire, including the ones neither this package nor the client names.
 */
export function advertisedOutputSchema(): JsonSchema {
  // A fresh parse per call rather than a cached object: the document goes onto
  // the wire and into a caller's hands, and one caller mutating it would change
  // what the next call advertises.
  return JSON.parse(outputContractJSON()) as JsonSchema;
}

/**
 * The contract input schema with `properties.params` replaced by the collector's
 * own declared params schema, and nothing else changed. The document is a decoded
 * object rather than a rebuilt one, so every keyword the contract file carries
 * survives and the top-level required list stays ["id"] by construction.
 */
export function advertisedInputSchema(params: JsonSchema): JsonSchema {
  const doc = JSON.parse(inputContractJSON()) as JsonSchema;
  const properties = doc["properties"];
  if (typeof properties !== "object" || properties === null || Array.isArray(properties)) {
    throw new Error(
      "framework: this package's checked-in input contract schema declares no properties object; the copy is corrupt",
    );
  }
  const props = properties as Record<string, unknown>;
  if (!("params" in props)) {
    throw new Error(
      "framework: this package's checked-in input contract schema declares no params property; the copy is corrupt",
    );
  }
  // A DEEP COPY OF THE COLLECTOR'S DECLARATION, so a collector that returns the
  // same object from paramsSchema() on every call cannot have it mutated by
  // anything downstream, and so the advertised document owns every byte it sends.
  props["params"] = JSON.parse(JSON.stringify(params)) as JsonSchema;
  return doc;
}

/**
 * Refuses a params schema that is not an object schema, naming the collector.
 *
 * The contract declares params as an object and the client's registration gate
 * compares that type, so a collector declaring `string` or `true` for its params
 * would serve here and be refused at registration with an error about a schema it
 * never wrote. A collector with no parameters declares
 * `{ type: "object", properties: {} }`.
 */
export function requireObjectParamsSchema(collector: string, params: JsonSchema): JsonSchema {
  if (typeof params !== "object" || params === null || Array.isArray(params)) {
    throw new Error(
      `framework: collector "${collector}" declares a params schema that is not a JSON Schema object, ` +
        `and the collector contract declares params as an object`,
    );
  }
  if (params["type"] !== "object") {
    throw new Error(
      `framework: collector "${collector}" declares the params schema type ${JSON.stringify(params["type"])}, ` +
        `and the collector contract declares params as an object; declare ` +
        `{ type: "object", properties: {} } for a collector with no parameters`,
    );
  }
  return params;
}
