// stub.test.ts — R2a.3, R2a.4, R2a.6 and R2a.7: the sixteen conformance-stub
// modes, driven DIRECTLY as a child process rather than through the client's Go
// suite.
//
// THE DIVISION OF PROOF. These rows assert each mode's exact payload or exit at
// the byte level; the client's eleven provider-dialing tests, run against this
// same stub through the contract harness's external seam, assert that the client
// ACCEPTS what these rows describe. Neither subsumes the other, and this half
// runs with no Go toolchain at all.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { test } from "node:test";

import { STUB_MODES, conformingPayload, type StubMode } from "./conformance-stub.js";
import { compiled, initializeRequest, repoRoot, rpc, runChild, type ChildRun } from "./helpers.js";

type Msg = Record<string, any>;

const STUB = () => compiled("tests/conformance-stub.js");
const MODE_ENV = "FUL1776_STUB_MODE";

/** Handshake, list, call — the client's whole message surface, in order. */
function session(args: Record<string, unknown> = { id: "board" }, tool = "collect_graph"): string[] {
  return [
    initializeRequest(1, "2026-07-28"),
    rpc(null, "notifications/initialized"),
    rpc(2, "tools/list"),
    rpc(3, "tools/call", { name: tool, arguments: args }),
  ];
}

async function driveMode(
  mode: StubMode,
  args: Record<string, unknown> = { id: "board" },
  env: Record<string, string> = {},
  tool = "collect_graph",
): Promise<ChildRun> {
  return runChild(STUB(), session(args, tool), { [MODE_ENV]: mode, ...env });
}

const listing = (run: ChildRun): Msg => (run.messages.find((m) => (m as Msg)["id"] === 2) as Msg)["result"];
const callResult = (run: ChildRun): Msg =>
  (run.messages.find((m) => (m as Msg)["id"] === 3) as Msg)["result"];

test("R2a.3 the mode vocabulary is the client's own const block, read from its source", (t) => {
  // NO LITERAL LIST AND NO COUNT. This row used to transcribe the client's
  // sixteen mode names and pin the number beside them, and when the client's stub
  // gained its four describe arms what went red was a count — with a message
  // about sixteen rather than about the four modes this stub does not emulate. A
  // transcription agrees with itself forever; this reads the authority.
  assert.equal(new Set(STUB_MODES).size, STUB_MODES.length, "a mode is named twice");
  assert.ok(STUB_MODES.length > 1, "the mode vocabulary is empty; every row below would be vacuous");

  const root = repoRoot();
  if (root === undefined) {
    t.skip("the client's stub source lives in this repository only; running from the published layout");
    return;
  }
  const source = readFileSync(
    resolve(root, "cmd", "knowledge", "internal", "externalcollector", "stubprovider_test.go"),
    "utf8",
  );
  const start = source.indexOf("// Stub modes.");
  assert.ok(start >= 0, "the client's stub source carries no `// Stub modes.` block; the slice is broken");
  const block = source.slice(start, source.indexOf("\n)\n", start));
  const clientModes = [...block.matchAll(/^\s*stubMode\w*\s*=\s*"([^"]+)"/gm)].map((m) => m[1]!).sort();
  assert.ok(clientModes.length > 1, `the client's mode block yielded ${clientModes.join(", ")}; the matcher is broken`);
  assert.deepEqual([...STUB_MODES].sort(), clientModes);
});

test("R2a.3 conforming: three nodes, one edge, a complete walk, and the tool named collect_graph", async () => {
  const run = await driveMode("conforming");
  assert.equal(run.code, 0, run.stderr);
  const tool = (listing(run)["tools"] as Msg[])[0]!;
  assert.equal(tool["name"], "collect_graph");
  assert.deepEqual(callResult(run)["structuredContent"], conformingPayload());
});

test("R2a.4 conforming: ISSUE-2 is ABSENT-optional and ISSUE-3 is PRESENT-AND-EMPTY, field by field", async () => {
  const run = await driveMode("conforming");
  const nodes = callResult(run)["structuredContent"]["nodes"] as Msg[];
  assert.equal(nodes.length, 3);

  assert.equal(nodes[0]!["id"], "ISSUE-1");
  assert.equal(nodes[0]!["metadata"]["priority"], "high");
  assert.equal(nodes[0]!["is_exported"], true);
  assert.equal(nodes[0]!["start_line"], 10);

  assert.deepEqual(
    Object.keys(nodes[1]!).sort(),
    ["id", "type"],
    "ISSUE-2 carries id and type and NOTHING else on the wire",
  );

  const empty = nodes[2]!;
  assert.equal(empty["id"], "ISSUE-3");
  for (const key of [
    "symbol_name",
    "file_path",
    "language",
    "content",
    "signature",
    "summary",
    "description",
    "source",
    "status",
    "keywords",
  ]) {
    assert.ok(key in empty, `ISSUE-3 must carry ${key} PRESENT`);
    assert.equal(empty[key], "", `and empty`);
  }
  assert.equal(empty["start_line"], 0);
  assert.equal(empty["end_line"], 0);
  assert.equal(empty["is_exported"], false);
  assert.deepEqual(empty["metadata"], {}, "a metadata map sent as {} must stay present and empty");
});

test("R2a.3 env-report: one node per REQUESTED name and no other, with present and value", async () => {
  const run = await driveMode(
    "env-report",
    { id: "probe", params: { names: ["FUL1776_DECLARED", "HOME", "PATH"] } },
    { FUL1776_DECLARED: "block-value" },
  );
  const nodes = callResult(run)["structuredContent"]["nodes"] as Msg[];
  assert.equal(nodes.length, 3, "one node per requested name, and only per requested name");
  const byId = Object.fromEntries(nodes.map((n) => [n["id"], n]));

  assert.equal(byId["FUL1776_DECLARED"]!["type"], "env_var");
  assert.equal(byId["FUL1776_DECLARED"]!["metadata"]["present"], "true");
  assert.equal(byId["FUL1776_DECLARED"]!["metadata"]["value"], "block-value");

  // THE ENTRY'S BLOCK IS THE CHILD'S WHOLE ENVIRONMENT: HOME and PATH are set in
  // this test process and absent from the block, so an absent report here is a
  // fact this harness produced rather than an unset host variable.
  for (const absent of ["HOME", "PATH"]) {
    assert.ok(process.env[absent] !== undefined, `control: ${absent} is set in the parent`);
    assert.equal(byId[absent]!["metadata"]["present"], "false");
    assert.equal("value" in byId[absent]!["metadata"], false, "an absent variable carries no value at all");
  }
});

test("R2a.3 env-report: a name set to the EMPTY STRING arrives present and empty", async () => {
  const run = await driveMode(
    "env-report",
    { id: "probe", params: { names: ["FUL1776_DECLARED", "FUL1794_NOT_IN_THE_BLOCK"] } },
    { FUL1776_DECLARED: "" },
  );
  const nodes = callResult(run)["structuredContent"]["nodes"] as Msg[];
  const byId = Object.fromEntries(nodes.map((n) => [n["id"], n]));
  assert.equal(byId["FUL1776_DECLARED"]!["metadata"]["present"], "true");
  assert.equal(byId["FUL1776_DECLARED"]!["metadata"]["value"], "");
  assert.equal(byId["FUL1794_NOT_IN_THE_BLOCK"]!["metadata"]["present"], "false");
});

test("R2a.3 env-report: a call asking for NO names emits no env nodes at all", async () => {
  const run = await driveMode("env-report", { id: "probe" }, { FUL1776_DECLARED: "x" });
  assert.deepEqual(
    callResult(run)["structuredContent"],
    { nodes: [], edges: [], walk_complete: true },
    "the whole environment is never serialized; a mode handed no names emits nothing",
  );
});

test("R2a.3 no-output-schema: the tool is listed with NO outputSchema key", async () => {
  const run = await driveMode("no-output-schema");
  const tool = (listing(run)["tools"] as Msg[])[0]!;
  assert.equal("outputSchema" in tool, false);
  assert.ok("inputSchema" in tool, "control: the input schema is still advertised");
});

test("R2a.3 bad-output-schema: walk_complete is neither required nor declared", async () => {
  const run = await driveMode("bad-output-schema");
  const out = (listing(run)["tools"] as Msg[])[0]!["outputSchema"] as Msg;
  assert.deepEqual(out["required"], ["nodes", "edges"]);
  assert.equal("walk_complete" in (out["properties"] as Msg), false);
  assert.ok("nodes" in (out["properties"] as Msg), "control: the rest of the contract schema is intact");
});

test("R2a.3 bad-input-schema: an object that does not require the collect id", async () => {
  const run = await driveMode("bad-input-schema");
  const input = (listing(run)["tools"] as Msg[])[0]!["inputSchema"] as Msg;
  assert.deepEqual(input, { type: "object", properties: { params: { type: "object" } } });
});

test("R2a.3 breaks-own-word: advertises a STRICTER schema and returns a node violating it", async () => {
  const run = await driveMode("breaks-own-word");
  const out = (listing(run)["tools"] as Msg[])[0]!["outputSchema"] as Msg;
  assert.deepEqual(out["properties"]["nodes"]["items"]["required"], ["id", "type", "summary"]);
  const payload = callResult(run)["structuredContent"] as Msg;
  assert.deepEqual(payload["nodes"], [{ id: "n1", type: "issue" }]);
  assert.equal("summary" in (payload["nodes"] as Msg[])[0]!, false, "the returned node breaks that word");
});

test("R2a.3 tool-error: isError with the exact refusal text", async () => {
  const run = await driveMode("tool-error");
  const result = callResult(run);
  assert.equal(result["isError"], true);
  assert.deepEqual(result["content"], [
    { type: "text", text: "the stub provider refused this collect" },
  ]);
  assert.equal("structuredContent" in result, false);
});

test("R2a.3 empty-node-type: one node with an empty type", async () => {
  const run = await driveMode("empty-node-type");
  assert.deepEqual(callResult(run)["structuredContent"], {
    nodes: [{ id: "n1", type: "" }],
    edges: [],
    walk_complete: true,
  });
});

test("R2a.3 unknown-field: the misspelled key reaches the wire exactly as written", async () => {
  const run = await driveMode("unknown-field");
  assert.deepEqual(callResult(run)["structuredContent"], {
    nodes: [{ id: "n1", type: "issue", summry: "typo'd key" }],
    edges: [],
    walk_complete: true,
  });
});

test("R2a.3 dangling-edge: an edge to an endpoint the result does not carry", async () => {
  const run = await driveMode("dangling-edge");
  assert.deepEqual(callResult(run)["structuredContent"], {
    nodes: [{ id: "n1", type: "issue" }],
    edges: [{ from_id: "n1", to_id: "absent", type: "blocks" }],
    walk_complete: true,
  });
});

test("R2a.3 incomplete-walk: a conforming result asserting walk_complete FALSE", async () => {
  const run = await driveMode("incomplete-walk");
  assert.deepEqual(callResult(run)["structuredContent"], {
    nodes: [{ id: "n1", type: "issue" }],
    edges: [],
    walk_complete: false,
  });
});

test("R2a.3 empty-graph: zero nodes, zero edges, a complete walk", async () => {
  const run = await driveMode("empty-graph");
  assert.deepEqual(callResult(run)["structuredContent"], {
    nodes: [],
    edges: [],
    walk_complete: true,
  });
});

test("R2a.6 over-former-cap: 68 MiB of node bodies crosses the retired 64 MiB bound", async () => {
  const run = await driveMode("over-former-cap");
  assert.equal(run.code, 0, run.stderr);
  const payload = callResult(run)["structuredContent"] as Msg;
  const nodes = payload["nodes"] as Msg[];
  assert.equal(nodes.length, 68);
  assert.equal((payload["edges"] as Msg[]).length, 67);
  assert.equal((nodes[0]!["content"] as string).length, 1 << 20);
  assert.equal(nodes[67]!["id"], "big-67");

  const bytes = Buffer.byteLength(JSON.stringify(payload), "utf8");
  assert.ok(
    bytes > 64 * 1024 * 1024,
    `the payload must cross the FORMER cap with real bytes; it was ${bytes}`,
  );
  const frame = run.stdoutLines.find((l) => l.includes("big-67"))!;
  assert.ok(
    frame.length > 64 * 1024 * 1024,
    "and it must travel as ONE newline-delimited frame, not chunked into several",
  );
});

test("R2a.7 exit-before-handshake: dies non-zero WITHOUT speaking MCP", async () => {
  const run = await driveMode("exit-before-handshake");
  assert.equal(run.code, 3);
  assert.deepEqual(run.stdoutLines, [], "not one byte of protocol may reach stdout");
  assert.match(run.stderr, /exiting before the handshake, deliberately/);
});

test("R2a.7 exit-mid-session: handshakes, is listed, and only then dies INSIDE the call", async () => {
  const run = await driveMode("exit-mid-session");
  assert.equal(run.code, 7);
  const ids = run.messages.map((m) => (m as Msg)["id"]);
  assert.deepEqual(ids, [1, 2], "the handshake and the listing were answered");
  assert.equal(
    run.messages.some((m) => (m as Msg)["id"] === 3),
    false,
    "and the call was not — the process stopped existing with it in flight",
  );
  assert.match(run.stderr, /exiting mid-session, deliberately/);
});

test("R2a.7 the two death modes are DISTINCT arms and reach different code", async () => {
  const before = await driveMode("exit-before-handshake");
  const mid = await driveMode("exit-mid-session");
  assert.notEqual(before.code, mid.code);
  assert.equal(before.stdoutLines.length, 0);
  assert.ok(mid.stdoutLines.length >= 2);
});

test("R2a.3 no-such-tool: the provider lists some_other_tool", async () => {
  const run = await driveMode("no-such-tool");
  const tools = listing(run)["tools"] as Msg[];
  // TWO TOOLS, and the collect one is not named collect: the describe tool is
  // REQUIRED and every mode but no-describe serves it, so what this row is about
  // is the COLLECT tool's name rather than the size of the listing.
  assert.deepEqual(
    tools.map((tool) => tool["name"]).sort(),
    ["describe", "some_other_tool"],
  );
});

test("R2a.3 the tool name is overridable, and no-such-tool overrides the override", async () => {
  const named = await driveMode("conforming", { id: "board" }, { FUL1776_STUB_TOOL: "collect_logs" }, "collect_logs");
  assert.equal((listing(named)["tools"] as Msg[])[0]!["name"], "collect_logs");
  const other = await driveMode("no-such-tool", { id: "board" }, { FUL1776_STUB_TOOL: "collect_logs" });
  assert.equal((listing(other)["tools"] as Msg[])[0]!["name"], "some_other_tool");
});

test("R2a.3 every mode is reachable, and an unknown mode is refused loudly", async () => {
  // THE ANTI-VACUITY ROW. Every mode above is asserted individually; this one
  // proves the SET is complete, so a seventeenth mode added to the client's
  // harness and not to this stub cannot pass unnoticed as "not covered here".
  const served: string[] = [];
  for (const mode of STUB_MODES) {
    const run = await runChild(STUB(), [initializeRequest(1, "2025-06-18")], { [MODE_ENV]: mode });
    if (mode === "exit-before-handshake") {
      assert.equal(run.code, 3, `${mode} must die before speaking`);
    } else {
      assert.equal(run.messages.length, 1, `${mode} must answer the handshake`);
      assert.equal((run.messages[0] as Msg)["result"]["protocolVersion"], "2025-06-18");
    }
    served.push(mode);
  }
  assert.equal(served.length, STUB_MODES.length);

  const unknown = await runChild(STUB(), [], { [MODE_ENV]: "not-a-mode" });
  assert.equal(unknown.code, 9);
  assert.match(unknown.stderr, /is not one of the stub modes/);
  // AND IT NAMES THEM, which is the half a count could never carry.
  assert.match(unknown.stderr, new RegExp(STUB_MODES[0]!));

  const unset = await runChild(STUB(), [], {});
  assert.equal(unset.code, 9);
  assert.match(unset.stderr, /the entry's env block did not reach it/);
});

test("R1.12 the stub writes nothing to stdout but JSON-RPC, in every mode that speaks", async () => {
  for (const mode of STUB_MODES) {
    if (mode === "exit-before-handshake") continue;
    const run = await driveMode(mode);
    assert.ok(run.stdoutLines.length > 0, `control: ${mode} wrote something`);
    for (const line of run.stdoutLines) {
      const parsed = JSON.parse(line) as Msg;
      assert.equal(parsed["jsonrpc"], "2.0", `${mode} wrote a non-JSON-RPC line`);
    }
  }
});
