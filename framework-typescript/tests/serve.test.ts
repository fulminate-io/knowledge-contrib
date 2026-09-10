// serve.test.ts — R1.1, R1.8 and R1.17: the author surface, the validation that
// stands before the walk, and the foreign context reaching it.

import assert from "node:assert/strict";
import { test } from "node:test";

import { complete } from "../src/completeness.js";
import { checkToolSchemas } from "../src/contract.js";
import { FAMILY_CODE, type ForeignContext } from "../src/context.js";
import { type Declaration } from "../src/describe.js";
import { DEFAULT_TOOL_NAME, type Collector, type JsonSchema, type Result } from "../src/framework.js";
import { inputContractJSON, outputContractJSON } from "../src/schema.js";
import { newSpeakerDefinition } from "../src/serve.js";
import { driveSession, initializeRequest, rpc } from "./helpers.js";

type Msg = Record<string, any>;

interface Params {
  since?: string;
}

/** What one drive of the collector observed, so a test can assert on the walk's inputs. */
interface Seen {
  id?: string;
  params?: Params;
  foreign?: ForeignContext;
  calls: number;
}

/** A declaration every fixture serves unless it overrides one. */
export function fixtureDeclaration(): Declaration {
  return {
    behavior: { summarizable: true, embeddable: false, syncable: true, embedFields: ["summary"] },
    nodeTypes: ["issue"],
    edgeTypes: ["blocks"],
    environment: [{ name: "SAMPLE_TOKEN", class: "secret" }],
  };
}

function collectorUnder(
  seen: Seen,
  over: Partial<{
    tool: () => { name?: string; description?: string };
    paramsSchema: () => JsonSchema;
    describe: () => Declaration;
    walk: Collector<Params>["walk"];
  }> = {},
): Collector<Params> {
  return {
    tool: over.tool ?? (() => ({ name: "collect_graph", description: "a sample" })),
    describe: over.describe ?? fixtureDeclaration,
    paramsSchema:
      over.paramsSchema ??
      (() => ({ type: "object", properties: { since: { type: "string" } }, additionalProperties: false })),
    walk:
      over.walk ??
      ((id, params, foreign): Result => {
        seen.id = id;
        seen.params = params;
        seen.foreign = foreign;
        seen.calls += 1;
        return { nodes: [{ id: "n1", type: "issue" }], complete: complete() };
      }),
  };
}

async function call(
  collector: Collector<Params>,
  args: unknown,
  toolName = "collect_graph",
): Promise<Msg> {
  const s = driveSession(newSpeakerDefinition(collector));
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/call", { name: toolName, arguments: args }))) as Msg[];
  return reply!;
}

// --- R1.1 ----------------------------------------------------------------

test("R1.1 a collector written against the public exports alone is served", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(collectorUnder(seen), { id: "board", params: { since: "2026-01-01" } });
  assert.equal(seen.calls, 1);
  assert.equal(seen.id, "board");
  assert.deepEqual(seen.params, { since: "2026-01-01" });
  assert.deepEqual(reply["result"]["structuredContent"], {
    nodes: [{ id: "n1", type: "issue" }],
    edges: [],
    walk_complete: true,
  });
});

test("R1.1 the served tool advertises schemas the client's own gate admits", async () => {
  const seen: Seen = { calls: 0 };
  const s = driveSession(newSpeakerDefinition(collectorUnder(seen)));
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/list"))) as Msg[];
  const tool = (reply!["result"]["tools"] as Msg[])[0]!;
  assert.equal(tool["name"], "collect_graph");
  assert.doesNotThrow(
    () => checkToolSchemas("collect_graph", tool["inputSchema"], tool["outputSchema"]),
    "this is the gate the client runs at registration and again at collect",
  );
  // And the advertised documents are the contract files, structurally.
  assert.deepEqual(tool["outputSchema"], JSON.parse(outputContractJSON()));
  const contractInput = JSON.parse(inputContractJSON()) as Msg;
  assert.deepEqual(tool["inputSchema"]["required"], ["id"]);
  assert.deepEqual(tool["inputSchema"]["properties"]["id"], contractInput["properties"]["id"]);
});

test("R1.10 serverInfo.name is the RESOLVED TOOL NAME, decided by newSpeakerDefinition", () => {
  // THE ROW IN speaker.test.ts CANNOT OBSERVE THIS. It hands the speaker a
  // definition it built itself and asserts the speaker echoes that definition's
  // serverName, which is a true property of the speaker and NOT the requirement:
  // the requirement is that newSpeakerDefinition puts the resolved tool name
  // there. Nothing in that row reaches this function, so hardcoding a package
  // name here left it green.
  const seen: Seen = { calls: 0 };
  assert.equal(
    newSpeakerDefinition(collectorUnder(seen, { tool: () => ({ name: "collect_logs" }) })).serverName,
    "collect_logs",
    "an operator matching a session to a config entry reads the tool, so serverInfo.name is the tool's",
  );
  assert.equal(
    newSpeakerDefinition(collectorUnder(seen, { tool: () => ({}) })).serverName,
    DEFAULT_TOOL_NAME,
    "and a spec naming no tool resolves to the default before it reaches serverInfo",
  );
  // THE CONTROL: the served tool carries the same name, so the two cannot drift
  // apart with this row still green.
  const def = newSpeakerDefinition(collectorUnder(seen, { tool: () => ({ name: "collect_logs" }) }));
  assert.equal(def.tools[0]!.name, def.serverName);
});

test("R1.1 a tool spec naming no name serves the default", async () => {
  const seen: Seen = { calls: 0 };
  const s = driveSession(newSpeakerDefinition(collectorUnder(seen, { tool: () => ({}) })));
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/list"))) as Msg[];
  assert.equal((reply!["result"]["tools"] as Msg[])[0]!["name"], "collect");
  assert.equal(reply!["result"] !== undefined, true);
});

// --- R1.8 ----------------------------------------------------------------

const REFUSED_ARGUMENTS: Array<{ name: string; args: unknown; names: string }> = [
  { name: "no id at all", args: {}, names: "id" },
  { name: "an id of the wrong type", args: { id: 42 }, names: "id" },
  { name: "params of the wrong type", args: { id: "board", params: "not-an-object" }, names: "params" },
  {
    name: "params failing the collector's OWN declared schema",
    args: { id: "board", params: { since: 7 } },
    names: "since",
  },
  {
    name: "params carrying a key the collector's schema forbids",
    args: { id: "board", params: { untilx: "x" } },
    names: "untilx",
  },
];

for (const tc of REFUSED_ARGUMENTS) {
  test(`R1.8 arguments refused BEFORE the walk: ${tc.name}`, async () => {
    const seen: Seen = { calls: 0 };
    const reply = await call(collectorUnder(seen), tc.args);
    assert.equal(seen.calls, 0, "the walk must never run on arguments the advertised schema refuses");
    assert.equal(reply["result"]["isError"], true);
    const text = (reply["result"]["content"] as Msg[])[0]!["text"] as string;
    assert.ok(
      text.includes(tc.names),
      `the refusal must name the offending property; it said ${JSON.stringify(text)}`,
    );
    assert.equal("structuredContent" in reply["result"], false);
  });
}

test("R1.8 an EMPTY id is refused too: the schema can require a string, not a non-empty one", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(collectorUnder(seen), { id: "" });
  assert.equal(seen.calls, 0);
  assert.equal(reply["result"]["isError"], true);
  assert.match((reply["result"]["content"] as Msg[])[0]!["text"] as string, /collect id is empty/);
});

test("R1.8 control: a call with NO params at all reaches the walk, because params is optional", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(collectorUnder(seen), { id: "board" });
  assert.equal(seen.calls, 1, "a paramless collect must not be refused — the client omits params entirely");
  assert.deepEqual(seen.params, {});
  assert.equal(reply["result"]["isError"], undefined);
});

test("R1.8 the RESULT is validated against the contract output schema before it leaves", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(
    collectorUnder(seen, {
      // A walk that returns a node the contract's node item schema refuses. The
      // encoder passes it through — id is a string here — so the result-shape
      // validation is the only thing between this and the wire.
      walk: (): Result => ({
        nodes: [{ id: 7 as unknown as string, type: "issue" }],
        complete: complete(),
      }),
    }),
    { id: "board" },
  );
  assert.equal(reply["result"]["isError"], true);
  const text = (reply["result"]["content"] as Msg[])[0]!["text"] as string;
  assert.match(text, /collect_graph/);
  assert.match(text, /id/);
});

test("R1.8 an encoder refusal reaches the caller as isError naming the collector", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(
    collectorUnder(seen, {
      walk: (): Result => ({ nodes: [{ id: "n1", type: "" }], complete: complete() }),
    }),
    { id: "board" },
  );
  assert.equal(reply["result"]["isError"], true);
  assert.match((reply["result"]["content"] as Msg[])[0]!["text"] as string, /empty type/);
});

test("R1.8 the handler refuses a call naming another tool, so no walk runs under a name it never advertised", async () => {
  const seen: Seen = { calls: 0 };
  const def = newSpeakerDefinition(collectorUnder(seen));
  const result = await def.call("some_other_tool", { id: "board" });
  assert.equal(seen.calls, 0);
  assert.equal(result.isError, true);
  assert.match(result.content![0]!.text, /some_other_tool/);
  assert.match(result.content![0]!.text, /collect_graph/);
});

test("R1.8 a params schema that is not an object schema is refused when the server is built", () => {
  assert.throws(
    () =>
      newSpeakerDefinition(
        collectorUnder({ calls: 0 }, { paramsSchema: () => ({ type: "string" }) }),
      ),
    /declares params as an object/,
  );
});

// --- R1.17 ---------------------------------------------------------------

const CONTEXT_BLOCK = {
  code: [
    {
      graph_name: "knowledge",
      nodes: [
        { id: "pkg/a.go:Fn", type: "function", symbol_name: "Fn", file_path: "pkg/a.go", metadata: { pkg: "a" } },
      ],
      edges: [{ from_id: "pkg/a.go:Fn", to_id: "pkg/b.go:Gn" }],
    },
  ],
  "acme-aws": [{ graph_name: "prod", nodes: [], edges: [] }],
};

test("R1.17 a call carrying a context block reaches the walk decoded, families and graphs intact", async () => {
  const seen: Seen = { calls: 0 };
  await call(collectorUnder(seen), { id: "board", context: CONTEXT_BLOCK });
  const foreign = seen.foreign!;
  assert.equal(foreign.isEmpty(), false);
  assert.deepEqual(foreign.families(), ["acme-aws", "code"], "families are sorted, so one input is one result");

  const code = foreign.graphs(FAMILY_CODE)!;
  assert.equal(code.length, 1);
  assert.equal(code[0]!.graphName, "knowledge");
  assert.equal(code[0]!.nodes.length, 1);
  assert.deepEqual(code[0]!.nodes[0], {
    id: "pkg/a.go:Fn",
    type: "function",
    symbolName: "Fn",
    filePath: "pkg/a.go",
    metadata: { pkg: "a" },
  });
  assert.deepEqual(code[0]!.edges[0], { fromId: "pkg/a.go:Fn", toId: "pkg/b.go:Gn" });

  // A DECLARED FAMILY THAT MATCHED NO GRAPH IS PRESENT AND EMPTY; an undeclared
  // one is absent. They are different facts and the accessors keep them apart.
  assert.deepEqual(foreign.graphs("acme-aws")![0]!.nodes, []);
  assert.equal(foreign.graphs("never-declared"), undefined);
  assert.deepEqual(foreign.graphsOrEmpty("never-declared"), []);
  assert.deepEqual(
    foreign.except(FAMILY_CODE).map((g) => g.graphName),
    ["prod"],
  );
});

test("R1.17 a call carrying NO context leaves the walk's context empty and changes nothing else", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(collectorUnder(seen), { id: "board", params: { since: "x" } });
  assert.equal(seen.foreign!.isEmpty(), true);
  assert.deepEqual(seen.foreign!.families(), []);
  assert.equal(seen.foreign!.graphs(FAMILY_CODE), undefined);
  assert.deepEqual(seen.foreign!.except(), []);
  assert.equal(seen.id, "board");
  assert.deepEqual(seen.params, { since: "x" });
  assert.deepEqual(reply["result"]["structuredContent"]["nodes"], [{ id: "n1", type: "issue" }]);
});

test("R1.17 the advertised input schema carries the context property", async () => {
  const seen: Seen = { calls: 0 };
  const s = driveSession(newSpeakerDefinition(collectorUnder(seen)));
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/list"))) as Msg[];
  const input = (reply!["result"]["tools"] as Msg[])[0]!["inputSchema"] as Msg;
  assert.equal(input["properties"]["context"]["type"], "object");
  assert.equal(
    (input["required"] as string[]).includes("context"),
    false,
    "declared, never required: a required context would refuse every provider written before it existed",
  );
});

test("R1.17 a LARGE context block is accepted whole, neither truncated nor refused", async () => {
  const nodes = Array.from({ length: 5000 }, (_, i) => ({
    id: `pkg/f${i}.go:Fn`,
    type: "function",
    content: "x".repeat(256),
  }));
  const seen: Seen = { calls: 0 };
  await call(collectorUnder(seen), { id: "board", context: { code: [{ graph_name: "big", nodes, edges: [] }] } });
  const graphs = seen.foreign!.graphs(FAMILY_CODE)!;
  assert.equal(graphs[0]!.nodes.length, 5000, "every declared node must reach the walk");
  assert.equal(graphs[0]!.nodes[4999]!.id, "pkg/f4999.go:Fn");
  assert.equal(graphs[0]!.nodes[4999]!.content!.length, 256);
});

test("R1.17 a context that is not an object is refused by the advertised schema, before the walk", async () => {
  for (const bad of [null, 7, "a string", []]) {
    const seen: Seen = { calls: 0 };
    const reply = await call(collectorUnder(seen), { id: "board", context: bad });
    assert.equal(seen.calls, 0, `context ${JSON.stringify(bad)} must not reach the walk`);
    assert.equal(reply["result"]["isError"], true);
    assert.match((reply["result"]["content"] as Msg[])[0]!["text"] as string, /context/);
  }
});

test("R1.17 a context object with a malformed interior ERRORS, naming the family", async () => {
  for (const bad of [{ code: "not-an-array" }, { code: [7] }, { code: [{ nodes: [] }] }]) {
    const seen: Seen = { calls: 0 };
    const reply = await call(collectorUnder(seen), { id: "board", context: bad });
    assert.equal(seen.calls, 0, `context ${JSON.stringify(bad)} must not reach the walk`);
    assert.equal(reply["result"]["isError"], true, "bad input errors; it is never coerced to an empty block");
    assert.match((reply["result"]["content"] as Msg[])[0]!["text"] as string, /code/);
  }
});

test("R1.17 a key this package does not name inside a foreign node is ignored, not refused", async () => {
  const seen: Seen = { calls: 0 };
  const reply = await call(collectorUnder(seen), {
    id: "board",
    context: {
      code: [
        {
          graph_name: "knowledge",
          nodes: [{ id: "a", type: "function", a_field_added_later: "x" }],
          edges: [{ from_id: "a", to_id: "b", weight: 1 }],
        },
      ],
    },
  });
  assert.equal(reply["result"]["isError"], undefined);
  const graph = seen.foreign!.graphs(FAMILY_CODE)![0]!;
  assert.deepEqual(graph.nodes[0], { id: "a", type: "function" });
  assert.deepEqual(graph.edges[0], { fromId: "a", toId: "b" });
});
