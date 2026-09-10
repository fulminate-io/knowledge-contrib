// serve.ts — the ENTRY POINT, and the handler that stands between the wire and a
// collector's walk.
//
// THE HANDLER OWNS THE VALIDATION, and that is not a stylistic choice. On the Go
// side the SDK's generic tool registration validates the call arguments against
// the advertised input schema before the handler runs, so the Go framework's
// handler receives an already-validated value. NOTHING DOES THAT HERE: measured
// against the contract input schema advertised verbatim, the official TypeScript
// SDK's low-level server ran the handler for arguments that did not satisfy the
// contract's own required `id`. So this package validates the arguments itself,
// before the walk, and validates the encoded envelope against the contract output
// schema before it goes out. Both are load-bearing rather than delegated.
//
// STDOUT IS THE PROTOCOL STREAM. A collector that prints to stdout corrupts the
// JSON-RPC framing and surfaces to an operator as an opaque handshake failure;
// every diagnostic a collector writes goes to stderr.

import { realpathSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { Ajv2020 } from "ajv/dist/2020.js";

import { validateResultPayload } from "./contract.js";
import { decodeForeignContext } from "./context.js";
import { DESCRIBE_TOOL_NAME, describeInputSchema, renderDeclaration } from "./describe.js";
import { encodeResult } from "./envelope.js";
import { defaultedToolName, type Collector } from "./framework.js";
import {
  advertisedDescribeSchema,
  advertisedInputSchema,
  advertisedOutputSchema,
  requireObjectParamsSchema,
} from "./schema.js";
import { serveSpeakerOverStdio, type CallToolResult, type SpeakerDefinition } from "./speaker.js";

/**
 * Identifies this framework in the MCP handshake. A consumer logs it, so it names
 * the serving layer rather than the package path.
 */
export const IMPLEMENTATION_VERSION = "v1";

/**
 * Builds the speaker definition one collector serves: the collect tool, carrying
 * the contract's advertised schemas with this collector's params spliced in and
 * bound to a handler that validates, walks, encodes and validates again; and
 * beside it the REQUIRED fixed-name describe tool carrying this collector's
 * declaration.
 *
 * It is exported because a collector with its own transport story (a test, an
 * embedding host) needs the value; a collector's main calls {@link serveStdio}
 * and never sees it.
 *
 * `serverInfo.name` is the RESOLVED TOOL NAME, not a package name: the client
 * logs the two together and an operator matching a session to a config entry
 * reads the tool.
 */
export function newSpeakerDefinition<P>(collector: Collector<P>): SpeakerDefinition {
  const name = defaultedToolName(collector.tool().name);
  const params = requireObjectParamsSchema(name, collector.paramsSchema());
  const inputSchema = advertisedInputSchema(params);
  const outputSchema = advertisedOutputSchema();

  // ONE COMPILED VALIDATOR FOR THE WHOLE PROCESS, over the schema this server
  // ADVERTISED. Validating against anything else would let the tool refuse a
  // call its own listing said was fine, or admit one it said was not.
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  const validateArguments = ajv.compile(inputSchema as object);
  // AND ONE FOR THE DECLARATION, over the schema the describe tool advertises,
  // for the same reason: a declaration this provider's own listing said was fine
  // must not go out unvalidated. It is what turns "the collector author got the
  // shape wrong" into a message here rather than a refusal at the operator's
  // `collector add`.
  const describeSchema = advertisedDescribeSchema();
  const validateDeclaration = ajv.compile(describeSchema as object);

  const description = collector.tool().description;
  return {
    serverName: name,
    serverVersion: IMPLEMENTATION_VERSION,
    tools: [
      {
        name,
        ...(description === undefined ? {} : { description }),
        inputSchema,
        outputSchema,
      },
      {
        name: DESCRIBE_TOOL_NAME,
        description:
          "Describe this collector: its behavior defaults, its per-node-type overrides, the node and edge " +
          "vocabulary it emits, the environment names it reads with each name's class, and the foreign-graph " +
          "context it needs.",
        inputSchema: describeInputSchema(),
        outputSchema: describeSchema,
      },
    ],
    async call(called: string, args: Record<string, unknown>): Promise<CallToolResult> {
      if (called === DESCRIBE_TOOL_NAME) {
        // THE DECLARATION IS RENDERED AND VALIDATED ON EVERY CALL rather than
        // once at build: a collector's describe() is its own code and may read
        // its own configuration, so the answer is the one it gives now.
        let document: Record<string, unknown>;
        try {
          document = renderDeclaration(collector.describe());
        } catch (err) {
          return toolError(`${name} collector: ${messageOf(err)}`);
        }
        if (!validateDeclaration(document)) {
          const detail = (validateDeclaration.errors ?? [])
            .map((e) => `${e.instancePath === "" ? "the declaration" : e.instancePath} ${e.message ?? "is invalid"}`)
            .join("; ");
          return toolError(
            `${name} collector: the rendered declaration does not satisfy the describe contract schema: ${detail}`,
          );
        }
        return { structuredContent: document };
      }
      if (called !== name) {
        return toolError(`${name} collector: this provider serves no tool named "${called}"`);
      }

      // THE ARGUMENTS ARE VALIDATED HERE, BEFORE THE WALK, and this is load
      // bearing rather than delegated: measured, an MCP SDK server ran the
      // handler for arguments that did not satisfy the contract's own required
      // id. A call the advertised schema refuses never reaches the walk.
      if (!validateArguments(args)) {
        const detail = (validateArguments.errors ?? [])
          .map(
            (e) =>
              `${e.instancePath === "" ? "the arguments" : e.instancePath} ${e.message ?? "are invalid"}` +
              (e.params !== undefined && "additionalProperty" in e.params
                ? ` "${String((e.params as { additionalProperty: string }).additionalProperty)}"`
                : "") +
              (e.params !== undefined && "missingProperty" in e.params
                ? ` "${String((e.params as { missingProperty: string }).missingProperty)}"`
                : ""),
          )
          .join("; ");
        return toolError(
          `${name} collector: the call arguments do not satisfy this tool's advertised input schema: ${detail}`,
        );
      }

      const id = args["id"];
      if (typeof id !== "string" || id === "") {
        // The schema requires the id to be PRESENT and a string; it cannot
        // require it to be non-empty. An empty collect id names no graph
        // instance, so it is refused here rather than walked and discarded.
        return toolError(
          `${name} collector: the collect id is empty; it names the graph instance this result lands in`,
        );
      }

      try {
        const foreign = decodeForeignContext(args["context"]);
        const walked = await collector.walk(id, (args["params"] ?? {}) as P, foreign);
        const envelope = encodeResult(name, walked);
        // AND THE RESULT IS VALIDATED AGAINST THE CONTRACT BEFORE IT LEAVES.
        // Nothing downstream on this side does it, and the client validates the
        // payload it receives — so without this the refusal reaches an operator
        // as a message about the collector rather than as one this collector
        // could have given them with the offending property named.
        validateResultPayload(name, envelope);
        return { structuredContent: envelope };
      } catch (err) {
        // A WALK FAILURE IS A TOOL ERROR, never an empty successful result: an
        // empty complete result here would assert a successful walk that found
        // nothing, and the server's deletion phase would act on it.
        return toolError(`${name} collector: the walk failed: ${messageOf(err)}`);
      }
    },
  };
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
 * Serves this collector over MCP on stdin and stdout and resolves when the
 * session ends. It is the entry point for a collector installed as a
 * `type: stdio` config entry, which is the shape a daemon spawns.
 */
export function serveStdio<P>(collector: Collector<P>): Promise<void> {
  return serveSpeakerOverStdio(newSpeakerDefinition(collector));
}

/**
 * Reports whether the module whose `import.meta.url` this is was run as the
 * process's entry point, rather than imported by something else. A collector's
 * script guards its `serveStdio` call with it so a test can import the collector
 * class without spawning a server.
 *
 * IT COMPARES REAL PATHS, and that is the whole reason it exists rather than
 * being written inline in every collector. `import.meta.url` is always the
 * RESOLVED path, while `process.argv[1]` is whatever the caller typed — and an
 * operator's config entry names the script by the path they know. On a machine
 * where /tmp is a symlink to /private/tmp, or where the package sits under any
 * symlinked prefix, the naive `import.meta.url === "file://" + process.argv[1]`
 * comparison is FALSE for a script that really is the entry point: the collector
 * loads, serves nothing, exits 0, and the client reports a handshake that failed
 * with an EOF. Reproduced exactly that way against a real client on this
 * machine's /tmp.
 */
export function isMainModule(importMetaUrl: string): boolean {
  const entry = process.argv[1];
  if (entry === undefined) return false;
  try {
    return realpathSync(fileURLToPath(importMetaUrl)) === realpathSync(entry);
  } catch {
    // A path that cannot be resolved is not the entry point, and saying so is
    // the whole answer: there is nothing to degrade to.
    return false;
  }
}
