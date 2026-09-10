// envelope.test.ts — R1.4, R1.5, R1.6 and R1.7: the three wire-shaping rules and
// the encoder's three refusals.

import assert from "node:assert/strict";
import { test } from "node:test";

import { complete, incomplete, type Completeness } from "../src/completeness.js";
import { encodeResult } from "../src/envelope.js";
import type { CollectorEdge, CollectorNode, Result } from "../src/framework.js";

const node = (over: Partial<CollectorNode> = {}): CollectorNode => ({
  id: "n1",
  type: "issue",
  ...over,
});

test("R1.4 every list is an ARRAY on the wire, in every cell of {nodes,edges} x {absent,undefined,null,empty,one}", () => {
  const cells: Array<{ name: string; result: Result }> = [
    { name: "both absent", result: { complete: complete() } },
    { name: "both undefined", result: { nodes: undefined, edges: undefined, complete: complete() } },
    {
      name: "both null (the shape nothing upstream catches)",
      result: {
        nodes: null as unknown as CollectorNode[],
        edges: null as unknown as CollectorEdge[],
        complete: complete(),
      },
    },
    { name: "both empty", result: { nodes: [], edges: [], complete: complete() } },
    {
      name: "one element each",
      result: {
        nodes: [node()],
        edges: [{ fromId: "n1", toId: "n2", type: "blocks" }],
        complete: complete(),
      },
    },
  ];

  for (const cell of cells) {
    const out = encodeResult("collect", cell.result);
    assert.ok(Array.isArray(out.nodes), `${cell.name}: nodes must be an array`);
    assert.ok(Array.isArray(out.edges), `${cell.name}: edges must be an array`);

    // THE WIRE, not the object: a key present in the object and dropped by
    // JSON.stringify (undefined) reads identically to a correct one here.
    const wire = JSON.parse(JSON.stringify(out)) as Record<string, unknown>;
    assert.ok(Array.isArray(wire["nodes"]), `${cell.name}: nodes must reach the wire as an array`);
    assert.ok(Array.isArray(wire["edges"]), `${cell.name}: edges must reach the wire as an array`);
    assert.notEqual(wire["nodes"], null, `${cell.name}: nodes must never be null`);
    assert.notEqual(wire["edges"], null, `${cell.name}: edges must never be null`);
  }
});

test("R1.5 walk_complete is always present, on BOTH values", () => {
  const yes = encodeResult("collect", { complete: complete() });
  assert.equal(yes.walk_complete, true);
  assert.ok("walk_complete" in JSON.parse(JSON.stringify(yes)));

  // The INCOMPLETE arm is the one that catches an omit-when-false serializer; a
  // complete walk emits `true` and the defect is invisible.
  const no = encodeResult("collect", { complete: incomplete("throttled") });
  assert.equal(no.walk_complete, false);
  const wire = JSON.parse(JSON.stringify(no)) as Record<string, unknown>;
  assert.ok("walk_complete" in wire, "an incomplete walk must still emit the key");
  assert.equal(wire["walk_complete"], false);
});

test("R1.6 an unasserted completeness value is refused at encode time, naming the collector", () => {
  for (const forged of [undefined, null, true, false, {}]) {
    assert.throws(
      () => encodeResult("collect_graph", { complete: forged as unknown as Completeness }),
      (err: Error) => {
        assert.match(err.message, /collect_graph/, "the refusal must name the collector");
        assert.match(err.message, /asserts nothing/);
        return true;
      },
      `a result whose completeness is ${String(forged)} must be refused`,
    );
  }
});

test("R1.6 an incomplete walk with an empty reason is refused, naming the collector", () => {
  assert.throws(
    () => encodeResult("collect_graph", { complete: incomplete("") }),
    (err: Error) => {
      assert.match(err.message, /collect_graph/);
      assert.match(err.message, /INCOMPLETE walk with no reason/);
      return true;
    },
  );
});

test("R1.6 the reason never reaches the wire", () => {
  const out = encodeResult("collect", { complete: incomplete("a secret-shaped reason") });
  const wire = JSON.stringify(out);
  assert.equal(wire.includes("a secret-shaped reason"), false);
  assert.equal(wire.includes("reason"), false);
});

test("R1.7 a node with an empty type is refused, naming the collector and the index", () => {
  assert.throws(
    () =>
      encodeResult("collect_graph", {
        nodes: [node(), node({ id: "n2", type: "" })],
        complete: complete(),
      }),
    (err: Error) => {
      assert.match(err.message, /collect_graph/);
      assert.match(err.message, /node\[1\]/, "the refusal must name the index");
      assert.match(err.message, /n2/, "and the node id");
      assert.match(err.message, /empty type/);
      return true;
    },
  );
});

test("R2a.4 absent and present-and-empty are different on the wire and never collapse", () => {
  const out = encodeResult("collect", {
    nodes: [
      node({ id: "ISSUE-2" }),
      node({
        id: "ISSUE-3",
        symbolName: "",
        filePath: "",
        language: "",
        content: "",
        signature: "",
        summary: "",
        description: "",
        source: "",
        status: "",
        keywords: "",
        startLine: 0,
        endLine: 0,
        isExported: false,
        metadata: {},
      }),
    ],
    complete: complete(),
  });
  const wire = JSON.parse(JSON.stringify(out)) as { nodes: Record<string, unknown>[] };
  const absent = wire.nodes[0]!;
  const empty = wire.nodes[1]!;

  assert.deepEqual(Object.keys(absent).sort(), ["id", "type"], "an omitted optional must not reach the wire");
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
    assert.equal(empty[key], "", `${key} set to the empty string must arrive present and empty`);
  }
  assert.equal(empty["start_line"], 0);
  assert.equal(empty["end_line"], 0);
  assert.equal(empty["is_exported"], false);
  assert.deepEqual(empty["metadata"], {}, "a metadata map sent as {} must arrive PRESENT, not dropped");
});

test("R1.4 every optional node and edge property is emitted under its contract key", () => {
  const out = encodeResult("collect", {
    nodes: [
      node({
        symbolName: "Login broken",
        filePath: "src/login.go",
        language: "go",
        startLine: 10,
        endLine: 20,
        content: "body",
        signature: "sig",
        summary: "one line",
        description: "longer",
        source: "stub",
        status: "open",
        keywords: "login auth",
        isExported: true,
        metadata: { priority: "high" },
      }),
    ],
    edges: [
      {
        fromId: "a",
        toId: "b",
        type: "blocks",
        weight: 0.5,
        confidence: 0.25,
        method: "derived",
        evidence: "src/login.go:10",
        targetGraph: "code",
      },
    ],
    complete: complete(),
  });
  const wire = JSON.parse(JSON.stringify(out)) as {
    nodes: Record<string, unknown>[];
    edges: Record<string, unknown>[];
  };
  assert.deepEqual(wire.nodes[0], {
    id: "n1",
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
  });
  assert.deepEqual(wire.edges[0], {
    from_id: "a",
    to_id: "b",
    type: "blocks",
    weight: 0.5,
    confidence: 0.25,
    method: "derived",
    evidence: "src/login.go:10",
    target_graph: "code",
  });
});

test("R1.4 an edge naming only source_graph emits it and omits target_graph", () => {
  const out = encodeResult("collect", {
    edges: [{ fromId: "a", toId: "b", type: "deploys", sourceGraph: "code" }],
    complete: complete(),
  });
  const wire = JSON.parse(JSON.stringify(out)) as { edges: Record<string, unknown>[] };
  assert.equal(wire.edges[0]!["source_graph"], "code");
  assert.equal("target_graph" in wire.edges[0]!, false);
});

test("R1.4 the encoded OBJECT carries no undefined-valued key, not just the serialized form", () => {
  // JSON.stringify drops an undefined-valued key, so the wire looks identical
  // either way — and the contract validation runs on the OBJECT, before anything
  // is serialized. A key present with the value undefined is a property the
  // validator sees as declared and wrongly typed, so the omission has to happen
  // in the encoder rather than in the serializer.
  const out = encodeResult("collect", {
    nodes: [node()],
    edges: [{ fromId: "a", toId: "b", type: "blocks" }],
    complete: complete(),
  });
  for (const row of [...out.nodes, ...out.edges]) {
    for (const [key, value] of Object.entries(row)) {
      assert.notEqual(value, undefined, `the encoded row carries ${key} with the value undefined`);
      assert.ok(Object.hasOwn(row, key));
    }
  }
  assert.deepEqual(Object.keys(out.nodes[0]!), ["id", "type"]);
  assert.deepEqual(Object.keys(out.edges[0]!), ["from_id", "to_id", "type"]);
});

test("R1.4 a property this package does not name never reaches the wire", () => {
  const out = encodeResult("collect", {
    nodes: [{ ...node(), summry: "typo'd key" } as unknown as CollectorNode],
    complete: complete(),
  });
  const wire = JSON.parse(JSON.stringify(out)) as { nodes: Record<string, unknown>[] };
  assert.equal(
    "summry" in wire.nodes[0]!,
    false,
    "the encoder emits the contract vocabulary only, so a collector's stray property cannot reach the client's strict decode",
  );
});
