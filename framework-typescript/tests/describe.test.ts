// describe.test.ts — R1.13: THE REQUIRED DESCRIBE TOOL, its renderer's refusals
// one row at a time, and one call driven over the real stdio transport.
//
// WHY THIS FILE EXISTS SEPARATELY FROM THE R1.13 ROW IN package.test.ts. That row
// asserts the tool is listed, that its advertised output schema is the contract
// file, and that its result validates. Those are properties of the OUTPUT, and a
// renderer is not observed by its output alone: every one of the eleven refusals
// below was neutralized in turn, one at a time, and the suite stayed green at 147
// passing — the schema admits what the renderer refuses, because the renderer
// refuses things a schema cannot express (a duplicate type name, an override for
// a type outside the vocabulary, an environment name shaped so the installer
// would decline it) and because a rendered document that skips a refusal is still
// a valid document. Each refusal therefore gets its own row, and each row asserts
// the message NAMES the offending value, because a refusal a reader cannot act on
// costs an author the same round trip as no refusal at all.
//
// EVERY REFUSAL ROW CARRIES A CONTROL, and the controls are not decoration: a row
// asserting only that a bad declaration throws passes just as well against a
// renderer that throws on everything. The near miss beside each — the same
// declaration one field away from the refusal — is what makes the row about the
// rule rather than about the throwing.
//
// THE FIXTURES ARE BUILT THROUGH A CAST, and the reason is worth stating. The
// Declaration type makes most of these unrepresentable, which is exactly what it
// is for: an author writing TypeScript against this package meets the compiler
// before the renderer. But a declaration crosses a language boundary as JSON on
// the way in from a port and comes out of code the compiler never saw, so the
// run-time refusals are the ones that hold for every author. The cast is how a
// test written IN TypeScript reaches the shape a JavaScript caller can hand it.

import assert from "node:assert/strict";
import { test } from "node:test";

import { Ajv2020 } from "ajv/dist/2020.js";

import { SampleCollector } from "../examples/sample-collector.js";
import {
  DESCRIBE_TOOL_NAME,
  DeclarationError,
  ENV_CLASSES,
  renderDeclaration,
  type Declaration,
} from "../src/describe.js";
import { describeContractJSON } from "../src/schema.js";
import { newSpeakerDefinition } from "../src/serve.js";
import { compiled, driveSession, initializeRequest, rpc, runChild } from "./helpers.js";

type Msg = Record<string, any>;

/** A declaration that renders, which every case below starts from. */
function base(): Declaration {
  return {
    behavior: { summarizable: true, embeddable: false, syncable: true, embedFields: ["summary"] },
    nodeTypeOverrides: { issue: { embeddable: false, bm25Fields: ["summary"] } },
    nodeTypes: ["issue", "epic"],
    edgeTypes: ["blocks"],
    environment: [
      { name: "ACME_TOKEN", class: "secret", description: "the API token" },
      { name: "ACME_ROOT", class: "path" },
      { name: "ACME_PROJECT", class: "selector" },
      { name: "ACME_PROXY", class: "not-carried" },
    ],
    context: {
      code: {
        nodeTypes: ["function"],
        nodeFields: ["id", "file_path"],
        metadataKeys: ["package"],
        edgeFields: ["from_id", "to_id"],
      },
    },
  };
}

/**
 * Applies one mutation to the base declaration through a cast.
 *
 * `mutate` receives the declaration as a plain object, which is what lets a case
 * delete a required property or write a value the type forbids — the two shapes a
 * caller in another language actually produces.
 */
function malformed(mutate: (d: Record<string, any>) => void): Declaration {
  const d = base() as unknown as Record<string, any>;
  mutate(d);
  return d as unknown as Declaration;
}

/**
 * Asserts that rendering this declaration is refused, and that the message names
 * every value a reader needs in order to fix it.
 */
function refused(declaration: Declaration, ...named: string[]): string {
  let message = "";
  assert.throws(
    () => renderDeclaration(declaration),
    (err: unknown) => {
      assert.ok(err instanceof DeclarationError, `the refusal is a DeclarationError, got ${String(err)}`);
      message = err.message;
      return true;
    },
  );
  for (const value of named) {
    assert.ok(
      message.includes(value),
      `the refusal does not name ${JSON.stringify(value)}: ${JSON.stringify(message)}. A refusal a reader ` +
        `cannot act on costs an author the same round trip as no refusal at all.`,
    );
  }
  return message;
}

/** The control every case carries: the near miss renders. */
function renders(declaration: Declaration): Record<string, unknown> {
  return renderDeclaration(declaration);
}

// ---------------------------------------------------------------------------
// THE CONTROL FOR THE CONTROLS. Without it a base() that had itself gone
// unrenderable would make every "the near miss renders" assertion below fail for
// a reason none of them is about, and every refusal row pass for the wrong one.
// ---------------------------------------------------------------------------

test("R1.13 the fixture this file mutates renders, and renders every part", () => {
  const document = renders(base());
  for (const key of ["behavior", "node_types", "edge_types", "environment", "node_type_overrides", "context"]) {
    assert.ok(key in document, `the fixture does not populate ${key}, so a case about it would assert nothing`);
  }
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  const validate = ajv.compile(JSON.parse(describeContractJSON()) as object);
  assert.ok(validate(document), JSON.stringify(validate.errors));
});

// ---------------------------------------------------------------------------
// THE FOREIGN-CONTEXT REFUSAL.
// ---------------------------------------------------------------------------

test("R1.13 a family selector beside a non-empty node-type list is refused, naming the family", () => {
  refused(
    malformed((d) => {
      d["context"]["code"]["allNodeTypes"] = true;
    }),
    "code",
    "all_node_types",
    "function",
  );

  // THE CONTROL, and it carries the second half of the rule: the selector ALONE
  // renders, and an empty list with no selector keeps its own deliberate meaning
  // — the graph names alone and no nodes, which is not the selector by another
  // name.
  const selectorOnly = renders(
    malformed((d) => {
      d["context"]["code"]["allNodeTypes"] = true;
      d["context"]["code"]["nodeTypes"] = [];
    }),
  );
  const family = (selectorOnly["context"] as Msg)["code"] as Msg;
  assert.equal(family["all_node_types"], true);
  assert.ok(!("node_types" in family));

  const namesOnly = renders(
    malformed((d) => {
      d["context"]["code"]["nodeTypes"] = [];
    }),
  );
  const bare = (namesOnly["context"] as Msg)["code"] as Msg;
  assert.ok(!("all_node_types" in bare));
  assert.ok(!("node_types" in bare));
});

// ---------------------------------------------------------------------------
// THE BEHAVIOR REFUSALS.
// ---------------------------------------------------------------------------

test("R1.13 a declaration carrying no behavior at all is refused", () => {
  refused(
    malformed((d) => {
      delete d["behavior"];
    }),
    "behavior",
  );
  // THE CONTROL: the block present with its three axes renders.
  assert.ok("behavior" in renders(base()));
});

for (const axis of ["summarizable", "embeddable", "syncable"] as const) {
  test(`R1.13 a behavior leaving ${axis} unset is refused, naming that axis`, () => {
    // ONE ROW PER AXIS AND NOT ONE FOR THE LOOP. A single row over one axis
    // passes against a renderer that checks only that axis, which is the shape a
    // hand-written check drifts into; three rows say the loop covers three.
    refused(
      malformed((d) => {
        delete d["behavior"][axis];
      }),
      axis,
    );
    // THE CONTROL: the other two unset are not what this row reported.
    const message = refused(
      malformed((d) => {
        delete d["behavior"][axis];
      }),
      axis,
    );
    for (const other of ["summarizable", "embeddable", "syncable"] as const) {
      if (other === axis) continue;
      assert.ok(
        !message.includes(`leaves ${other} unset`),
        `the refusal names ${other} for a declaration that sets it: ${message}`,
      );
    }
  });
}

// ---------------------------------------------------------------------------
// THE VOCABULARY REFUSALS. Each runs over BOTH vocabularies, because the loop
// that carries them takes the label as a variable and a row over one of the two
// passes against a renderer that checks only that one.
// ---------------------------------------------------------------------------

for (const [property, label] of [
  ["nodeTypes", "node_types"],
  ["edgeTypes", "edge_types"],
] as const) {
  test(`R1.13 a declaration naming no ${label} is refused, naming the property`, () => {
    refused(
      malformed((d) => {
        delete d[property];
      }),
      label,
    );
    // THE CONTROL: an EMPTY array is a declaration and renders. It is the
    // difference the whole closed vocabulary rests on — a collector that emits no
    // edges says so, and a collector that says nothing is one the server keeps
    // accepting anything from.
    const document = renders(
      malformed((d) => {
        d[property] = [];
        if (property === "nodeTypes") delete d["nodeTypeOverrides"];
      }),
    );
    assert.deepEqual(document[label], []);
  });

  test(`R1.13 an empty type name in ${label} is refused`, () => {
    refused(
      malformed((d) => {
        d[property] = [...d[property], "  "];
      }),
      label,
      "empty type name",
    );
  });

  test(`R1.13 a duplicate in ${label} is refused, naming the repeated type`, () => {
    const repeated = base()[property === "nodeTypes" ? "nodeTypes" : "edgeTypes"][0]!;
    refused(
      malformed((d) => {
        d[property] = [...d[property], repeated];
      }),
      label,
      repeated,
      "twice",
    );
  });
}

test("R1.13 a per-node-type override outside the vocabulary is refused, naming the type and the vocabulary", () => {
  refused(
    malformed((d) => {
      d["nodeTypeOverrides"]["ticket"] = { embeddable: true };
    }),
    "ticket",
    "node vocabulary",
    "issue",
  );
  // THE CONTROL: the same override for a type that IS in the vocabulary renders.
  const document = renders(
    malformed((d) => {
      d["nodeTypes"] = [...d["nodeTypes"], "ticket"];
      d["nodeTypeOverrides"]["ticket"] = { embeddable: true };
    }),
  );
  assert.ok("ticket" in (document["node_type_overrides"] as Msg));
});

// ---------------------------------------------------------------------------
// THE ENVIRONMENT REFUSALS.
// ---------------------------------------------------------------------------

test("R1.13 a declaration naming no environment is refused", () => {
  refused(
    malformed((d) => {
      delete d["environment"];
    }),
    "environment",
  );
  // THE CONTROL: an empty array renders, for the same reason an empty vocabulary
  // does — a collector that reads no variable says so.
  assert.deepEqual(
    renders(
      malformed((d) => {
        d["environment"] = [];
      }),
    )["environment"],
    [],
  );
});

test("R1.13 an environment name the installer would refuse is refused here, naming it", () => {
  // THE CONTRACT FILE DESCRIBES THE NAME SHAPE IN PROSE AND CARRIES NO PATTERN,
  // so the schema admits this document and the installer's own consumption loop
  // is what would decline it — in a run the collector author never sees.
  refused(
    malformed((d) => {
      d["environment"] = [...d["environment"], { name: "ACME-TOKEN", class: "secret" }];
    }),
    "ACME-TOKEN",
    "[A-Za-z_][A-Za-z0-9_]*",
  );
  // THE CONTROL, and it is the row's whole point: the same name with an
  // underscore renders.
  assert.ok(
    JSON.stringify(
      renders(
        malformed((d) => {
          d["environment"] = [...d["environment"], { name: "ACME_OTHER", class: "secret" }];
        }),
      ),
    ).includes("ACME_OTHER"),
  );
});

test("R1.13 an environment class outside the closed four is refused, naming the class and the four", () => {
  refused(
    malformed((d) => {
      d["environment"] = [...d["environment"], { name: "ACME_OTHER", class: "credential" }];
    }),
    "ACME_OTHER",
    "credential",
    ...ENV_CLASSES,
  );
  // THE CONTROL: every one of the four renders. It is what keeps a class added to
  // the vocabulary from arriving with no fixture behind it.
  for (const admitted of ENV_CLASSES) {
    const document = renders(
      malformed((d) => {
        d["environment"] = [{ name: "ACME_ONE", class: admitted }];
      }),
    );
    assert.equal(((document["environment"] as Msg[])[0] as Msg)["class"], admitted);
  }
});

test("R1.13 one environment name declared twice is refused, naming it", () => {
  refused(
    malformed((d) => {
      d["environment"] = [...d["environment"], { name: "ACME_ROOT", class: "selector" }];
    }),
    "ACME_ROOT",
    "twice",
  );
});

// ---------------------------------------------------------------------------
// THE TRANSPORT. Everything above drives the renderer directly; this drives the
// TOOL, over a real child process on real pipes.
// ---------------------------------------------------------------------------

test("R1.13 describe answers over the stdio transport, and stdout carries nothing but JSON-RPC", async () => {
  // A REAL CHILD RATHER THAN definition.call, and that is the whole reason this
  // row exists beside the in-process one. A stray write inside the describe
  // branch of the call handler is invisible to an in-process call: the driven
  // session collects what the speaker WROTE THROUGH ITS IO, while a stray write
  // goes to the process's own stdout. Only a child on real pipes sees both on one
  // stream, which is where the corruption an operator meets actually happens.
  //
  // AND IT IS THE DESCRIBE CALL SPECIFICALLY. The sample's collect row already
  // polices this stream, so a write at the handler's ENTRY reds there; a write
  // inside the describe branch reds nowhere until this row exists.
  const run = await runChild(
    compiled("examples/sample-collector.js"),
    [initializeRequest(1, "2025-06-18"), rpc(2, "tools/call", { name: DESCRIBE_TOOL_NAME, arguments: {} })],
    { HOME: "/tmp", PATH: "/usr/bin:/bin" },
  );

  assert.ok(run.stdoutLines.length > 0, "control: the child really answered on stdout");
  for (const line of run.stdoutLines) {
    let parsed: Msg;
    try {
      parsed = JSON.parse(line) as Msg;
    } catch {
      assert.fail(
        `describe wrote a non-JSON-RPC line to stdout: ${JSON.stringify(line.slice(0, 160))}. stdout is the ` +
          `protocol stream; a stray print corrupts the framing and reaches an operator as an opaque handshake failure.`,
      );
    }
    assert.equal(parsed["jsonrpc"], "2.0", `stdout line is not a JSON-RPC message: ${line.slice(0, 160)}`);
  }
  assert.equal(
    run.stdoutLines.length,
    run.messages.length,
    "every line on the protocol stream is one of the messages, and no line was skipped at the parser",
  );

  const answer = run.messages.find((m) => (m as Msg)["id"] === 2) as Msg;
  assert.ok(answer !== undefined, `the child never answered the describe call: ${run.stderr}`);
  const result = answer["result"] as Msg;
  assert.notEqual(result["isError"], true, JSON.stringify(result));
  const document = result["structuredContent"] as Msg;
  assert.ok(document !== undefined, "the describe call carried no structured content");

  const ajv = new Ajv2020({ allErrors: true, strict: false });
  const validate = ajv.compile(JSON.parse(describeContractJSON()) as object);
  assert.ok(validate(document), JSON.stringify(validate.errors));
  assert.deepEqual(document, renderDeclaration(new SampleCollector().describe()));
});

test("R1.13 describe is listed over the transport, beside the collect tool", async () => {
  const run = await runChild(
    compiled("examples/sample-collector.js"),
    [initializeRequest(1, "2025-06-18"), rpc(2, "tools/list")],
    { HOME: "/tmp", PATH: "/usr/bin:/bin" },
  );
  const listed = ((run.messages.find((m) => (m as Msg)["id"] === 2) as Msg)["result"] as Msg)["tools"] as Msg[];
  const names = listed.map((t) => t["name"] as string).sort();
  assert.deepEqual(names, ["collect", DESCRIBE_TOOL_NAME].sort());
});

test("R1.13 a declaration the renderer refuses becomes a tool error rather than a thrown process", async () => {
  // THE REFUSAL REACHES THE WIRE, which no row above observes: every one of them
  // calls the renderer directly and reads the exception. A collector whose
  // describe is wrong must meet a NAMED tool error, because the operator running
  // `collector add` is who reads it.
  //
  // IN PROCESS THROUGH THE SPEAKER rather than as a child, because the fixture is
  // a collector this package defines and a child would need a compiled artifact
  // for each malformed shape.
  const collector = {
    tool: () => ({ name: "collect_broken" }),
    paramsSchema: () => ({ type: "object" }),
    describe: () =>
      malformed((d) => {
        d["environment"] = [...d["environment"], { name: "ACME_ROOT", class: "selector" }];
      }),
    walk: () => ({ complete: { walkComplete: true } }) as never,
  };
  const session = driveSession(newSpeakerDefinition(collector as never));
  await session.send(initializeRequest(1, "2025-06-18"));
  const answered = await session.send(rpc(2, "tools/call", { name: DESCRIBE_TOOL_NAME, arguments: {} }));
  const result = (answered[0] as Msg)["result"] as Msg;
  assert.equal(result["isError"], true, JSON.stringify(result));
  const text = ((result["content"] as Msg[])[0] as Msg)["text"] as string;
  assert.ok(text.includes("ACME_ROOT"), `the tool error does not name the offending value: ${text}`);
  assert.ok(text.includes("collect_broken"), `the tool error does not name the collector: ${text}`);
});

test("R1.13 a rendered declaration the SCHEMA refuses becomes a tool error too", async () => {
  // THE SERVED TOOL VALIDATES ITS OWN RESULT, and this is the row that observes
  // it. renderDeclaration checks a declaration's internal consistency; the schema
  // checks shapes it has no reason to know about — here an optional description
  // carrying a number where the contract declares a string. Without this row the
  // validation call in the describe branch could be deleted and nothing would
  // notice.
  const collector = {
    tool: () => ({ name: "collect_broken" }),
    paramsSchema: () => ({ type: "object" }),
    describe: () =>
      malformed((d) => {
        d["environment"] = [...d["environment"], { name: "ACME_EXTRA", class: "selector", description: 7 }];
      }),
    walk: () => ({ complete: { walkComplete: true } }) as never,
  };
  const session = driveSession(newSpeakerDefinition(collector as never));
  await session.send(initializeRequest(1, "2025-06-18"));
  const answered = await session.send(rpc(2, "tools/call", { name: DESCRIBE_TOOL_NAME, arguments: {} }));
  const result = (answered[0] as Msg)["result"] as Msg;
  assert.equal(result["isError"], true, JSON.stringify(result));
  const text = ((result["content"] as Msg[])[0] as Msg)["text"] as string;
  assert.ok(
    text.includes("describe contract schema"),
    `the tool error does not say the schema refused it: ${text}`,
  );
});
