// speaker.ts — the MCP STDIO SPEAKER, written here rather than taken from an SDK.
//
// THE DECISION AND ITS MEASUREMENT. This package speaks newline-delimited
// JSON-RPC on stdin and stdout itself and takes NO MCP SDK dependency. The SDK
// route was measured and superseded: against the collector contract's input
// schema advertised VERBATIM, the official TypeScript SDK's low-level server ran
// the handler and returned success for arguments `{}` (the contract's own
// required `id` unsatisfied), for a wrong-typed `params` and for a wrong-typed
// `id`, and it passed a result violating the advertised output schema straight
// through. It validates neither side, so its contribution is framing alone — and
// the framing is the four messages below. Its cost is 93 node_modules entries and
// 116 lockfile entries against 8 and 8 here, in a lockfile this repository
// publishes and leak-scans under a policy that fixes every dependency alert by a
// deliberate pull request. Nothing here is on a hot path; the argument is alert
// surface, and disk is incidental. If you are reading this because you need a
// protocol feature, ADD IT HERE — reaching for the SDK restores the whole tree.
//
// STDOUT IS THE PROTOCOL STREAM. Every byte this module writes to stdout is a
// JSON-RPC message followed by a newline. A stray `console.log` anywhere in a
// collector, a dependency or this package corrupts the framing and surfaces to an
// operator as an opaque handshake failure. Diagnostics go to stderr, where
// `console.error` already writes.
//
// THE CONSUMER SURFACE IS FOUR MESSAGES, source-read on the client's dial path:
// initialize, notifications/initialized, tools/list, tools/call. MCP carries
// more (ping, cancellation, progress, logging), and a request naming a method
// this speaker does not implement is answered with a JSON-RPC "method not found"
// rather than crashing the session; a notification naming one is ignored.

import { createInterface } from "node:readline";

import type { JsonSchema } from "./framework.js";

/**
 * The protocol revisions this speaker can speak, newest first. Every one of them
 * carries the tool `outputSchema` declaration and the `structuredContent` result
 * the collector contract depends on; the collector contract's own floor is
 * 2025-06-18, which is why that is what an unrecognised request is answered with.
 */
export const SUPPORTED_PROTOCOL_VERSIONS: readonly string[] = [
  "2026-07-28",
  "2025-11-25",
  "2025-06-18",
];

/**
 * The revision an `initialize` naming an unsupported one is answered with. It is
 * the collector contract's feature floor rather than the newest revision: a
 * caller that asked for something this speaker does not know is told the oldest
 * revision that still carries everything the contract needs.
 */
export const PREFERRED_PROTOCOL_VERSION = "2025-06-18";

/** One tool as `tools/list` carries it. */
export interface ToolDefinition {
  name: string;
  description?: string;
  inputSchema: JsonSchema;
  /** Omitted entirely when absent, which is a shape a conformance stub needs. */
  outputSchema?: JsonSchema;
}

/** One content block of a tool result. Only text is used by this package. */
export interface TextContent {
  type: "text";
  text: string;
}

/** A `tools/call` result, in the shape the wire carries it. */
export interface CallToolResult {
  content?: TextContent[];
  structuredContent?: unknown;
  isError?: boolean;
}

/** What a speaker serves: a tool listing and one handler for every call. */
export interface SpeakerDefinition {
  serverName: string;
  serverVersion: string;
  tools: ToolDefinition[];
  call(name: string, args: Record<string, unknown>): Promise<CallToolResult> | CallToolResult;
}

/**
 * The transport a speaker reads and writes. It exists so the session can be
 * driven in-process by a test with no child process and no pipes, which is what
 * lets the protocol arms be asserted directly rather than inferred from a
 * client's behaviour.
 */
export interface SpeakerIO {
  /** Called once per outbound message, with the message already newline-terminated. */
  write(line: string): void;
}

/**
 * One live MCP session over a line-oriented transport. Feed it inbound lines with
 * {@link Session.handleLine}; it writes every response through the
 * {@link SpeakerIO} it was built with.
 */
export class Session {
  #negotiated: string | undefined;

  constructor(
    private readonly def: SpeakerDefinition,
    private readonly io: SpeakerIO,
  ) {}

  /**
   * Handles one inbound line. A line that is not JSON, and a JSON value that is
   * not a JSON-RPC message, are answered with an error or ignored; neither ends
   * the session. Resolves once any response has been written.
   */
  async handleLine(line: string): Promise<void> {
    if (line.trim() === "") return;

    let message: unknown;
    try {
      message = JSON.parse(line);
    } catch {
      // A LINE THAT DID NOT PARSE HAS NO ID TO ANSWER, so the response carries
      // the null id the JSON-RPC spec reserves for exactly this. It is answered
      // rather than ignored because a caller waiting on a request it mangled
      // would otherwise wait forever.
      this.#send({ jsonrpc: "2.0", id: null, error: { code: -32700, message: "parse error" } });
      return;
    }
    if (typeof message !== "object" || message === null || Array.isArray(message)) {
      // A scalar, an array or null carries no id, so there is nothing to answer.
      return;
    }
    const request = message as Record<string, unknown>;
    const id = request["id"];
    const method = request["method"];
    const hasId = id !== undefined && id !== null;

    if (typeof method !== "string") {
      if (hasId) {
        this.#send({
          jsonrpc: "2.0",
          id: id as string | number,
          error: { code: -32600, message: "invalid request: no method" },
        });
      }
      return;
    }

    const params = request["params"];
    switch (method) {
      case "initialize": {
        const requested =
          typeof params === "object" && params !== null
            ? (params as Record<string, unknown>)["protocolVersion"]
            : undefined;
        this.#negotiated = negotiateProtocolVersion(requested);
        if (!hasId) return;
        this.#send({
          jsonrpc: "2.0",
          id: id as string | number,
          result: {
            protocolVersion: this.#negotiated,
            capabilities: { tools: {} },
            serverInfo: { name: this.def.serverName, version: this.def.serverVersion },
          },
        });
        return;
      }
      case "notifications/initialized":
        return;
      case "tools/list": {
        if (!hasId) return;
        this.#send({
          jsonrpc: "2.0",
          id: id as string | number,
          result: { tools: this.def.tools.map(toolWire) },
        });
        return;
      }
      case "tools/call": {
        if (!hasId) return;
        const call = typeof params === "object" && params !== null ? (params as Record<string, unknown>) : {};
        const name = typeof call["name"] === "string" ? (call["name"] as string) : "";
        const args =
          typeof call["arguments"] === "object" &&
          call["arguments"] !== null &&
          !Array.isArray(call["arguments"])
            ? (call["arguments"] as Record<string, unknown>)
            : {};
        const result = await this.#invoke(name, args);
        this.#send({ jsonrpc: "2.0", id: id as string | number, result: resultWire(result) });
        return;
      }
      default: {
        // A NOTIFICATION NAMING AN UNKNOWN METHOD IS IGNORED; a REQUEST is
        // answered. MCP carries more methods than the four above, and neither
        // crashing nor hanging is an answer.
        if (!hasId) return;
        this.#send({
          jsonrpc: "2.0",
          id: id as string | number,
          error: { code: -32601, message: `method not found: ${method}` },
        });
      }
    }
  }

  /** The negotiated protocol version, or undefined before `initialize`. */
  negotiatedVersion(): string | undefined {
    return this.#negotiated;
  }

  /**
   * Runs one tool call, turning both refusals a call can meet into a RESULT with
   * isError rather than a JSON-RPC error: the client reads isError as a refused
   * collect that writes nothing, and a protocol-level error is a statement about
   * the request rather than about the walk.
   */
  async #invoke(name: string, args: Record<string, unknown>): Promise<CallToolResult> {
    if (!this.def.tools.some((t) => t.name === name)) {
      // A CALL NAMING A TOOL THIS SPEAKER DOES NOT LIST is refused here, so no
      // handler can be reached under a name it never advertised. The client
      // selects by name from the listing, so a stale entry hits this arm.
      return toolError(
        `this provider serves no tool named "${name}"; it serves ` +
          this.def.tools.map((t) => `"${t.name}"`).join(", "),
      );
    }
    try {
      return await this.def.call(name, args);
    } catch (err) {
      return toolError(messageOf(err));
    }
  }

  #send(message: unknown): void {
    // ONE MESSAGE, ONE LINE. JSON.stringify emits no interior newline for any
    // value, so the framing is the terminator alone.
    this.io.write(JSON.stringify(message) + "\n");
  }
}

/** One tool as the wire carries it, with an absent output schema OMITTED. */
function toolWire(tool: ToolDefinition): Record<string, unknown> {
  const out: Record<string, unknown> = { name: tool.name, inputSchema: tool.inputSchema };
  if (tool.description !== undefined) out["description"] = tool.description;
  // AN ABSENT SCHEMA IS AN ABSENT KEY, never a null one: the client's gate reads
  // "no output schema" from the key's absence, and `null` and a missing key are
  // the same fact to it only by luck.
  if (tool.outputSchema !== undefined) out["outputSchema"] = tool.outputSchema;
  return out;
}

/** One call result as the wire carries it, with every absent field omitted. */
function resultWire(result: CallToolResult): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (result.content !== undefined) out["content"] = result.content;
  if (result.structuredContent !== undefined) out["structuredContent"] = result.structuredContent;
  if (result.isError !== undefined) out["isError"] = result.isError;
  return out;
}

/** A tool result carrying a failure as text, with no structured content at all. */
function toolError(text: string): CallToolResult {
  return { isError: true, content: [{ type: "text", text }] };
}

/** The message of a thrown value, whatever kind of value it was. */
function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Chooses the protocol version an `initialize` naming `requested` is answered
 * with: the requested revision when this speaker supports it, and
 * {@link PREFERRED_PROTOCOL_VERSION} otherwise. It never answers a revision this
 * speaker cannot speak.
 */
export function negotiateProtocolVersion(requested: unknown): string {
  if (typeof requested === "string" && SUPPORTED_PROTOCOL_VERSIONS.includes(requested)) {
    return requested;
  }
  return PREFERRED_PROTOCOL_VERSION;
}

/**
 * Serves one speaker definition over stdin and stdout until stdin closes. This is
 * the entry point a collector binary reaches through `serveStdio`, and the one a
 * conformance stub reaches directly.
 */
export function serveSpeakerOverStdio(def: SpeakerDefinition): Promise<void> {
  const session = new Session(def, {
    write(line: string) {
      process.stdout.write(line);
    },
  });
  const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });
  // LINES ARE HANDLED IN ORDER even though the handler is async: readline emits
  // synchronously, so without this chain a slow call and a fast one would answer
  // out of order and a caller matching responses by arrival would be wrong.
  let queue: Promise<void> = Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    rl.on("line", (line: string) => {
      queue = queue.then(() => session.handleLine(line)).catch(reject);
    });
    rl.on("close", () => {
      queue.then(resolve).catch(reject);
    });
  });
}
