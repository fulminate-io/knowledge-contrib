// contract.ts — the COLLECTOR CONTRACT as this package can check it for itself:
// whether an advertised schema pair satisfies the contract, whether a result
// payload satisfies the contract's output schema, and whether every key that
// payload carries is one the contract defines.
//
// WHY A COLLECTOR LIBRARY CARRIES THE CLIENT'S OWN GATES. The client runs these
// three at registration and again at collect, and a collector that fails one is
// refused with an error about a schema its author may never have read. Running
// them here, over this package's own advertised schemas and its own encoded
// result, turns that refusal into a local test failure with the offending key
// named. It is also what makes the contract's admit/refuse behaviour testable in
// this package rather than only in the client's Go suite.
//
// THE COMPARATOR READS THE CHECKED-IN FILES, so the requirement has ONE source. A
// hand-written table of "the fields we require" beside a checked-in schema is two
// sources that drift; here the contract schema IS the requirement and
// schemaSatisfies walks it.

import { Ajv2020, type ValidateFunction } from "ajv/dist/2020.js";

import { EDGE_WIRE_KEYS, NODE_WIRE_KEYS } from "./envelope.js";
import { inputContractJSON, outputContractJSON } from "./schema.js";

/** One lazily compiled validator for the contract's output schema. */
let outputValidator: ValidateFunction | undefined;

function contractOutputValidator(): ValidateFunction {
  let validator = outputValidator;
  if (validator === undefined) {
    // The draft-2020-12 entry point, because both contract files declare that
    // dialect. Strict mode is ajv's default and both files compile under it.
    const ajv = new Ajv2020({ allErrors: true, strict: true });
    validator = ajv.compile(JSON.parse(outputContractJSON()) as object);
    outputValidator = validator;
  }
  return validator;
}

/** A parsed schema document, read the way the client's comparator reads one. */
interface ParsedSchema {
  type: string;
  required: string[];
  properties: Record<string, unknown>;
  items: unknown;
}

/**
 * Reads the four keywords the comparator compares.
 *
 * A NON-SCALAR `type` READS AS "no declared type", which is the client's own
 * behaviour and the reason the advertised output schema must be the contract file
 * rather than one generated from a nullable list: a generator emits the union
 * ["null","array"], the client's comparator finds no scalar type, and the tool is
 * refused with a message about a schema its author never wrote.
 */
function parseSchema(raw: unknown): ParsedSchema | undefined {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return undefined;
  const doc = raw as Record<string, unknown>;
  const type = typeof doc["type"] === "string" ? (doc["type"] as string) : "";
  const required = Array.isArray(doc["required"])
    ? (doc["required"] as unknown[]).filter((v): v is string => typeof v === "string")
    : [];
  const properties =
    typeof doc["properties"] === "object" && doc["properties"] !== null && !Array.isArray(doc["properties"])
      ? (doc["properties"] as Record<string, unknown>)
      : {};
  return { type, required, properties, items: doc["items"] };
}

/**
 * The gate the client runs at registration and again at collect: the target tool
 * must advertise BOTH an input schema and an output schema, and both must satisfy
 * the contract. A missing schema or a mismatch is an error naming the tool and
 * the mismatch; there is no admit-and-validate-later path.
 *
 * Throws on refusal and returns nothing on success, so a caller cannot ignore a
 * refusal by forgetting to read a return value.
 */
export function checkToolSchemas(
  tool: string,
  advertisedInput: unknown,
  advertisedOutput: unknown,
): void {
  if (advertisedInput === undefined || advertisedInput === null) {
    throw new Error(
      `custom collector: tool "${tool}" advertises NO input schema; the collector contract requires one ` +
        `(see contract/collector_input.schema.json)`,
    );
  }
  if (advertisedOutput === undefined || advertisedOutput === null) {
    throw new Error(
      `custom collector: tool "${tool}" advertises NO output schema; the collector contract requires one, ` +
        `including the walk_complete completeness assertion (see contract/collector_output.schema.json)`,
    );
  }
  checkAgainstContract(tool, "input", inputContractJSON(), advertisedInput);
  checkAgainstContract(tool, "output", outputContractJSON(), advertisedOutput);
}

/** Compares one advertised schema to the contract schema of the same side. */
function checkAgainstContract(tool: string, side: string, contractJSON: string, advertised: unknown): void {
  const contract = parseSchema(JSON.parse(contractJSON));
  if (contract === undefined) {
    throw new Error(`custom collector: the checked-in ${side} contract schema is unreadable`);
  }
  if (parseSchema(advertised) === undefined) {
    throw new Error(`custom collector: tool "${tool}" ${side} schema is not a readable JSON Schema`);
  }
  const problem = schemaSatisfies(JSON.parse(contractJSON), advertised, side + "Schema");
  if (problem !== undefined) {
    throw new Error(
      `custom collector: tool "${tool}" ${side} schema does not satisfy the collector contract: ${problem}`,
    );
  }
}

/**
 * Reports whether an advertised schema declares AT LEAST what the contract
 * declares, at every path the contract names: the same type, every required key
 * the contract requires, every property the contract declares, and the item
 * schema of every array. It says nothing about the keywords the contract does not
 * name — a provider is free to be STRICTER (extra properties, extra required
 * keys, formats, enums), never looser.
 *
 * AN OPTIONAL PROPERTY THE TOOL DOES NOT DECLARE IS ADMITTED, and the
 * discriminator is the CONTRACT'S OWN required list rather than the advertised
 * one, because the contract grows optional properties over time and under the
 * other rule every such addition would refuse every existing third-party provider
 * at once. It SKIPS AN ABSENT PROPERTY, NEVER A PRESENT ONE: a tool that does
 * declare an optional property still has it compared.
 *
 * Returns the refusal message, or undefined when the advertised schema satisfies
 * the contract. The message names the PATH, the expectation and what was found.
 */
export function schemaSatisfies(
  contract: unknown,
  advertised: unknown,
  path: string,
): string | undefined {
  const c = parseSchema(contract);
  if (c === undefined) return undefined;
  const a = parseSchema(advertised);
  if (a === undefined) return `${path}: the contract declares this and the tool does not`;

  if (c.type !== "" && a.type !== c.type) {
    const got = a.type === "" ? "no declared type" : a.type;
    return `${path}: the contract requires type "${c.type}", the tool declares ${got}`;
  }
  for (const req of c.required) {
    if (!a.required.includes(req)) {
      return (
        `${path}: the contract requires "${req}" to be a required property, ` +
        `the tool's required list is [${a.required.join(" ")}]`
      );
    }
  }
  for (const [name, sub] of Object.entries(c.properties)) {
    if (!(name in a.properties)) {
      // AN OPTIONAL PROPERTY THE TOOL DOES NOT DECLARE IS ADMITTED, and the
      // discriminator is the CONTRACT'S OWN required list rather than the
      // advertised one. A property the contract requires is still refused when
      // its declaration is missing, even where the tool lists the key as
      // required — listing a key and declaring nothing for it is exactly the
      // shape this loop exists to catch.
      //
      // IT SKIPS AN ABSENT PROPERTY, NEVER A PRESENT ONE: a tool that DOES
      // declare an optional property still has it compared below.
      if (!c.required.includes(name)) continue;
      return `${path}: the contract declares the property "${name}" and the tool does not`;
    }
    const problem = schemaSatisfies(sub, a.properties[name], `${path}.${name}`);
    if (problem !== undefined) return problem;
  }
  if (c.items !== undefined) {
    if (a.items === undefined) {
      return `${path}: the contract declares an item schema and the tool does not`;
    }
    const problem = schemaSatisfies(c.items, a.items, `${path}[]`);
    if (problem !== undefined) return problem;
  }
  return undefined;
}

/**
 * Validates raw result JSON against the checked-in contract output schema. Split
 * from {@link decodeResult} so a caller holding bytes validates through the same
 * instrument. Throws naming the tool and the offending property.
 */
export function validateResultPayload(tool: string, raw: unknown): void {
  const validate = contractOutputValidator();
  if (validate(raw)) return;
  const detail = (validate.errors ?? [])
    .map((e) => `${e.instancePath === "" ? "the result" : e.instancePath} ${e.message ?? "is invalid"}` +
      (e.params !== undefined && "missingProperty" in e.params
        ? ` "${String((e.params as { missingProperty: string }).missingProperty)}"`
        : ""))
    .join("; ");
  throw new Error(
    `custom collector: tool "${tool}" result does not satisfy the collector contract's output schema: ${detail}`,
  );
}

/**
 * Validates a tool call's structured content against the contract output schema
 * and refuses any key the contract does not define.
 *
 * The two gates answer different questions. The schema validation asks whether
 * the payload is a conforming collector result at all; the strict walk asks
 * whether every field it carries is one the contract knows how to ship. A key the
 * contract does not name is an ERROR rather than a silent drop, because a typo'd
 * field name silently dropped is a collector author debugging an empty graph with
 * no message to go on.
 */
export function decodeResult(tool: string, structured: unknown): unknown {
  if (structured === undefined || structured === null) {
    throw new Error(
      `custom collector: tool "${tool}" returned no structuredContent; the contract requires a structured ` +
        `result matching its advertised output schema`,
    );
  }
  validateResultPayload(tool, structured);
  const unknownKey = firstUnknownKey(structured as Record<string, unknown>);
  if (unknownKey !== undefined) {
    throw new Error(
      `custom collector: tool "${tool}" result carries a field this collector contract does not define: ` +
        `unknown field "${unknownKey.key}" in ${unknownKey.where}`,
    );
  }
  return structured;
}

/** The contract's own top-level, node and edge key sets, derived from the wire maps. */
const TOP_LEVEL_KEYS = new Set(["nodes", "edges", "walk_complete"]);
const NODE_KEYS = new Set(NODE_WIRE_KEYS.values());
const EDGE_KEYS = new Set(EDGE_WIRE_KEYS.values());

/**
 * The first key the contract does not define, and where it was found.
 *
 * A KEY THE CONTRACT DOES NOT NAME IS AN ERROR RATHER THAN A SILENT DROP: a
 * typo'd field name silently dropped is a collector author debugging an empty
 * graph with no message to go on. The walk reaches the top level, every node and
 * every edge, because the client's own strict decode does.
 */
function firstUnknownKey(result: Record<string, unknown>): { key: string; where: string } | undefined {
  for (const key of Object.keys(result)) {
    if (!TOP_LEVEL_KEYS.has(key)) return { key, where: "the result" };
  }
  for (const [collection, allowed] of [
    ["nodes", NODE_KEYS],
    ["edges", EDGE_KEYS],
  ] as const) {
    const rows = result[collection];
    if (!Array.isArray(rows)) continue;
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i] as Record<string, unknown>;
      if (typeof row !== "object" || row === null) continue;
      for (const key of Object.keys(row)) {
        if (!allowed.has(key)) return { key, where: `${collection}[${i}]` };
      }
    }
  }
  return undefined;
}
