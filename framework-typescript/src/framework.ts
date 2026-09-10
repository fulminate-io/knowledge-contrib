// framework.ts — the whole surface a collector author implements, and the
// node and edge model their walk returns.
//
// THE DIVISION OF LABOR IS THE POINT. Nothing about MCP, JSON Schema or the
// envelope is visible to a collector: it implements Collector and calls
// serveStdio. A collector that carries MCP server code or envelope-encoding code
// of its own has re-derived something this package already settled.
//
// THE WIRE SPELLINGS ARE THE CONTRACT AND ARE NEVER IDIOMATIZED. The properties
// below are camelCase because that is TypeScript, and the encoder maps each to
// the contract's own snake_case key — symbol_name, walk_complete, from_id,
// source_graph, target_graph. That mapping is the counterpart of the Go
// framework's json tags, and the checked-in schema files are what both sides
// read.

import type { Completeness } from "./completeness.js";
import type { ForeignContext } from "./context.js";
import type { Declaration } from "./describe.js";

/**
 * Names the single MCP tool a collector serves. The name is the collector's, not
 * a constant: the config-file entry that registers a collector carries a `tool`
 * field naming the one tool the daemon calls, and an operator is free to serve
 * `collect_logs` beside another provider's `collect`.
 */
export interface ToolSpec {
  /** The served tool's name. Empty or absent means {@link DEFAULT_TOOL_NAME}. */
  name?: string;
  /**
   * The tool's human-readable description, shown in an MCP tool listing. Empty is
   * allowed; a collector that means to be installed by a human should write one.
   */
  description?: string;
}

/**
 * The tool name a collector serves when its {@link ToolSpec} names none. It
 * matches the `tool` value in the config-file entry's own worked example.
 */
export const DEFAULT_TOOL_NAME = "collect";

/** A JSON Schema document, as the wire and the checked-in contract files carry it. */
export type JsonSchema = Record<string, unknown>;

/**
 * One node a walk produced. The property set is the TypeScript counterpart of the
 * contract output schema's node shape, which is the artifact a collector author
 * in another language reads.
 *
 * Server-owned bookkeeping fields (created/updated/tombstoned stamps, the collect
 * epoch) are deliberately absent: the collect-write path stamps them, so a
 * collector cannot set them. Anything beyond the typed fields rides in
 * `metadata`, exactly as the built-in collectors do.
 *
 * ABSENT AND PRESENT-AND-EMPTY ARE DIFFERENT INPUTS AND THE ENCODER KEEPS THEM
 * APART. A property left out, or set to `undefined`, is omitted from the wire; a
 * property set to the empty string, to zero, to false or to `{}` is emitted with
 * that value. This is the one place the port deliberately does NOT mirror the Go
 * framework, whose `omitempty` tags collapse the two: TypeScript has no such
 * modifier, and the distinction is one the client's own contract tests assert.
 */
export interface CollectorNode {
  /** Stable identifier for the node within the graph. */
  id: string;
  /** Node type. A node with an empty type fails the collect loud. */
  type: string;
  symbolName?: string;
  filePath?: string;
  language?: string;
  startLine?: number;
  endLine?: number;
  content?: string;
  signature?: string;
  summary?: string;
  description?: string;
  source?: string;
  status?: string;
  keywords?: string;
  isExported?: boolean;
  metadata?: Record<string, string>;
}

/**
 * One edge a walk produced. Endpoints are referenced by node id.
 *
 * AN IN-GRAPH EDGE IS PASSED THROUGH UNTOUCHED, including one naming an endpoint
 * this result does not carry. That is not an oversight and a collector must not
 * "fix" it: the client converts a dangling edge deliberately, and the write path
 * resolves no endpoint.
 *
 * AN EDGE INTO ANOTHER GRAPH IS THE ONE THING THAT IS NOT PASSED THROUGH. Set
 * `targetGraph` and the client resolves the endpoint against that graph family
 * and links the edge into the LINKAGE graph; leave it empty and the edge is an
 * ordinary edge of this collect's own graph. `sourceGraph` is its mirror, for a
 * relationship whose far endpoint is the source. SET AT MOST ONE OF THE TWO: an
 * edge naming both is refused by the collect, because one resolution reaches one
 * foreign family.
 */
export interface CollectorEdge {
  fromId: string;
  toId: string;
  type: string;
  weight?: number;
  confidence?: number;
  method?: string;
  evidence?: string;
  /** The graph FAMILY `fromId` lives in — a graph type, never a graph instance. */
  sourceGraph?: string;
  /** The graph FAMILY `toId` lives in — a graph type, never a graph instance. */
  targetGraph?: string;
}

/**
 * One walk's output. `complete` has no usable zero value: see
 * {@link Completeness}.
 */
export interface Result {
  // `| undefined` is deliberate under exactOptionalPropertyTypes: a walk that
  // found nothing writes `nodes: undefined` as readily as it omits the property,
  // and both must be a legal Result so the encoder is what normalizes them
  // rather than the type system hiding one of the two cells.
  nodes?: CollectorNode[] | undefined;
  edges?: CollectorEdge[] | undefined;
  complete: Completeness;
}

/**
 * The whole surface a collector author implements. P is the collector's params
 * type: the framework advertises {@link Collector.paramsSchema} inside the
 * contract's input schema and hands the walk an ALREADY-VALIDATED value of it — a
 * call whose arguments do not satisfy the advertised schema is refused before
 * `walk` is reached.
 *
 * THE PARAMS SCHEMA IS DECLARED RATHER THAN INFERRED, and that is the one place
 * this surface cannot mirror the Go framework's. Go infers the schema from the
 * params type at run time through reflection; TypeScript erases its types at
 * compile time and has nothing to reflect over, so the author writes the schema.
 * It must be an object schema, for the same reason it must be one in Go: the
 * contract declares `params` as an object and the client's registration gate
 * compares that type.
 */
export interface Collector<P> {
  /** Names the MCP tool this collector serves. */
  tool(): ToolSpec;
  /** The JSON Schema of this collector's params. It must declare type "object". */
  paramsSchema(): JsonSchema;
  /**
   * Returns what this collector IS: its suggested behavior, the node and edge
   * types it emits, the environment variables it reads with their class, and the
   * foreign-graph context it needs. The framework serves it on the required
   * {@link DESCRIBE_TOOL_NAME} tool, `knowledge collector add` writes the
   * registration entry from it, and an installer derives its per-collector tables
   * from it.
   *
   * IT IS A METHOD RATHER THAN A FIELD ON {@link ToolSpec} for the same reason
   * the walk's foreign block is a parameter: a collector must be unable to serve
   * without answering. A field on an object literal is omittable by writing
   * nothing, and a collector that omitted it would advertise a describe tool
   * returning an empty declaration — an empty vocabulary, which refuses every node
   * it then emits, discovered at the operator's first collect rather than at the
   * author's first build.
   *
   * THE TWO LLM AXES ARE A SUGGESTION. Declare what the collector is worth; the
   * operator's flags decide what is paid for.
   */
  describe(): Declaration;
  /**
   * Enumerates the source and returns what it found, together with the
   * completeness assertion for this walk. A thrown error becomes a tool-call
   * error naming the collector and the cause; it never becomes an empty
   * successful result.
   *
   * `id` is the collect id, which names the graph INSTANCE the result lands in.
   * It is NOT the graph family: the family is the registration name, which the
   * client derives on its own side and never sends.
   *
   * `foreign` is the DECLARED FOREIGN-GRAPH CONTEXT: the graph slices this
   * collector's registration entry declared it needs, read out of the operator's
   * own graphs by the client and sent with the call. It is EMPTY for a collector
   * whose entry declares nothing, which is what every collector written before
   * this parameter existed sees.
   */
  walk(id: string, params: P, foreign: ForeignContext): Result | Promise<Result>;
}

/** Resolves a tool spec's name against {@link DEFAULT_TOOL_NAME}. */
export function defaultedToolName(name: string | undefined): string {
  return name === undefined || name === "" ? DEFAULT_TOOL_NAME : name;
}
