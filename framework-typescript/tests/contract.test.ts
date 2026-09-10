// contract.test.ts — R2(b): the THIRTEEN contract properties the client's own Go
// suite pins, replicated here one by one so a collector written in this language
// is held to them locally rather than only at the client's registration gate.
//
// THE OTHER THREE OF THE GO SUITE'S SIXTEEN ARE DISPOSITIONED, by name, at the
// foot of this file, so a reader sees a decided exclusion rather than a miss. A
// mirror invented to reach sixteen would assert its own fixture.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

import {
  checkToolSchemas,
  decodeResult,
  validateResultPayload,
} from "../src/contract.js";
import { EDGE_WIRE_KEYS, NODE_WIRE_KEYS, encodeResult } from "../src/envelope.js";
import { complete } from "../src/completeness.js";
import { inputContractJSON, outputContractJSON } from "../src/schema.js";
import { goFrameworkDir } from "./helpers.js";

type Doc = Record<string, any>;

/** A mutable decode of a contract schema, so a row can bend exactly one thing. */
const contractMap = (raw: string): Doc => JSON.parse(raw) as Doc;
const inputDoc = (): Doc => contractMap(inputContractJSON());
const outputDoc = (): Doc => contractMap(outputContractJSON());

const dropProperty = (doc: Doc, name: string): Doc => {
  delete doc["properties"][name];
  return doc;
};

/** Asserts a refusal and returns its message. */
function refusal(fn: () => void, ...mustContain: string[]): string {
  let message: string | undefined;
  assert.throws(fn, (err: Error) => {
    message = err.message;
    return true;
  });
  assert.ok(message !== undefined);
  for (const part of mustContain) {
    assert.ok(
      message.includes(part),
      `the refusal must name ${JSON.stringify(part)}; it said ${JSON.stringify(message)}`,
    );
  }
  return message;
}

// --- R2b.1 ---------------------------------------------------------------

test("R2b.1 the output contract requires the completeness assertion", () => {
  const schema = outputDoc();
  assert.equal(schema["type"], "object");
  assert.deepEqual(
    [...(schema["required"] as string[])].sort(),
    ["edges", "nodes", "walk_complete"],
    "the completeness assertion is REQUIRED — a provider must not be able to omit it and disable deletion by silence",
  );
  const props = schema["properties"] as Doc;
  assert.equal(props["walk_complete"]["type"], "boolean");
  assert.equal(props["nodes"]["type"], "array");
  assert.equal(props["edges"]["type"], "array");
});

// --- R2b.2 ---------------------------------------------------------------

test("R2b.2 the input contract requires the collect id and nothing else at top level", () => {
  const schema = inputDoc();
  assert.equal(schema["type"], "object");
  assert.deepEqual(schema["required"], ["id"]);
  const props = schema["properties"] as Doc;
  assert.equal(props["id"]["type"], "string");
  assert.equal(props["params"]["type"], "object");
});

// --- R2b.3 ---------------------------------------------------------------

test("R2b.3 the contract schema and this package's node/edge model agree, field for field", () => {
  // Every key the schema names must decode through this package's own gate...
  const minimal =
    '{"nodes":[{"id":"a","type":"issue"}],"edges":[{"from_id":"a","to_id":"b","type":"blocks"}],"walk_complete":true}';
  const decoded = decodeResult("t", JSON.parse(minimal)) as Doc;
  assert.equal((decoded["nodes"] as unknown[]).length, 1);
  assert.equal((decoded["edges"] as unknown[]).length, 1);
  assert.equal(decoded["walk_complete"], true);

  // ...and the envelope must name no top-level field the schema does not.
  const encoded = JSON.parse(JSON.stringify(encodeResult("t", { complete: complete() }))) as Doc;
  const props = outputDoc()["properties"] as Doc;
  for (const key of Object.keys(encoded)) {
    assert.ok(
      key in props,
      `the envelope carries the top-level field ${key} and the checked-in contract schema does not describe it`,
    );
  }

  // ...and every node and edge key this package can emit is one the contract's
  // own vocabulary declares, which is the half a top-level check cannot see.
  const full = JSON.parse(
    JSON.stringify(
      encodeResult("t", {
        nodes: [
          {
            id: "a",
            type: "issue",
            symbolName: "s",
            filePath: "f",
            language: "go",
            startLine: 1,
            endLine: 2,
            content: "c",
            signature: "sig",
            summary: "sum",
            description: "d",
            source: "src",
            status: "open",
            keywords: "k",
            isExported: true,
            metadata: { a: "b" },
          },
        ],
        edges: [
          {
            fromId: "a",
            toId: "b",
            type: "blocks",
            weight: 1,
            confidence: 1,
            method: "m",
            evidence: "e",
            targetGraph: "code",
          },
        ],
        complete: complete(),
      }),
    ),
  ) as Doc;
  assert.doesNotThrow(
    () => decodeResult("t", full),
    "a payload carrying EVERY property this package can emit must survive the contract's own strict decode",
  );
});

test("R2b.3 and the AUTHORITATIVE vocabulary is the Go model beside this package, not this package's own map", () => {
  // WHY THE ROW ABOVE IS NOT ENOUGH, measured. Its third arm encodes a node
  // carrying every property this package can emit and asserts it survives
  // decodeResult — whose allowed key set is derived from the SAME wire map the
  // encoder emits from. The subject supplies its own answer key, so adding a
  // seventeenth field to both the model and the map leaves it green.
  //
  // AND THE CONTRACT SCHEMA CANNOT SEE IT EITHER: the output schema declares two
  // node properties and five edge properties and sets no additionalProperties, so
  // it is a floor over a fraction of the vocabulary rather than the vocabulary.
  //
  // THE EXTERNAL EXPECTATION IS THE CLIENT'S OWN DECODE, and its shape is carried
  // by the Go framework's model, which sits beside this package in THIS layout
  // and in the published one alike. A field this package can emit and that model
  // cannot name would have every collect refused by the client's strict decode
  // in production while this suite stayed green.
  const goSource = readFileSync(join(goFrameworkDir(), "framework.go"), "utf8");

  const tagsOf = (typeName: string): string[] => {
    const start = goSource.indexOf(`type ${typeName} struct {`);
    assert.ok(start > 0, `the Go framework must declare type ${typeName}`);
    const end = goSource.indexOf("\n}", start);
    assert.ok(end > start, `type ${typeName} must be a closed struct literal`);
    return [...goSource.slice(start, end).matchAll(/`json:"([^",]+)(?:,[^"]*)?"`/g)].map((m) => m[1]!);
  };

  const goNode = tagsOf("Node");
  const goEdge = tagsOf("Edge");
  // THE CONTROL ON THE INSTRUMENT: a regex that matched nothing would make both
  // set comparisons below trivially true in the direction that matters.
  assert.equal(goNode.length, 16, `read ${goNode.length} node json tags from the Go model: [${goNode.join(", ")}]`);
  assert.equal(goEdge.length, 9, `read ${goEdge.length} edge json tags from the Go model: [${goEdge.join(", ")}]`);

  // BOTH DIRECTIONS. A field this package has and the Go model does not is a
  // collect the client will refuse; one the Go model has and this package does
  // not is a field an author cannot emit at all.
  assert.deepEqual([...NODE_WIRE_KEYS.values()].sort(), [...goNode].sort());
  assert.deepEqual([...EDGE_WIRE_KEYS.values()].sort(), [...goEdge].sort());
});

// --- R2b.4 ---------------------------------------------------------------

test("R2b.4 a provider advertising no schema is refused — BOTH halves", () => {
  // The Go original can reach only the output half end to end, because its SDK
  // server refuses to publish a tool with no input schema at all. This package's
  // speaker has no such constraint, so both halves are covered here.
  refusal(
    () => checkToolSchemas("collect_graph", undefined, outputDoc()),
    "NO input schema",
    "collect_graph",
  );
  refusal(() => checkToolSchemas("collect_graph", null, outputDoc()), "NO input schema");
  refusal(
    () => checkToolSchemas("collect_graph", inputDoc(), undefined),
    "NO output schema",
    "walk_complete",
  );
  refusal(() => checkToolSchemas("collect_graph", inputDoc(), null), "NO output schema");
});

// --- R2b.5 ---------------------------------------------------------------

test("R2b.5 the contract verbatim passes, and so does a STRICTER schema: the contract is a floor", () => {
  assert.doesNotThrow(() => checkToolSchemas("collect_graph", inputDoc(), outputDoc()));

  const stricter = outputDoc();
  stricter["required"] = ["nodes", "edges", "walk_complete", "collected_at"];
  stricter["properties"]["collected_at"] = { type: "string" };
  assert.doesNotThrow(
    () => checkToolSchemas("collect_graph", inputDoc(), stricter),
    "a provider declaring MORE than the contract is conforming; the contract is a floor, not an equality",
  );
});

// --- R2b.6 ---------------------------------------------------------------

const LOOSENINGS: Array<{ name: string; bend: (out: Doc) => void; wantErrHas: string }> = [
  {
    name: "top-level type is not an object",
    bend: (out) => {
      out["type"] = "array";
    },
    wantErrHas: 'the contract requires type "object"',
  },
  {
    name: "walk_complete not required",
    bend: (out) => {
      out["required"] = ["nodes", "edges"];
    },
    wantErrHas: "walk_complete",
  },
  {
    name: "walk_complete property missing",
    bend: (out) => {
      delete out["properties"]["walk_complete"];
    },
    wantErrHas: "walk_complete",
  },
  {
    name: "walk_complete declared as a string",
    bend: (out) => {
      out["properties"]["walk_complete"] = { type: "string" };
    },
    wantErrHas: 'requires type "boolean"',
  },
  {
    name: "nodes declared as an object",
    bend: (out) => {
      out["properties"]["nodes"] = { type: "object" };
    },
    wantErrHas: 'requires type "array"',
  },
  {
    name: "node items do not require an id",
    bend: (out) => {
      out["properties"]["nodes"]["items"]["required"] = ["type"];
    },
    wantErrHas: "outputSchema.nodes[]",
  },
  {
    name: "edge items do not require the endpoints",
    bend: (out) => {
      out["properties"]["edges"]["items"]["required"] = ["type"];
    },
    wantErrHas: "from_id",
  },
  {
    name: "nodes declares no item schema",
    bend: (out) => {
      delete out["properties"]["nodes"]["items"];
    },
    wantErrHas: "item schema",
  },
];

for (const tc of LOOSENINGS) {
  test(`R2b.6 refused individually: ${tc.name}`, () => {
    const out = outputDoc();
    tc.bend(out);
    refusal(() => checkToolSchemas("collect_graph", inputDoc(), out), tc.wantErrHas, "collect_graph");
  });
}

test("R2b.6 the eight loosenings bend eight different things", () => {
  assert.equal(LOOSENINGS.length, 8);
  assert.equal(new Set(LOOSENINGS.map((l) => l.name)).size, 8);
});

// --- R2b.7 ---------------------------------------------------------------

const NON_CONFORMING: Array<{ name: string; payload: string; wantErrHas: string }> = [
  { name: "missing walk_complete", payload: '{"nodes":[],"edges":[]}', wantErrHas: "walk_complete" },
  {
    name: "nodes is not an array",
    payload: '{"nodes":{},"edges":[],"walk_complete":true}',
    wantErrHas: "nodes",
  },
  {
    name: "node missing its type",
    payload: '{"nodes":[{"id":"a"}],"edges":[],"walk_complete":true}',
    wantErrHas: "type",
  },
  {
    // THE TRAP ROW, transcribed in full rather than abbreviated: shortened to
    // {"walk_complete":"yes"} it would also violate two required properties, so a
    // validator that never reached the boolean type check would still refuse it
    // and this row would pass for the wrong reason.
    name: "walk_complete is a string",
    payload: '{"nodes":[],"edges":[],"walk_complete":"yes"}',
    wantErrHas: "walk_complete",
  },
];

for (const tc of NON_CONFORMING) {
  test(`R2b.7 payload refused: ${tc.name}`, () => {
    refusal(
      () => validateResultPayload("collect_graph", JSON.parse(tc.payload)),
      "collect_graph",
      tc.wantErrHas,
    );
  });
}

test("R2b.7 the fourth fixture violates ONLY the boolean type, so the row cannot pass for the wrong reason", () => {
  const payload = JSON.parse(NON_CONFORMING[3]!.payload) as Doc;
  assert.deepEqual(Object.keys(payload).sort(), ["edges", "nodes", "walk_complete"]);
  assert.deepEqual(payload["nodes"], []);
  assert.deepEqual(payload["edges"], []);
});

// --- R2b.8 ---------------------------------------------------------------

test("R2b.8 a typo'd key is an ERROR, not a silent drop, and the error names the key", () => {
  refusal(
    () =>
      decodeResult(
        "collect_graph",
        JSON.parse('{"nodes":[{"id":"a","type":"issue","summry":"typo"}],"edges":[],"walk_complete":true}'),
      ),
    "summry",
    "does not define",
  );
});

test("R2b.8 the unknown-key walk reaches every level, and admits the whole declared vocabulary", () => {
  refusal(
    () => decodeResult("t", JSON.parse('{"nodes":[],"edges":[],"walk_complete":true,"extra":1}')),
    "extra",
  );
  refusal(
    () =>
      decodeResult(
        "t",
        JSON.parse('{"nodes":[],"edges":[{"from_id":"a","to_id":"b","type":"t","wieght":1}],"walk_complete":true}'),
      ),
    "wieght",
  );
  // THE CONTROL: the correctly spelled siblings of both typos are admitted, so
  // the two refusals above are about the key and not about the walk refusing
  // everything.
  assert.doesNotThrow(() =>
    decodeResult(
      "t",
      JSON.parse(
        '{"nodes":[{"id":"a","type":"issue","summary":"ok","metadata":{"k":"v"}}],' +
          '"edges":[{"from_id":"a","to_id":"b","type":"t","weight":1,"source_graph":"code"}],"walk_complete":true}',
      ),
    ),
  );
});

// --- R2b.9 ---------------------------------------------------------------

test("R2b.9 a result with no structured content is refused", () => {
  refusal(() => decodeResult("collect_graph", null), "no structuredContent", "collect_graph");
  refusal(() => decodeResult("collect_graph", undefined), "no structuredContent");
});

// --- R2b.10 --------------------------------------------------------------

test("R2b.10 context is a declared but OPTIONAL input property of type object", () => {
  const schema = inputDoc();
  assert.deepEqual(
    schema["required"],
    ["id"],
    "the collect id is the ONLY required input property; an added required property refuses every existing provider",
  );
  const props = schema["properties"] as Doc;
  assert.ok(props["context"] !== undefined, "the context block is a declared property of the published input schema");
  assert.equal(props["context"]["type"], "object");
  // THE CONTROL: the two properties that predate it are untouched, so the
  // assertion above is about an addition rather than about a rewritten file.
  assert.equal(props["id"]["type"], "string");
  assert.equal(props["params"]["type"], "object");
});

// --- R2b.11 --------------------------------------------------------------

test("R2b.11 a provider lacking the optional context property is admitted", () => {
  assert.doesNotThrow(() =>
    checkToolSchemas("collect", dropProperty(inputDoc(), "context"), outputDoc()),
  );
});

test("R2b.11 a provider lacking the optional params property is admitted", () => {
  assert.doesNotThrow(() =>
    checkToolSchemas("collect", dropProperty(inputDoc(), "params"), outputDoc()),
  );
});

test("R2b.11 control: the contract advertised verbatim still passes, through the same instrument", () => {
  assert.doesNotThrow(() => checkToolSchemas("collect", inputDoc(), outputDoc()));
});

// --- R2b.12 --------------------------------------------------------------

test("R2b.12 a provider missing a REQUIRED property is still refused, by the property loop", () => {
  const advertised = dropProperty(inputDoc(), "id");
  assert.ok(
    (advertised["required"] as string[]).includes("id"),
    "fixture control: the advertised required list still names id, so only the property is missing",
  );
  refusal(() => checkToolSchemas("collect", advertised, outputDoc()), 'the property "id"');
});

// THE ONE MUTATION THIS ROW CANNOT KILL, RECORDED RATHER THAN PAPERED OVER.
// Keying the property loop's skip on the ADVERTISED required list instead of the
// CONTRACT's is an EQUIVALENT MUTANT here, and no fixture distinguishes them: the
// required-list loop runs first and refuses every advertised schema whose
// required list is missing a key the contract requires, so by the time the
// property loop is reached the two lists always agree on the contract's required
// keys. What the choice still buys is the OPTIONAL half — a property the contract
// declares without requiring — and that half IS killable, by the two R2b.11 rows
// and the R2b.13 wrong-type row above.

// --- R2b.13 --------------------------------------------------------------

test("R2b.13 a PRESENT optional property of the wrong type is refused", () => {
  const advertised = inputDoc();
  advertised["properties"]["context"] = { type: "string" };
  refusal(
    () => checkToolSchemas("collect", advertised, outputDoc()),
    "inputSchema.context",
    'requires type "object"',
  );
});

// --- the three dispositioned properties ----------------------------------
//
// Property 15, TestChildEnv_EmptyBlockIsAnEmptyEnvironmentNotInheritance:
// COVERED, no mirror written. It asserts that the client builds an EMPTY child
// environment rather than a nil one, which in Go means inheritance. That is a
// rule of the PARENT side of the spawn; its collector-side consequence is proven
// by the four environment arms among the eleven provider-dialing tests, run
// against this package's conformance stub through the contract harness's external
// seam. A mirror here would assert this package's own fixture.
//
// Property 14, TestContractSummary_StatesTheContextBlock: EXCLUDED BY NAME. It
// asserts over a one-line operator-facing string the client splices into its own
// registration tool's description. A collector library renders no such string and
// has nothing to assert.
//
// Property 16, TestRunMCP_RecordShapeGuards: EXCLUDED BY NAME. It hands the
// client's own runtime registration record five malformed shapes and requires a
// refusal before any transport is built. A collector library holds no such
// record.
//
// Do not invent mirrors of 14 or 16 to reach a count of sixteen.
