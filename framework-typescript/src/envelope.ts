// envelope.ts — the CONTRACT ENVELOPE the served tool returns, and the three
// refusals plus the one normalization that stand between a walk's Result and it.
//
// NO KEY IS EVER OMITTED FROM THE THREE TOP-LEVEL FIELDS, and walk_complete is
// the one that matters. The contract output schema REQUIRES walk_complete, and
// this package validates the envelope against that schema before the result
// leaves the provider; a walk asserting INCOMPLETE that emitted no walk_complete
// at all would fail the whole collect with a missing-property error. A walk
// asserting COMPLETE emits `true` and the defect is invisible, which is why the
// test that pins this rides the incomplete arm.
//
// EVERY LIST IS AN ARRAY, NEVER null AND NEVER AN OMITTED KEY. A walk that found
// nothing leaves both properties undefined; `undefined` disappears from
// JSON.stringify and `null` marshals as null; the contract output schema declares
// both as "array" with no null union. An empty region, an empty log group or a
// filter that matched nothing is the first real run of a new collector, so this is
// the cell it hits. Nothing upstream catches it: measured, a result carrying
// `edges: null` went out untouched on every route tried.

import { isCompleteness } from "./completeness.js";
import type { CollectorEdge, CollectorNode, Result } from "./framework.js";

/** The served tool's output value: the contract envelope, exactly. */
export interface CollectOutput {
  nodes: Record<string, unknown>[];
  edges: Record<string, unknown>[];
  walk_complete: boolean;
}

/**
 * The wire key each node property is emitted under. The map IS the contract's
 * node vocabulary; a property this package does not name never reaches the wire,
 * and a key the contract does not declare is never emitted.
 */
export const NODE_WIRE_KEYS: ReadonlyMap<keyof CollectorNode, string> = new Map([
  ["id", "id"],
  ["type", "type"],
  ["symbolName", "symbol_name"],
  ["filePath", "file_path"],
  ["language", "language"],
  ["startLine", "start_line"],
  ["endLine", "end_line"],
  ["content", "content"],
  ["signature", "signature"],
  ["summary", "summary"],
  ["description", "description"],
  ["source", "source"],
  ["status", "status"],
  ["keywords", "keywords"],
  ["isExported", "is_exported"],
  ["metadata", "metadata"],
]);

/** The wire key each edge property is emitted under. */
export const EDGE_WIRE_KEYS: ReadonlyMap<keyof CollectorEdge, string> = new Map([
  ["fromId", "from_id"],
  ["toId", "to_id"],
  ["type", "type"],
  ["weight", "weight"],
  ["confidence", "confidence"],
  ["method", "method"],
  ["evidence", "evidence"],
  ["sourceGraph", "source_graph"],
  ["targetGraph", "target_graph"],
]);

/**
 * Turns one walk's Result into the envelope, refusing the three shapes that are
 * bad input and normalizing the one that is a language artifact.
 *
 * `collector` names the served tool, which is the only identity this package has
 * for the collector, so a refusal reaching an operator names something they can
 * find in their config entry.
 */
export function encodeResult(collector: string, result: Result): CollectOutput {
  const assertion = result.complete;
  if (!isCompleteness(assertion)) {
    throw new Error(
      `framework: collector "${collector}" returned a completeness value that asserts nothing; ` +
        `a walk returns complete() or incomplete(reason)`,
    );
  }
  if (!assertion.isComplete() && assertion.reason() === "") {
    throw new Error(
      `framework: collector "${collector}" asserted an INCOMPLETE walk with no reason; ` +
        `incomplete() takes the reason the walk did not finish`,
    );
  }

  const nodes: Record<string, unknown>[] = [];
  const inputNodes = result.nodes ?? [];
  for (let i = 0; i < inputNodes.length; i++) {
    const node = inputNodes[i]!;
    if (node.type === "") {
      throw new Error(
        `framework: collector "${collector}": node[${i}] (id="${node.id}") has an empty type`,
      );
    }
    nodes.push(project(node, NODE_WIRE_KEYS));
  }

  const edges: Record<string, unknown>[] = [];
  for (const edge of result.edges ?? []) {
    edges.push(project(edge, EDGE_WIRE_KEYS));
  }

  return { nodes, edges, walk_complete: assertion.isComplete() };
}

/**
 * Emits the properties the wire map names, under their contract keys, in the
 * map's own order.
 *
 * `undefined` IS THE ONLY THING DROPPED. A property set to the empty string, to
 * zero, to false or to `{}` is emitted with that value, so a node carrying its
 * optionals PRESENT AND EMPTY stays distinguishable from one that omits them —
 * which is a distinction the client's own contract tests assert. A property the
 * map does not name never reaches the wire at all, so a collector's stray key
 * cannot reach the client's strict decode and fail a collect there.
 */
function project<T extends object>(
  value: T,
  wireKeys: ReadonlyMap<keyof T, string>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [property, wireKey] of wireKeys) {
    const v = value[property];
    if (v !== undefined) out[wireKey] = v;
  }
  return out;
}
