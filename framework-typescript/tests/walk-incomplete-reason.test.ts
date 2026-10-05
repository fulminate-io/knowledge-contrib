// walk-incomplete-reason.test.ts — the incomplete walk's reason on the wire.
//
// The encoder sends the reason incomplete(reason) carries, as
// walk_incomplete_reason beside walk_complete false, and sends no key at all on a
// complete walk. The package's own strict decoder admits the key, still refuses a
// misspelled one, and refuses the two shapes that contradict the key's contract:
// a reason beside a walk asserting completeness, and an empty reason. An
// incomplete walk with no reason key is admitted, because the key is optional.

import assert from "node:assert/strict";
import { test } from "node:test";

import { complete, incomplete } from "../src/completeness.js";
import { decodeResult } from "../src/contract.js";
import { encodeResult } from "../src/envelope.js";

const wireOf = (value: unknown): Record<string, unknown> =>
  JSON.parse(JSON.stringify(value)) as Record<string, unknown>;

test("an incomplete walk sends its reason as walk_incomplete_reason", () => {
  const wire = wireOf(encodeResult("collect", { complete: incomplete("throttled") }));
  assert.equal(wire["walk_complete"], false);
  assert.equal(wire["walk_incomplete_reason"], "throttled");
});

test("a complete walk sends no walk_incomplete_reason key at all", () => {
  const wire = wireOf(encodeResult("collect", { complete: complete() }));
  assert.equal(wire["walk_complete"], true);
  assert.ok(!("walk_incomplete_reason" in wire), `the complete envelope carries the key: ${JSON.stringify(wire)}`);
});

test("the package's own incomplete result decodes", () => {
  const wire = wireOf(encodeResult("collect", { complete: incomplete("throttled") }));
  const decoded = decodeResult("collect", wire) as Record<string, unknown>;
  assert.equal(decoded["walk_incomplete_reason"], "throttled");
});

test("a misspelled reason key is still refused, naming it", () => {
  assert.throws(
    () => decodeResult("collect", { nodes: [], edges: [], walk_complete: false, walk_incomplete_reasn: "r" }),
    /walk_incomplete_reasn/,
  );
});

test("a reason beside a complete walk is refused, naming the field and the arm", () => {
  assert.throws(
    () => decodeResult("collect", { nodes: [], edges: [], walk_complete: true, walk_incomplete_reason: "r" }),
    (err: Error) => {
      assert.match(err.message, /walk_incomplete_reason/);
      assert.match(err.message, /walk_complete true/);
      return true;
    },
  );
});

test("an empty reason beside an incomplete walk is refused, naming the field and the arm", () => {
  assert.throws(
    () => decodeResult("collect", { nodes: [], edges: [], walk_complete: false, walk_incomplete_reason: "" }),
    (err: Error) => {
      assert.match(err.message, /empty walk_incomplete_reason/);
      assert.match(err.message, /walk_complete false/);
      return true;
    },
  );
});

test("control: an incomplete walk with no reason key is admitted", () => {
  const decoded = decodeResult("collect", { nodes: [], edges: [], walk_complete: false }) as Record<string, unknown>;
  assert.equal(decoded["walk_complete"], false);
  assert.ok(!("walk_incomplete_reason" in decoded));
});
