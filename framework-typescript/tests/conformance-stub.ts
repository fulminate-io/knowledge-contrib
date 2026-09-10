// conformance-stub.ts — the TEST-ONLY CONFORMANCE STUB the client's own contract
// tests drive, in the sixteen shapes the contract has to be proven against.
//
// IT IS NOT THE SAMPLE COLLECTOR AND MUST NEVER BE MISTAKEN FOR ONE. The client's
// eleven provider-dialing tests assert the Go stub's EXACT payloads and strings —
// ISSUE-1 through ISSUE-3 with the absent-versus-present-and-empty boundary, a
// tool named collect_graph, the literal refusal text, the misspelled "summry"
// key, per-name environment lookups, two process deaths and a result over the
// former 64 MiB bound — and one of them asserts the stdio result renders
// byte-equal to an in-process HTTP stub's. So this file is a TRANSCRIPTION of
// cmd/knowledge/internal/externalcollector/stubprovider_test.go, not an
// implementation with latitude. Its source of truth is that file.
//
// IT REUSES THIS PACKAGE'S OWN SPEAKER rather than carrying a second one. The
// modes are payload and schema choices, not protocol ones: the speaker validates
// only what a caller asks it to, so a mode that advertises a deliberately
// malformed schema or returns a payload violating its own advertised one is
// unobstructed. A second protocol implementation would be a second thing to keep
// correct, and driving the eleven against it would prove nothing about the one
// that ships.
//
// IT WRITES NOTHING TO STDOUT BUT JSON-RPC. Every diagnostic goes to stderr.

import { DESCRIBE_TOOL_NAME, describeInputSchema } from "../src/describe.js";
import type { JsonSchema } from "../src/framework.js";
import { describeContractJSON, inputContractJSON, outputContractJSON } from "../src/schema.js";
import { isMainModule } from "../src/serve.js";
import { serveSpeakerOverStdio, type CallToolResult } from "../src/speaker.js";

type JsonSchemaDoc = JsonSchema;

/** Switches this file from "be nothing" to "be an MCP provider in one mode". */
const STUB_MODE_ENV = "FUL1776_STUB_MODE";
/** Names the tool the stub advertises; empty means the default. */
const STUB_TOOL_ENV = "FUL1776_STUB_TOOL";

const DEFAULT_STUB_TOOL = "collect_graph";

/**
 * The modes, in the order the client's own const block declares them. NO COUNT IS
 * WRITTEN BESIDE THE LIST: a number next to a list is a second thing to keep true
 * and it is the one that rots.
 */
export const STUB_MODES = [
  "conforming",
  "env-report",
  "no-output-schema",
  "bad-output-schema",
  "bad-input-schema",
  "breaks-own-word",
  "tool-error",
  "empty-node-type",
  "unknown-field",
  "dangling-edge",
  "incomplete-walk",
  "empty-graph",
  "over-former-cap",
  "exit-before-handshake",
  "exit-mid-session",
  "no-such-tool",
  // The four DESCRIBE arms. The describe tool is REQUIRED, so each of these is
  // one way a provider fails the requirement rather than a variation on serving
  // it.
  "no-describe",
  "bad-describe-schema",
  "bad-declaration",
  "describe-error",
] as const;

export type StubMode = (typeof STUB_MODES)[number];

const contractSchema = (raw: string): JsonSchemaDoc => JSON.parse(raw) as JsonSchemaDoc;

/** The input schema this mode advertises. */
export function stubInputSchema(mode: StubMode): JsonSchemaDoc {
  if (mode === "bad-input-schema") {
    // Type object, but it does not require the collect id — the one thing the
    // contract's input side insists on.
    return { type: "object", properties: { params: { type: "object" } } };
  }
  return contractSchema(inputContractJSON());
}

/** The output schema this mode advertises, or undefined for none at all. */
export function stubOutputSchema(mode: StubMode): JsonSchemaDoc | undefined {
  switch (mode) {
    case "no-output-schema":
      return undefined;
    case "bad-output-schema": {
      // Conforming but for the completeness assertion, which is exactly the
      // omission the contract exists to refuse.
      const out = contractSchema(outputContractJSON());
      out["required"] = ["nodes", "edges"];
      delete (out["properties"] as Record<string, unknown>)["walk_complete"];
      return out;
    }
    case "breaks-own-word": {
      // STRICTER than the contract: every node must also carry a summary. The
      // handler then returns one that does not.
      const out = contractSchema(outputContractJSON());
      const props = out["properties"] as Record<string, Record<string, any>>;
      props["nodes"]!["items"]["required"] = ["id", "type", "summary"];
      return out;
    }
    default:
      return contractSchema(outputContractJSON());
  }
}

/** The tool name this mode advertises. */
export function stubToolName(mode: StubMode, configured: string): string {
  const name = configured === "" ? DEFAULT_STUB_TOOL : configured;
  return mode === "no-such-tool" ? "some_other_tool" : name;
}

/**
 * The reference result: three nodes, one edge, a complete walk, every optional
 * field exercised on one node, absent on another and PRESENT-AND-EMPTY on a
 * third.
 *
 * ISSUE-3 IS NOT A DUPLICATE OF ISSUE-2. Absent and empty-valued are distinct
 * inputs to the JSON-Schema validation and to the client's strict decode, and one
 * of the eleven asserts the stdio result renders byte-equal to the in-process
 * HTTP stub's — so a serializer that dropped empty strings would turn ISSUE-3
 * into ISSUE-2 and fail that equality with a diff that reads like a transport
 * bug.
 */
export function conformingPayload(): unknown {
  return {
    nodes: [
      {
        id: "ISSUE-1",
        type: "issue",
        symbol_name: "Login broken",
        file_path: "src/login.go",
        language: "go",
        start_line: 10,
        end_line: 20,
        content: "body",
        signature: "sig",
        summary: "one line",
        description: "longer",
        source: "stub",
        status: "open",
        keywords: "login auth",
        is_exported: true,
        metadata: { priority: "high" },
      },
      { id: "ISSUE-2", type: "issue" },
      {
        id: "ISSUE-3",
        type: "issue",
        symbol_name: "",
        file_path: "",
        language: "",
        content: "",
        signature: "",
        summary: "",
        description: "",
        source: "",
        status: "",
        keywords: "",
        start_line: 0,
        end_line: 0,
        is_exported: false,
        metadata: {},
      },
    ],
    edges: [{ from_id: "ISSUE-1", to_id: "ISSUE-2", type: "blocks" }],
    walk_complete: true,
  };
}

/**
 * A conforming result LARGER than the 64 MiB bound the client used to enforce. It
 * exists because the retired over-cap tests reached their boundary by lowering
 * the cap, and there is no cap left to lower: crossing the former bound with real
 * bytes is the only way to prove it no longer applies.
 */
export function overFormerCapPayload(): unknown {
  const bodyLen = 1 << 20;
  const nodeRows = 68;
  const body = "x".repeat(bodyLen);
  const nodes: unknown[] = [];
  const edges: unknown[] = [];
  for (let i = 0; i < nodeRows; i++) {
    nodes.push({ id: `big-${i}`, type: "blob", content: body });
    if (i > 0) edges.push({ from_id: `big-${i - 1}`, to_id: `big-${i}`, type: "NEXT" });
  }
  return { nodes, edges, walk_complete: true };
}

/**
 * Answers the environment probe. It reports ONLY the names the caller asked
 * about, one node each, carrying whether the variable is present in this child's
 * environment and — for a present one — its value.
 *
 * IT NEVER SERIALIZES THE WHOLE ENVIRONMENT. A stub that dumped its environment
 * into a result the collect then admitted would write whatever the process held
 * into a graph — the very shape the contract exists to close, reproduced inside
 * its own test.
 */
export function envReportPayload(
  args: Record<string, unknown>,
  env: Record<string, string | undefined>,
): unknown {
  const nodes: unknown[] = [];
  for (const name of askedNames(args)) {
    const present = Object.prototype.hasOwnProperty.call(env, name);
    const metadata: Record<string, string> = { present: String(present) };
    if (present) metadata["value"] = env[name] ?? "";
    nodes.push({ id: name, type: "env_var", metadata });
  }
  return { nodes, edges: [], walk_complete: true };
}

/** Pulls the requested variable names out of the call arguments. */
export function askedNames(args: Record<string, unknown>): string[] {
  const params = args["params"];
  if (typeof params !== "object" || params === null) return [];
  const names = (params as Record<string, unknown>)["names"];
  if (!Array.isArray(names)) return [];
  return names.filter((n): n is string => typeof n === "string");
}

/** The structured content for one mode. */
export function stubPayload(
  mode: StubMode,
  args: Record<string, unknown>,
  env: Record<string, string | undefined>,
): unknown {
  switch (mode) {
    case "env-report":
      return envReportPayload(args, env);
    case "empty-node-type":
      return { nodes: [{ id: "n1", type: "" }], edges: [], walk_complete: true };
    case "unknown-field":
      return {
        nodes: [{ id: "n1", type: "issue", summry: "typo'd key" }],
        edges: [],
        walk_complete: true,
      };
    case "dangling-edge":
      return {
        nodes: [{ id: "n1", type: "issue" }],
        edges: [{ from_id: "n1", to_id: "absent", type: "blocks" }],
        walk_complete: true,
      };
    case "incomplete-walk":
      return { nodes: [{ id: "n1", type: "issue" }], edges: [], walk_complete: false };
    case "empty-graph":
      return { nodes: [], edges: [], walk_complete: true };
    case "over-former-cap":
      return overFormerCapPayload();
    case "breaks-own-word":
      // No summary, which the schema this stub advertised requires.
      return { nodes: [{ id: "n1", type: "issue" }], edges: [], walk_complete: true };
    default:
      return conformingPayload();
  }
}

/** The tool-call result for one mode, or a process death. */
export function stubCall(
  mode: StubMode,
  args: Record<string, unknown>,
  env: Record<string, string | undefined>,
  exit: (code: number) => never,
): CallToolResult {
  if (mode === "exit-mid-session") {
    // Dies WITH THE CALL IN FLIGHT. An explicit exit rather than a thrown error:
    // the arm under test is a provider that stops existing, not one that reports
    // a failure, and in Node an async handler that throws does not end the
    // process at all.
    process.stderr.write("stub provider: exiting mid-session, deliberately\n");
    exit(7);
  }
  if (mode === "tool-error") {
    return {
      isError: true,
      content: [{ type: "text", text: "the stub provider refused this collect" }],
    };
  }
  return { structuredContent: stubPayload(mode, args, env) };
}

/** Whether a name is one of the sixteen. */
export function isStubMode(name: string): name is StubMode {
  return (STUB_MODES as readonly string[]).includes(name);
}

/** The one environment NAME the stub's declaration declares, the client stub's own spelling. */
const DECLARED_ENV = "FUL1776_DECLARED";

/**
 * The describe output schema this mode advertises. The bad arm is conforming but
 * for the node vocabulary, which is the half the ingest refusal reads: a provider
 * that does not declare it declares nothing the server can refuse against.
 */
function stubDescribeSchema(mode: StubMode): JsonSchemaDoc {
  const document = contractSchema(describeContractJSON());
  if (mode === "bad-describe-schema") {
    document["required"] = ["behavior", "edge_types", "environment"];
    delete (document["properties"] as Record<string, unknown>)["node_types"];
  }
  return document;
}

/** The conforming declaration every mode but the two failure arms returns. */
function stubDeclarationDocument(): Record<string, unknown> {
  return {
    behavior: { summarizable: true, embeddable: false, syncable: true, embed_fields: ["summary"] },
    node_types: ["issue", "epic"],
    edge_types: ["blocks"],
    environment: [{ name: DECLARED_ENV, class: "selector" }],
  };
}

/**
 * Answers the describe tool for one mode. IT BUILDS THE DOCUMENT BY HAND rather
 * than through renderDeclaration, for the same reason the collect half advertises
 * schemas it then breaks: two of these arms must return a declaration the
 * renderer would refuse.
 */
function stubDescribeCall(mode: StubMode): CallToolResult {
  if (mode === "describe-error") {
    return {
      isError: true,
      content: [{ type: "text", text: "the collector cannot describe itself right now" }],
    };
  }
  const document = stubDeclarationDocument();
  if (mode === "bad-declaration") delete document["behavior"];
  return { structuredContent: document };
}

/** Runs the stub in one mode over stdio, and never returns. */
export async function main(): Promise<void> {
  const mode = process.env[STUB_MODE_ENV] ?? "";
  if (mode === "") {
    process.stderr.write(
      `conformance stub: ${STUB_MODE_ENV} is unset — the entry's env block did not reach it. ` +
        `Exiting rather than serving an undeclared mode.\n`,
    );
    process.exit(9);
  }
  if (!isStubMode(mode)) {
    process.stderr.write(
      `conformance stub: ${STUB_MODE_ENV}=${JSON.stringify(mode)} is not one of the stub modes ` +
        `(${STUB_MODES.join(", ")}).\n`,
    );
    process.exit(9);
  }
  if (mode === "exit-before-handshake") {
    process.stderr.write("stub provider: exiting before the handshake, deliberately\n");
    process.exit(3);
  }

  const toolName = stubToolName(mode, process.env[STUB_TOOL_ENV] ?? "");
  const outputSchema = stubOutputSchema(mode);
  const tools = [
    {
      name: toolName,
      description: "stub custom collector",
      inputSchema: stubInputSchema(mode),
      ...(outputSchema === undefined ? {} : { outputSchema }),
    },
  ];
  // THE no-describe MODE SERVES THE COLLECT TOOL ALONE: it is every provider
  // written before the describe tool was required, and the arm that proves the
  // requirement is a requirement.
  if (mode !== "no-describe") {
    tools.push({
      name: DESCRIBE_TOOL_NAME,
      description: "stub declaration",
      inputSchema: describeInputSchema(),
      outputSchema: stubDescribeSchema(mode),
    });
  }
  await serveSpeakerOverStdio({
    serverName: "ful1776-stub",
    serverVersion: "v1",
    tools,
    call: (name, args) => {
      if (name === DESCRIBE_TOOL_NAME) return stubDescribeCall(mode);
      return stubCall(mode, args, process.env, (code) => {
        process.exit(code);
      });
    },
  });
  // NO process.exit HERE, and that is not tidiness. process.exit DISCARDS
  // whatever is still buffered for an asynchronous stdout, and the
  // over-former-cap mode's answer is a single 68 MiB frame that is still
  // draining when the session ends: exiting here truncated it mid-line and the
  // client saw a parse failure rather than a large result. Returning lets Node
  // flush the pipe and exit 0 of its own accord.
}

// RUN ONLY WHEN THIS FILE IS THE ENTRY POINT, so the test file beside it can
// import the payload builders and assert them without spawning anything.
if (isMainModule(import.meta.url)) {
  await main();
}
