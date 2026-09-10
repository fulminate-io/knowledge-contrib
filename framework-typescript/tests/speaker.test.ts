// speaker.test.ts — R1.9, R1.10, R1.12, R1.14, R1.15 and R1.16: the MCP surface
// this package now owns, asserted at the wire rather than through a client.

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  PREFERRED_PROTOCOL_VERSION,
  SUPPORTED_PROTOCOL_VERSIONS,
  negotiateProtocolVersion,
  type CallToolResult,
  type SpeakerDefinition,
} from "../src/speaker.js";
import { driveSession, initializeRequest, rpc } from "./helpers.js";

type Msg = Record<string, any>;

const TOOL = {
  name: "collect",
  description: "a test collector",
  inputSchema: { type: "object", required: ["id"], properties: { id: { type: "string" } } },
  outputSchema: { type: "object", required: ["nodes", "edges", "walk_complete"], properties: {} },
};

function speaker(call?: SpeakerDefinition["call"]): SpeakerDefinition {
  return {
    serverName: "collect",
    serverVersion: "v1",
    tools: [TOOL],
    call:
      call ??
      ((_name, args): CallToolResult => ({ structuredContent: { echoed: args } })),
  };
}

test("R1.9 initialize answers with a negotiated version, tool capability and serverInfo", async () => {
  const s = driveSession(speaker());
  const [reply] = (await s.send(initializeRequest(1, "2025-06-18"))) as Msg[];
  assert.equal(reply!["jsonrpc"], "2.0");
  assert.equal(reply!["id"], 1);
  assert.equal(reply!["result"]["protocolVersion"], "2025-06-18");
  assert.ok(reply!["result"]["capabilities"]["tools"] !== undefined, "the server declares the tools capability");
  assert.equal(reply!["result"]["serverInfo"]["version"], "v1");
});

test("R1.10 serverInfo.name equals the TOOL name", async () => {
  const s = driveSession({ ...speaker(), serverName: "collect_logs" });
  const [reply] = (await s.send(initializeRequest(1, "2025-06-18"))) as Msg[];
  assert.equal(
    reply!["result"]["serverInfo"]["name"],
    "collect_logs",
    "an operator matching a session to a config entry reads the tool, not a package name",
  );
});

test("R1.9 notifications/initialized is accepted and answered with nothing", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  const out = await s.send(rpc(null, "notifications/initialized"));
  assert.deepEqual(out, [], "a notification carries no id and must draw no response");
});

test("R1.9 tools/list carries BOTH schemas, key for key", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/list"))) as Msg[];
  const tools = reply!["result"]["tools"] as Msg[];
  assert.equal(tools.length, 1);
  assert.equal(tools[0]!["name"], "collect");
  assert.equal(tools[0]!["description"], "a test collector");
  // STRUCTURAL, never a serialized-string comparison.
  assert.deepEqual(tools[0]!["inputSchema"], TOOL.inputSchema);
  assert.deepEqual(tools[0]!["outputSchema"], TOOL.outputSchema);
});

test("R1.9 a tool with NO output schema omits the key entirely rather than sending null", async () => {
  const noOutput: SpeakerDefinition = {
    ...speaker(),
    tools: [{ name: "collect", inputSchema: { type: "object" } }],
  };
  const s = driveSession(noOutput);
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(rpc(2, "tools/list"))) as Msg[];
  const tool = (reply!["result"]["tools"] as Msg[])[0]!;
  assert.equal("outputSchema" in tool, false, "an absent schema is an absent key, never a null one");
});

test("R1.9 tools/call returns structuredContent", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(
    rpc(3, "tools/call", { name: "collect", arguments: { id: "board" } }),
  )) as Msg[];
  assert.equal(reply!["id"], 3);
  assert.deepEqual(reply!["result"]["structuredContent"], { echoed: { id: "board" } });
  assert.ok(!("error" in reply!), "a successful call is a JSON-RPC result, not an error");
});

test("R1.9 a handler that throws becomes isError with text, never a JSON-RPC error and never an empty success", async () => {
  const s = driveSession(
    speaker(() => {
      throw new Error("the walk fell over");
    }),
  );
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(
    rpc(3, "tools/call", { name: "collect", arguments: { id: "board" } }),
  )) as Msg[];
  assert.ok(!("error" in reply!), "a tool failure is a RESULT with isError, which is what the client reads");
  assert.equal(reply!["result"]["isError"], true);
  const content = reply!["result"]["content"] as Msg[];
  assert.equal(content[0]!["type"], "text");
  assert.match(content[0]!["text"] as string, /the walk fell over/);
  assert.equal(
    "structuredContent" in reply!["result"],
    false,
    "a failed call must carry no structured result for a caller to admit",
  );
});

test("R1.9 calling a tool the speaker does not serve is an isError result naming both names", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  const [reply] = (await s.send(
    rpc(3, "tools/call", { name: "some_other_tool", arguments: { id: "board" } }),
  )) as Msg[];
  assert.equal(reply!["result"]["isError"], true);
  const text = (reply!["result"]["content"] as Msg[])[0]!["text"] as string;
  assert.match(text, /some_other_tool/);
  assert.match(text, /collect/);
});

test("R1.15 protocol negotiation answers the caller's revision when supported", () => {
  for (const v of SUPPORTED_PROTOCOL_VERSIONS) {
    assert.equal(negotiateProtocolVersion(v), v);
  }
});

test("R1.15 an unsupported, malformed or absent protocolVersion is answered with a supported one", () => {
  for (const bad of ["2024-11-05", "1999-01-01", "", 7, null, undefined, {}, []]) {
    const answered = negotiateProtocolVersion(bad);
    assert.equal(
      answered,
      PREFERRED_PROTOCOL_VERSION,
      `a request for ${JSON.stringify(bad)} must be answered with the contract's feature floor`,
    );
    assert.ok(
      SUPPORTED_PROTOCOL_VERSIONS.includes(answered),
      "the speaker never answers a revision it cannot speak",
    );
  }
});

test("R1.15 the negotiated version reaches the wire, and the client's own request is one of them", async () => {
  // 2026-07-28 is what the client's Go SDK asks for; it must be echoed rather
  // than answered with something older, which would make the client negotiate
  // down for no reason.
  const s = driveSession(speaker());
  const [reply] = (await s.send(initializeRequest(1, "2026-07-28"))) as Msg[];
  assert.equal(reply!["result"]["protocolVersion"], "2026-07-28");
  assert.equal(s.session.negotiatedVersion(), "2026-07-28");
});

test("R1.14 an unknown METHOD with an id is answered with JSON-RPC method-not-found, and the session continues", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  for (const method of ["ping", "notifications/cancelled/x", "logging/setLevel", "resources/list"]) {
    const out = (await s.send(rpc(9, method, {}))) as Msg[];
    assert.equal(out.length, 1, `${method} must draw exactly one response`);
    assert.equal(out[0]!["id"], 9);
    assert.equal(out[0]!["error"]["code"], -32601, `${method} must be method-not-found`);
    assert.match(out[0]!["error"]["message"] as string, /method not found/i);
  }
  // THE SESSION CONTINUES: a call after four unknown methods still works.
  const [reply] = (await s.send(
    rpc(10, "tools/call", { name: "collect", arguments: { id: "board" } }),
  )) as Msg[];
  assert.deepEqual(reply!["result"]["structuredContent"], { echoed: { id: "board" } });
});

test("R1.14 an unknown NOTIFICATION is ignored, not answered and not fatal", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  for (const method of ["notifications/cancelled", "notifications/progress", "$/anything"]) {
    assert.deepEqual(await s.send(rpc(null, method, {})), [], `${method} must draw no response`);
  }
  const [reply] = (await s.send(rpc(10, "tools/list"))) as Msg[];
  assert.equal((reply!["result"]["tools"] as unknown[]).length, 1);
});

test("R1.16 a line that is not JSON is answered with a parse error and the session survives", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  const out = (await s.send("this is not json {")) as Msg[];
  assert.equal(out.length, 1);
  assert.equal(out[0]!["id"], null, "a line that did not parse has no id to answer");
  assert.equal(out[0]!["error"]["code"], -32700);
  const [reply] = (await s.send(rpc(11, "tools/list"))) as Msg[];
  assert.equal((reply!["result"]["tools"] as unknown[]).length, 1);
});

test("R1.16 a blank line is ignored entirely", async () => {
  const s = driveSession(speaker());
  assert.deepEqual(await s.send(""), []);
  assert.deepEqual(await s.send("   "), []);
});

test("R1.16 JSON that is not a JSON-RPC message is refused or ignored, never fatal", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));

  // A JSON scalar and a JSON array carry no id to answer, so they are ignored.
  assert.deepEqual(await s.send("42"), []);
  assert.deepEqual(await s.send('"a string"'), []);
  assert.deepEqual(await s.send("null"), []);
  assert.deepEqual(await s.send("[1,2,3]"), []);

  // An object with an id but no usable method is an invalid request.
  const out = (await s.send(JSON.stringify({ jsonrpc: "2.0", id: 5 }))) as Msg[];
  assert.equal(out.length, 1);
  assert.equal(out[0]!["id"], 5);
  assert.equal(out[0]!["error"]["code"], -32600);

  const [reply] = (await s.send(rpc(12, "tools/list"))) as Msg[];
  assert.equal((reply!["result"]["tools"] as unknown[]).length, 1);
});

test("R1.12 every line the speaker writes is one JSON-RPC message and nothing else", async () => {
  const s = driveSession(speaker());
  await s.send(initializeRequest(1, "2025-06-18"));
  await s.send(rpc(null, "notifications/initialized"));
  await s.send(rpc(2, "tools/list"));
  await s.send(rpc(3, "tools/call", { name: "collect", arguments: { id: "board" } }));
  await s.send("not json");

  assert.ok(s.rawLines.length > 0, "control: the speaker wrote something to compare");
  for (const line of s.rawLines) {
    assert.equal(line.endsWith("\n"), true, "every message is newline-terminated framing");
    assert.equal(line.slice(0, -1).includes("\n"), false, "and carries no interior newline");
    const parsed = JSON.parse(line) as Msg;
    assert.equal(parsed["jsonrpc"], "2.0");
    assert.ok("result" in parsed || "error" in parsed, "every written line answers something");
  }
});
