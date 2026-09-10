// sample-collector.ts — the WORKED EXAMPLE the README registers and collects, and
// the whole of what a collector author writes.
//
// IT IS CREDENTIAL-FREE ON PURPOSE. It walks a directory the caller names and
// emits one node per file and one per directory, with a `contains` edge from a
// directory to each of its entries. Nothing it emits comes from the environment,
// and the only thing it reads is the path in the collect params.
//
// WHAT IT DEMONSTRATES, in the order an author meets them: a tool spec, a params
// schema, a walk that returns nodes and edges, an INCOMPLETE assertion carrying
// the reason a subtree could not be read, and a thrown error becoming a tool
// error rather than an empty successful result.
//
// EVERY DIAGNOSTIC GOES TO STDERR. stdout is the JSON-RPC protocol stream: one
// console.log anywhere in this file would corrupt the framing and reach an
// operator as an opaque handshake failure.

import { readdirSync, statSync } from "node:fs";
import { join } from "node:path";

import {
  complete,
  incomplete,
  isMainModule,
  serveStdio,
  type Collector,
  type CollectorEdge,
  type CollectorNode,
  type ForeignContext,
  type Declaration,
  type JsonSchema,
  type Result,
} from "../src/index.js";

/**
 * The node and edge types this collector emits, named once and read by both the
 * walk and the declaration. THE DECLARATION IS CLOSED once the family is
 * registered — a collect carrying a type outside it is refused at ingest by name
 * — so a type added to the walk and not to this list is a collect that stops
 * working, which is exactly why the two read one source.
 */
export const NODE_TYPES = ["directory", "file"] as const;
export const EDGE_TYPES = ["contains"] as const;

/** What a collect of this collector takes. */
export interface SampleParams {
  /** The directory to walk. Required: there is no default worth guessing. */
  root?: string;
  /** How deep to walk. Absent means one level. */
  depth?: number;
}

/**
 * A collector over a directory tree.
 *
 * THE COMPLETENESS ASSERTION IS THE PART WORTH COPYING. A subtree this walk could
 * not read makes the whole walk INCOMPLETE, with the reason naming the path,
 * because a walk that swallowed a permission error and asserted Complete() would
 * let the server treat every row this collect did not carry as gone.
 */
export class SampleCollector implements Collector<SampleParams> {
  tool() {
    return {
      name: "collect",
      description: "Walks a directory and emits one node per file and directory.",
    };
  }

  paramsSchema(): JsonSchema {
    return {
      type: "object",
      properties: {
        root: { type: "string", description: "The directory to walk." },
        depth: { type: "integer", minimum: 1, description: "How deep to walk. Absent means one level." },
      },
      required: ["root"],
      additionalProperties: false,
    };
  }

  describe(): Declaration {
    return {
      behavior: {
        // A SUGGESTION AND NOT A SETTING: the operator's flags decide what is
        // paid for, and `collector add` prints these rather than applying them.
        summarizable: false,
        embeddable: false,
        syncable: true,
        bm25Fields: ["symbol_name", "file_path"],
      },
      nodeTypeOverrides: {
        file: { embeddable: false, bm25Fields: ["symbol_name", "file_path"] },
      },
      nodeTypes: [...NODE_TYPES],
      edgeTypes: [...EDGE_TYPES],
      environment: [],
    };
  }

  walk(id: string, params: SampleParams, _foreign: ForeignContext): Result {
    const root = params.root ?? "";
    if (root === "") {
      // A THROWN ERROR BECOMES A TOOL ERROR, never an empty successful result:
      // an empty complete result here would assert a walk that found nothing.
      throw new Error("the `root` param names the directory to walk and was empty");
    }
    const nodes: CollectorNode[] = [];
    const edges: CollectorEdge[] = [];
    const unreadable: string[] = [];
    walkDir(root, params.depth ?? 1, nodes, edges, unreadable);
    process.stderr.write(`sample-collector: collect ${id} walked ${root}, ${nodes.length} nodes\n`);
    return {
      nodes,
      edges,
      complete:
        unreadable.length === 0
          ? complete()
          : incomplete(`could not read ${unreadable.length} path(s): ${unreadable.join(", ")}`),
    };
  }
}

/**
 * Emits one node per entry, recursing while depth allows.
 *
 * A DIRECTORY IT CANNOT READ IS RECORDED, NOT SWALLOWED. The path lands in
 * `unreadable`, which is what turns the walk's assertion to incomplete; the walk
 * carries on so a partial result is still useful.
 */
function walkDir(
  dir: string,
  depth: number,
  nodes: CollectorNode[],
  edges: CollectorEdge[],
  unreadable: string[],
): void {
  nodes.push({ id: dir, type: "directory", filePath: dir });
  let entries: string[];
  try {
    entries = readdirSync(dir).sort();
  } catch (err) {
    unreadable.push(dir);
    process.stderr.write(`sample-collector: cannot read ${dir}: ${String(err)}\n`);
    return;
  }
  for (const entry of entries) {
    const path = join(dir, entry);
    let isDir: boolean;
    let size: number;
    try {
      const info = statSync(path);
      isDir = info.isDirectory();
      size = info.size;
    } catch {
      // A path that disappeared between the listing and the stat is a real
      // outcome of a live filesystem, and it makes this walk incomplete.
      unreadable.push(path);
      continue;
    }
    edges.push({ fromId: dir, toId: path, type: "contains" });
    if (isDir) {
      if (depth > 1) {
        walkDir(path, depth - 1, nodes, edges, unreadable);
      } else {
        nodes.push({ id: path, type: "directory", filePath: path, symbolName: entry });
      }
      continue;
    }
    nodes.push({
      id: path,
      type: "file",
      filePath: path,
      symbolName: entry,
      metadata: { size_bytes: String(size) },
    });
  }
}

if (isMainModule(import.meta.url)) {
  await serveStdio(new SampleCollector());
}
