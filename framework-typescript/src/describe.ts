// describe.ts — the DESCRIBE TOOL's declaration: what a collector says about
// itself, and the refusals that keep the saying honest.
//
// WHY IT IS A SECOND TOOL RATHER THAN A FIELD ON THE COLLECT RESULT. The client
// must be able to ask a provider what it is BEFORE it has been told what to call:
// `knowledge collector add` dials the provider and fills the registration entry
// from what it finds. That ordering is what makes describe a FIXED-NAME tool —
// the client knows this one name and nothing else about the provider — while the
// collect tool's name stays the operator's, carried in the entry.
//
// THE DECLARATION NAMES NO TOOL, and the absence is the contract's rather than an
// omission here. The collect tool's name is the ToolSpec's, which the listing
// already carries; a second spelling inside the declaration would be a value the
// client would then have to reconcile against the tool it just listed.
//
// THE WIRE SPELLINGS ARE THE CONTRACT'S AND ARE NEVER IDIOMATIZED, on the same
// terms as the node and edge model: the properties below are camelCase because
// that is TypeScript, and the renderer maps each to the contract's own snake_case
// key — node_type_overrides, node_types, edge_types, all_node_types.

import type { JsonSchema } from "./framework.js";

/**
 * The describe tool's FIXED name. It is a constant on both sides of the wire and
 * the two must agree: the client cannot discover a tool it does not already know
 * the name of.
 */
export const DESCRIBE_TOOL_NAME = "describe";

/**
 * The closed environment-name class vocabulary, in the contract file's own order.
 *
 * `not-carried` is the one easiest to mistake for an omission: it is a name the
 * collector READS that an installed entry deliberately does not declare, so
 * declaring it says the absence was a decision rather than an oversight.
 */
export const ENV_CLASSES = ["path", "selector", "secret", "not-carried"] as const;

/** One environment class. */
export type EnvClass = (typeof ENV_CLASSES)[number];

/**
 * The environment-variable name shape an installer accepts, mirroring the Go
 * framework's own pattern.
 *
 * IT IS ENFORCED HERE AND NOT BY THE SCHEMA. The contract file DESCRIBES the
 * shape in prose and carries no `pattern` keyword, so a malformed name validates
 * and is refused later by the installer's own consumption loop, in a run the
 * collector author never sees.
 */
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Thrown when a declaration cannot be rendered. Every message names the part. */
export class DeclarationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DeclarationError";
  }
}

/**
 * The graph-level behavior defaults this collector SUGGESTS.
 *
 * THE TWO LLM AXES ARE A SUGGESTION AND NEVER A SETTING. `summarizable` and
 * `embeddable` are the operator's, taken from their flags; the add path prints
 * what a collector declared and does not apply it. `syncable` and the three field
 * lists cost no LLM call and are written as declared.
 *
 * ALL THREE BOOLEANS ARE REQUIRED. An omitted one is not a default: it is a
 * collector that never said, and the renderer refuses it by name.
 */
export interface BehaviorDeclaration {
  summarizable: boolean;
  embeddable: boolean;
  syncable: boolean;
  embedFields?: string[];
  summarizeFields?: string[];
  bm25Fields?: string[];
}

/** One node type's override of the graph-level behavior. Any subset; an absent key inherits. */
export interface NodeTypeOverride {
  summarizable?: boolean;
  embeddable?: boolean;
  embedFields?: string[];
  summarizeFields?: string[];
  bm25Fields?: string[];
}

/**
 * One environment variable NAME this collector reads, with its class. A VALUE
 * never appears in a declaration.
 */
export interface EnvDeclaration {
  name: string;
  class: EnvClass;
  description?: string;
  /**
   * Declares that this collector tells the name PRESENT AND EMPTY apart from
   * ABSENT: it refuses such a value, branches on the name's presence, or hands it
   * to a dependency that does either.
   *
   * ABSENT MEANS FALSE, and false renders nothing, so a collector that
   * discriminates on nothing produces the document it always produced.
   *
   * WHAT IT GOVERNS IS A DOCUMENT, NOT AN ENTRY. The class decides what an
   * installer writes; this decides whether a worked entry may show `${NAME:-}`
   * for the name. That reference resolves to the empty string in the process
   * serving the collect, so the child receives the name present and empty --
   * inert for an unmarked name and a broken collect for a marked one. It is legal
   * on every class, `not-carried` included: a name no installed entry carries can
   * still appear in an example someone copies.
   */
  emptySensitive?: boolean;
}

/**
 * One foreign graph family this collector needs filled.
 *
 * `nodeTypes` and `allNodeTypes` are alternatives: the selector admits every node
 * of the family whatever its type, and declaring both says two different things
 * with neither silently preferred.
 *
 * IT CARRIES NO FAMILY NAME. The rendered context is an object KEYED BY FAMILY,
 * so the name is the key rather than a member of the value; rendering it inside
 * the value too would be an unknown key the client's config loader refuses.
 */
export interface ForeignFamilyDeclaration {
  nodeTypes?: string[];
  allNodeTypes?: boolean;
  nodeFields?: string[];
  metadataKeys?: string[];
  edgeFields?: string[];
  pathBasenames?: string[];
}

/** The whole declaration, before it is rendered onto the wire. */
export interface Declaration {
  behavior: BehaviorDeclaration;
  nodeTypeOverrides?: Record<string, NodeTypeOverride>;
  nodeTypes: string[];
  edgeTypes: string[];
  environment: EnvDeclaration[];
  context?: Record<string, ForeignFamilyDeclaration>;
}

/** Appends a field list under its wire key when it carries anything. */
function fieldList(target: Record<string, unknown>, key: string, value: string[] | undefined): void {
  if (value !== undefined && value.length > 0) target[key] = [...value];
}

/** Sets a TRI-STATE boolean: undefined leaves the key absent so the cascade inherits it. */
function triState(target: Record<string, unknown>, key: string, value: boolean | undefined): void {
  if (value !== undefined) target[key] = value;
}

function renderOverride(override: NodeTypeOverride): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  triState(out, "summarizable", override.summarizable);
  triState(out, "embeddable", override.embeddable);
  fieldList(out, "embed_fields", override.embedFields);
  fieldList(out, "summarize_fields", override.summarizeFields);
  fieldList(out, "bm25_fields", override.bm25Fields);
  return out;
}

function renderFamily(family: string, declaration: ForeignFamilyDeclaration): Record<string, unknown> {
  const nodeTypes = declaration.nodeTypes ?? [];
  if (declaration.allNodeTypes === true && nodeTypes.length > 0) {
    throw new DeclarationError(
      `the foreign-context declaration for family "${family}" carries all_node_types beside a non-empty ` +
        `node_types list [${nodeTypes.join(", ")}]; the two say different things and neither is silently preferred`,
    );
  }
  const out: Record<string, unknown> = {};
  // AN EMPTY node_types WITH NO SELECTOR KEEPS ITS DELIBERATE MEANING: the graph
  // names alone and no nodes. It is not the selector by another name.
  fieldList(out, "node_types", declaration.nodeTypes);
  if (declaration.allNodeTypes === true) out["all_node_types"] = true;
  fieldList(out, "node_fields", declaration.nodeFields);
  fieldList(out, "metadata_keys", declaration.metadataKeys);
  fieldList(out, "edge_fields", declaration.edgeFields);
  fieldList(out, "path_basenames", declaration.pathBasenames);
  return out;
}

/**
 * Renders a declaration into the document the describe tool returns, refusing by
 * name everything a collector author can get wrong.
 *
 * THE FOUR REQUIRED KEYS ARE ALWAYS EMITTED, empty or not. A collector that emits
 * no edges declares an empty array rather than saying nothing: an absent key
 * would be read as "this collector never said", and a family registered that way
 * keeps accepting anything, which is the widening the closed vocabulary exists to
 * prevent.
 */
export function renderDeclaration(declaration: Declaration): Record<string, unknown> {
  const behavior = declaration.behavior;
  if (behavior === undefined || behavior === null) {
    throw new DeclarationError(
      "the declaration carries no behavior; summarizable, embeddable and syncable are required and an omitted " +
        "one is not a default, it is a collector that never said",
    );
  }
  for (const axis of ["summarizable", "embeddable", "syncable"] as const) {
    if (typeof behavior[axis] !== "boolean") {
      throw new DeclarationError(
        `the declaration's behavior leaves ${axis} unset; the schema requires summarizable, embeddable and ` +
          `syncable explicitly (the two LLM axes are a suggestion the operator's flags override)`,
      );
    }
  }

  for (const [label, types] of [
    ["node_types", declaration.nodeTypes],
    ["edge_types", declaration.edgeTypes],
  ] as const) {
    if (types === undefined) {
      throw new DeclarationError(
        `the declaration names no ${label}; declare every type this collector emits, or an empty array — a collect ` +
          `carrying an undeclared type is refused at ingest by name`,
      );
    }
    const seen = new Set<string>();
    for (const type of types) {
      if (type.trim() === "") {
        throw new DeclarationError(`the declaration's ${label} carries an empty type name`);
      }
      if (seen.has(type)) {
        throw new DeclarationError(`the declaration's ${label} names "${type}" twice`);
      }
      seen.add(type);
    }
  }

  const vocabulary = new Set(declaration.nodeTypes);
  const overrides = declaration.nodeTypeOverrides ?? {};
  for (const nodeType of Object.keys(overrides).sort()) {
    if (!vocabulary.has(nodeType)) {
      throw new DeclarationError(
        `the per-node-type behavior override names the node type "${nodeType}", which is not in this collector's ` +
          `node vocabulary [${[...vocabulary].sort().join(", ")}]; an override for a type the walk never emits can ` +
          `never take effect`,
      );
    }
  }

  if (declaration.environment === undefined) {
    throw new DeclarationError(
      "the declaration names no environment; declare every variable this collector reads with its class, or an empty array",
    );
  }
  const seenNames = new Set<string>();
  for (const entry of declaration.environment) {
    if (!ENV_NAME.test(entry.name)) {
      throw new DeclarationError(
        `the declaration's environment name "${entry.name}" is not an environment variable name; a name matches ` +
          `[A-Za-z_][A-Za-z0-9_]* and an installer refuses anything else`,
      );
    }
    if (!(ENV_CLASSES as readonly string[]).includes(entry.class)) {
      throw new DeclarationError(
        `the declaration's environment name "${entry.name}" carries the class "${entry.class}"; the classes are ` +
          ENV_CLASSES.join(", "),
      );
    }
    if (seenNames.has(entry.name)) {
      throw new DeclarationError(
        `the declaration's environment names "${entry.name}" twice; one name has one class`,
      );
    }
    seenNames.add(entry.name);
  }

  const out: Record<string, unknown> = {
    behavior: (() => {
      const rendered: Record<string, unknown> = {
        summarizable: behavior.summarizable,
        embeddable: behavior.embeddable,
        syncable: behavior.syncable,
      };
      fieldList(rendered, "embed_fields", behavior.embedFields);
      fieldList(rendered, "summarize_fields", behavior.summarizeFields);
      fieldList(rendered, "bm25_fields", behavior.bm25Fields);
      return rendered;
    })(),
    node_types: [...declaration.nodeTypes],
    edge_types: [...declaration.edgeTypes],
    environment: declaration.environment.map((entry) => {
      const rendered: Record<string, unknown> = { name: entry.name, class: entry.class };
      if (entry.description !== undefined && entry.description !== "") {
        rendered["description"] = entry.description;
      }
      // ONLY A TRUE MARK RENDERS. An explicit false says the same thing as an
      // omission, and rendering it would make two collectors that discriminate on
      // nothing produce different bytes.
      if (entry.emptySensitive === true) {
        rendered["empty_sensitive"] = true;
      }
      return rendered;
    }),
  };
  const overrideNames = Object.keys(overrides).sort();
  if (overrideNames.length > 0) {
    const rendered: Record<string, unknown> = {};
    for (const name of overrideNames) {
      const override = overrides[name];
      if (override !== undefined) rendered[name] = renderOverride(override);
    }
    out["node_type_overrides"] = rendered;
  }
  const context = declaration.context ?? {};
  const families = Object.keys(context).sort();
  if (families.length > 0) {
    const rendered: Record<string, unknown> = {};
    for (const family of families) {
      const declared = context[family];
      if (declared !== undefined) rendered[family] = renderFamily(family, declared);
    }
    out["context"] = rendered;
  }
  return out;
}

/**
 * The describe tool's advertised input schema: it takes no arguments.
 *
 * It is written here rather than read from a file because the contract carries no
 * document for it — describe's input is the empty object, and the one shape that
 * says so is this one.
 */
export function describeInputSchema(): JsonSchema {
  return { type: "object", properties: {} };
}
