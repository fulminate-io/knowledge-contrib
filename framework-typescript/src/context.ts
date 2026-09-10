// context.ts — the DECLARED FOREIGN-GRAPH CONTEXT a collect may carry, as the
// TypeScript shape a walk receives it in.
//
// IT IS A COPY OF THE CLIENT'S SHAPE AND THAT IS THE DESIGN, not an oversight.
// The client's own types live in a client-internal package this package cannot
// import, and the contract between the two sides is the checked-in JSON schema
// pair this package holds a copy of, exactly as the node and edge shapes on the
// output side are.
//
// A COLLECTOR NEVER ASKS FOR THIS BLOCK AT RUN TIME. It is DECLARED, once, in the
// operator's config entry — which graph families the collector needs, which node
// types, which fields, which metadata keys — and the client fills it from its own
// graphs before the call. A collector whose entry declares nothing receives an
// empty block, which is what every collector written before this property existed
// sees and the reason adding it broke none of them.
//
// WHAT A COLLECTOR CAN CONCLUDE FROM AN EMPTY BLOCK: nothing about the operator's
// graphs unless its own entry declared the family. The block carries what was
// declared and only what was declared, so an absent field means "not declared",
// never "not present in the graph".
//
// IT IS KEYED BY GRAPH-TYPE NAME, NOT BY A FIXED SET OF FIELDS. The families a
// client can supply are `code` plus whatever graph types the operator has
// REGISTERED, which is not a set any type in this package could enumerate.

/**
 * The one family name this package can name as a constant: the only supplyable
 * family that is not an operator-registered graph type.
 */
export const FAMILY_CODE = "code";

/** One node of a declared slice, carrying the declared fields and no others. */
export interface ForeignNode {
  id?: string;
  type?: string;
  symbolName?: string;
  filePath?: string;
  content?: string;
  metadata?: Record<string, string>;
}

/** One edge of a declared slice. Endpoints are node ids in the same graph. */
export interface ForeignEdge {
  fromId?: string;
  toId?: string;
}

/**
 * One graph's declared slice.
 *
 * `graphName` is always carried, even where the declaration asks for no nodes: a
 * predicate that is a membership test over graph names needs the names and
 * nothing else, and that is a legitimate declaration rather than an empty one.
 */
export interface ForeignGraph {
  graphName: string;
  nodes: ForeignNode[];
  edges: ForeignEdge[];
}

/**
 * The collect input's context block: the foreign-graph slices this collector's
 * registration declared it needs, keyed by graph-type name.
 *
 * A DECLARED FAMILY IS ALWAYS PRESENT, even when the operator's store held no
 * graph of that type: its value is an empty array rather than a missing key. A
 * missing key means the entry never asked, which is a different fact and one a
 * collector is entitled to distinguish — `graphs` returns undefined for it, while
 * a declared-but-empty family returns an empty array.
 */
export class ForeignContext {
  readonly #families: ReadonlyMap<string, ForeignGraph[]>;

  constructor(families?: ReadonlyMap<string, ForeignGraph[]>) {
    this.#families = families ?? new Map();
  }

  /**
   * Reports whether this block carries no family at all, which is what a
   * collector whose entry declares nothing receives.
   */
  isEmpty(): boolean {
    return this.#families.size === 0;
  }

  /**
   * The slice declared under one family name, or undefined when the entry did not
   * declare it.
   *
   * AN UNDEFINED RETURN AND AN EMPTY ARRAY MEAN DIFFERENT THINGS: undefined is
   * "the entry never asked", an empty array is "asked, and the store held no such
   * graph". A caller that need not tell them apart uses {@link graphsOrEmpty}.
   */
  graphs(family: string): ForeignGraph[] | undefined {
    return this.#families.get(family);
  }

  /** {@link graphs} with an undeclared family flattened to an empty array. */
  graphsOrEmpty(family: string): ForeignGraph[] {
    return this.#families.get(family) ?? [];
  }

  /**
   * Every declared family name, sorted.
   *
   * SORTED because a collector that walks families and emits edges from them
   * would otherwise emit them in an insertion order it does not control, which
   * makes one unchanged input produce a different result each run.
   */
  families(): string[] {
    return [...this.#families.keys()].sort();
  }

  /**
   * Every declared family's graphs EXCEPT those named, flattened, in sorted
   * family order.
   *
   * IT EXISTS FOR THE COLLECTORS THAT CORRELATE ACROSS PROVIDERS. A log collector
   * matches its streams against resource metadata and does not care which
   * provider's graph a resource came from — it declares every provider family it
   * might correlate with and wants them as one set. Naming the families to
   * EXCLUDE rather than to include is what keeps that working when an operator
   * registers a provider the collector's author never heard of.
   */
  except(...families: string[]): ForeignGraph[] {
    const out: ForeignGraph[] = [];
    for (const name of this.families()) {
      if (families.includes(name)) continue;
      out.push(...(this.#families.get(name) ?? []));
    }
    return out;
  }
}

/**
 * Decodes the wire form of the context block — an object keyed by graph-type
 * name, each holding an array of `{graph_name, nodes[], edges[]}` in the
 * contract's own snake_case spellings — into a {@link ForeignContext}.
 *
 * AN ABSENT BLOCK IS AN EMPTY CONTEXT, and that is the declared-nothing case
 * rather than a fallback: the property is optional on the contract's input schema
 * and a collector whose entry declares nothing must walk unchanged.
 *
 * EVERY OTHER MALFORMED SHAPE THROWS, naming what it found and where. The
 * advertised input schema refuses a `context` that is not an object before this
 * is reached; what is left is an object whose interior the schema says nothing
 * about, and a family whose value is not an array, or a graph entry that is not
 * an object, is bad input rather than something to walk past silently.
 *
 * A KEY THIS PACKAGE DOES NOT NAME INSIDE A NODE OR AN EDGE IS IGNORED, which is
 * the one deliberate asymmetry. The block is DECLARED per entry and the client
 * fills it from its own graphs, so a field this collector's entry did not declare
 * simply is not sent; refusing an unrecognised one would make every future
 * addition to the client's own slice shape break every collector at once.
 */
export function decodeForeignContext(raw: unknown): ForeignContext {
  if (raw === undefined) return new ForeignContext();
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new Error(
      `the collect context block must be an object keyed by graph-type name; got ${describe(raw)}`,
    );
  }
  const families = new Map<string, ForeignGraph[]>();
  for (const [family, value] of Object.entries(raw as Record<string, unknown>)) {
    if (!Array.isArray(value)) {
      throw new Error(
        `the collect context block's family "${family}" must be an array of {graph_name, nodes, edges}; got ${describe(value)}`,
      );
    }
    families.set(
      family,
      value.map((entry, i) => decodeGraph(family, i, entry)),
    );
  }
  return new ForeignContext(families);
}

/** Decodes one declared graph slice, refusing a shape the contract cannot mean. */
function decodeGraph(family: string, index: number, raw: unknown): ForeignGraph {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new Error(
      `the collect context block's family "${family}"[${index}] must be an object; got ${describe(raw)}`,
    );
  }
  const entry = raw as Record<string, unknown>;
  const graphName = entry["graph_name"];
  if (typeof graphName !== "string") {
    throw new Error(
      `the collect context block's family "${family}"[${index}] declares no graph_name; ` +
        `the name is always carried, even where the declaration asks for no nodes`,
    );
  }
  return {
    graphName,
    nodes: decodeArray(family, index, "nodes", entry["nodes"], decodeNode),
    edges: decodeArray(family, index, "edges", entry["edges"], decodeEdge),
  };
}

function decodeArray<T>(
  family: string,
  index: number,
  key: string,
  raw: unknown,
  decode: (o: Record<string, unknown>) => T,
): T[] {
  // An ABSENT array is an empty one: the declaration may ask for graph names and
  // nothing else, which is a legitimate declaration rather than a malformed
  // block. A PRESENT value of the wrong kind is bad input and errors.
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw)) {
    throw new Error(
      `the collect context block's family "${family}"[${index}].${key} must be an array; got ${describe(raw)}`,
    );
  }
  return raw.map((entry, i) => {
    if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
      throw new Error(
        `the collect context block's family "${family}"[${index}].${key}[${i}] must be an object; got ${describe(entry)}`,
      );
    }
    return decode(entry as Record<string, unknown>);
  });
}

/** The declared subset of a foreign node. A key not named here is ignored. */
function decodeNode(o: Record<string, unknown>): ForeignNode {
  const out: ForeignNode = {};
  copyString(o, "id", out, "id");
  copyString(o, "type", out, "type");
  copyString(o, "symbol_name", out, "symbolName");
  copyString(o, "file_path", out, "filePath");
  copyString(o, "content", out, "content");
  const metadata = o["metadata"];
  if (typeof metadata === "object" && metadata !== null && !Array.isArray(metadata)) {
    const map: Record<string, string> = {};
    for (const [k, v] of Object.entries(metadata as Record<string, unknown>)) {
      if (typeof v === "string") map[k] = v;
    }
    out.metadata = map;
  }
  return out;
}

/** The declared subset of a foreign edge. A key not named here is ignored. */
function decodeEdge(o: Record<string, unknown>): ForeignEdge {
  const out: ForeignEdge = {};
  copyString(o, "from_id", out, "fromId");
  copyString(o, "to_id", out, "toId");
  return out;
}

function copyString<T extends object>(
  from: Record<string, unknown>,
  wireKey: string,
  to: T,
  property: keyof T,
): void {
  const value = from[wireKey];
  if (typeof value === "string") (to as Record<string, unknown>)[property as string] = value;
}

/** Names what a malformed value actually was, so a refusal is readable. */
function describe(value: unknown): string {
  if (value === null) return "null";
  if (Array.isArray(value)) return "an array";
  return typeof value;
}
