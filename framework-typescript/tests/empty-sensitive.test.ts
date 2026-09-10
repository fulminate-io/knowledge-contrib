// empty-sensitive.test.ts — the EMPTY-SENSITIVE MARK, this port's half.
//
// A collector declares, per environment name, whether it tells that name PRESENT
// AND EMPTY apart from ABSENT. The mark governs what a worked entry in
// documentation may show rather than what an installer writes: a `${NAME:-}`
// reference resolves to the empty string in the process serving the collect, so
// the child receives the name present and empty, which is inert for an unmarked
// name and a broken collect for a marked one.
//
// THIS PORT OWES THE PROPERTY BECAUSE THE CLIENT'S DECODE IS STRICT. A
// declaration crossing the wire is checked twice, against the JSON Schema and by
// a decode that refuses a field the client does not define; a port that cannot
// express the mark ships collectors that can never be marked, and one that
// spells it differently ships collectors the client refuses by name.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

import { renderDeclaration } from "../src/describe.js";
import { packageRoot } from "./helpers.js";

// A minimal conforming declaration, so each row below differs from its
// neighbours in exactly the environment entry under test.
function declarationWith(environment: Parameters<typeof renderDeclaration>[0]["environment"]) {
  return renderDeclaration({
    behavior: { summarizable: false, embeddable: false, syncable: true },
    nodeTypes: [],
    edgeTypes: [],
    environment,
  });
}

test("a marked name renders the property", () => {
  const out = declarationWith([
    { name: "LOKI_PASSWORD", class: "secret", emptySensitive: true },
  ]);
  assert.deepEqual(out["environment"], [
    { name: "LOKI_PASSWORD", class: "secret", empty_sensitive: true },
  ]);
});

test("the mark is legal on the not-carried class", () => {
  // THE CLASS THAT MAKES THE PROPERTY NECESSARY. An installed entry never carries
  // a not-carried name, so the installer's class table drops it; a document an
  // operator copies can still show it, which is the only place the mark has to
  // live for such a name.
  const out = declarationWith([
    { name: "KUBERNETES_SERVICE_HOST", class: "not-carried", emptySensitive: true },
  ]);
  assert.deepEqual(out["environment"], [
    { name: "KUBERNETES_SERVICE_HOST", class: "not-carried", empty_sensitive: true },
  ]);
});

test("an unmarked name renders no property at all — absent means false", () => {
  // This is what keeps every collector written before the property existed valid:
  // the rendered document is byte-identical to the one it rendered then.
  const omitted = declarationWith([{ name: "HOME", class: "path" }]);
  assert.deepEqual(omitted["environment"], [{ name: "HOME", class: "path" }]);
  const explicitFalse = declarationWith([
    { name: "HOME", class: "path", emptySensitive: false },
  ]);
  assert.deepEqual(explicitFalse["environment"], [{ name: "HOME", class: "path" }]);
});

test("the description and the mark ride together", () => {
  // The control for the rows above: the two optional properties do not exclude
  // each other, so "the mark rendered" is not an artifact of the renderer having
  // stopped rendering everything else.
  const out = declarationWith([
    {
      name: "AZURE_TOKEN_CREDENTIALS",
      class: "selector",
      description: "the credential set",
      emptySensitive: true,
    },
  ]);
  assert.deepEqual(out["environment"], [
    {
      name: "AZURE_TOKEN_CREDENTIALS",
      class: "selector",
      description: "the credential set",
      empty_sensitive: true,
    },
  ]);
});

test("this package's contract copy declares the property, optionally", () => {
  // A rendered mark this port's OWN schema copy does not declare would validate
  // against nothing here and be refused at the client.
  const doc = JSON.parse(
    readFileSync(join(packageRoot(), "contract", "collector_describe.schema.json"), "utf8"),
  );
  const items = doc["properties"]["environment"]["items"];
  assert.equal(items["properties"]["empty_sensitive"]["type"], "boolean");
  assert.deepEqual(items["required"], ["name", "class"]);
});
