// schema.test.ts — R1.2, R1.3 and R1.11: the advertised schemas, and the
// checked-in copies they are built from.

import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { test } from "node:test";

import {
  advertisedInputSchema,
  advertisedOutputSchema,
  inputContractJSON,
  outputContractJSON,
  requireObjectParamsSchema,
} from "../src/schema.js";
import { goFrameworkDir, packageRoot, repoRoot } from "./helpers.js";

// THE LIST IS READ OFF THE DIRECTORY, not typed here. It was a two-name literal
// when the contract was two documents, and a third document landing on the client
// side was then a file this package could ship stale with every row still green.
// A name that appears here appears because this package HOLDS it; whether the set
// is the right set is the assertion below, against the authority.
const CONTRACT_FILES = readdirSync(join(packageRoot(), "contract"))
  .filter((f) => f.endsWith(".schema.json"))
  .sort();

test("R1.11 this package's contract copies are byte-identical to the Go framework's, in whichever layout this runs in", () => {
  const mine = join(packageRoot(), "contract");
  const theirs = join(goFrameworkDir(), "contract");
  assert.ok(
    existsSync(theirs),
    `the Go framework's contract directory must sit beside this package in both layouts; looked at ${theirs}`,
  );
  // THE SET, NOT ONLY THE BYTES. A per-file comparison over this package's own
  // directory passes over a document it never copied, which is exactly how a
  // third contract file could land upstream and leave this package silently
  // shipping two.
  assert.deepEqual(
    CONTRACT_FILES,
    readdirSync(theirs)
      .filter((f) => f.endsWith(".schema.json"))
      .sort(),
    "this package ships a different SET of contract documents than the Go framework",
  );
  assert.ok(CONTRACT_FILES.length > 0, "the contract directory is empty; every comparison below would be vacuous");
  for (const f of CONTRACT_FILES) {
    const a = readFileSync(join(mine, f));
    const b = readFileSync(join(theirs, f));
    assert.ok(
      a.equals(b),
      `${f} has drifted from the Go framework's copy; a byte here decides what the client's registration gate compares`,
    );
  }
});

test("R1.11 and against the CLIENT's authoritative files, in this repository", (t) => {
  const root = repoRoot();
  if (root === undefined) {
    // A NAMED SKIP, not a silent pass: the published layout carries no client
    // source at all, and the same-run control is the test above, which compares
    // the same bytes against the Go framework's copy and DOES run here. The Go
    // framework's own suite pins that copy against the client's.
    t.skip(
      "the client's contract directory exists only in this repository; running from the published layout, " +
        "where the byte chain is this package -> the Go framework (asserted above) -> the client (asserted by the Go framework's own suite)",
    );
    return;
  }
  const client = resolve(root, "cmd", "knowledge", "internal", "externalcollector", "contract");
  assert.ok(existsSync(client), `the client's contract directory must exist at ${client}`);
  assert.deepEqual(
    CONTRACT_FILES,
    readdirSync(client)
      .filter((f) => f.endsWith(".schema.json"))
      .sort(),
    "this package ships a different SET of contract documents than the client's authoritative directory",
  );
  for (const f of CONTRACT_FILES) {
    const a = readFileSync(join(packageRoot(), "contract", f));
    const b = readFileSync(join(client, f));
    assert.ok(a.equals(b), `${f} has drifted from the client's authoritative file`);
  }
});

test("R1.2 the advertised OUTPUT schema is the contract file verbatim", () => {
  const file = JSON.parse(outputContractJSON()) as Record<string, unknown>;
  const advertised = advertisedOutputSchema();
  // STRUCTURAL, never a serialized-string comparison: a parsing client reorders
  // object keys, and a string comparison there yields a confident false red.
  assert.deepEqual(advertised, file);
  assert.deepEqual(
    Object.keys(advertised).sort(),
    Object.keys(file).sort(),
    "no top-level keyword added and none dropped",
  );
});

test("R1.2 the advertised output schema is a COPY, so a caller cannot mutate what the next call advertises", () => {
  const first = advertisedOutputSchema();
  (first as Record<string, unknown>)["type"] = "array";
  delete (first as Record<string, unknown>)["properties"];
  const second = advertisedOutputSchema();
  assert.equal(second["type"], "object");
  assert.ok("properties" in second);
});

test("R1.3 the advertised INPUT schema is the contract file with ONLY params replaced", () => {
  const file = JSON.parse(inputContractJSON()) as Record<string, unknown>;
  const params = { type: "object", properties: { since: { type: "string" } }, required: ["since"] };
  const advertised = advertisedInputSchema(params);

  assert.deepEqual(advertised["required"], ["id"], "top-level required must stay exactly [\"id\"]");
  assert.deepEqual(
    Object.keys(advertised).sort(),
    Object.keys(file).sort(),
    "no top-level keyword added and none dropped",
  );
  for (const key of Object.keys(file)) {
    if (key === "properties") continue;
    assert.deepEqual(advertised[key], file[key], `the keyword ${key} must be carried through unchanged`);
  }

  const advProps = advertised["properties"] as Record<string, unknown>;
  const fileProps = file["properties"] as Record<string, unknown>;
  assert.deepEqual(Object.keys(advProps).sort(), Object.keys(fileProps).sort());
  assert.deepEqual(advProps["params"], params, "params is the collector's own declaration");
  assert.deepEqual(advProps["id"], fileProps["id"], "id is untouched");
  assert.deepEqual(advProps["context"], fileProps["context"], "context is untouched");
});

test("R1.3 splicing params does not mutate the checked-in copy for the next caller", () => {
  advertisedInputSchema({ type: "object", properties: { a: { type: "string" } } });
  const second = advertisedInputSchema({ type: "object", properties: {} });
  const props = second["properties"] as Record<string, Record<string, unknown>>;
  assert.deepEqual(props["params"], { type: "object", properties: {} });
  assert.deepEqual(JSON.parse(inputContractJSON())["properties"]["params"], {
    type: "object",
    description:
      "The collect params object, passed through verbatim. The provider's own tool schema declares what it accepts inside.",
  });
});

test("R1.3 a params schema that is not an object schema is refused, naming the collector and the type it declared", () => {
  for (const bad of [
    { type: "string" },
    { type: "array", items: { type: "string" } },
    {},
  ]) {
    assert.throws(
      () => requireObjectParamsSchema("collect_graph", bad),
      (err: Error) => {
        assert.match(err.message, /collect_graph/);
        assert.match(err.message, /declares params as an object/);
        return true;
      },
      `params schema ${JSON.stringify(bad)} must be refused`,
    );
  }
  assert.deepEqual(requireObjectParamsSchema("collect_graph", { type: "object" }), { type: "object" });
});
