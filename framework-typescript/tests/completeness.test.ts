// completeness.test.ts — R1.6: the completeness value has no usable zero.

import assert from "node:assert/strict";
import { test } from "node:test";

import { Completeness, complete, incomplete, isCompleteness } from "../src/completeness.js";

test("R1.6 complete() asserts a complete walk and carries no reason", () => {
  const c = complete();
  assert.equal(c.isComplete(), true);
  assert.equal(c.reason(), "");
});

test("R1.6 incomplete(reason) asserts an incomplete walk and carries the reason", () => {
  const c = incomplete("the region listing was throttled");
  assert.equal(c.isComplete(), false);
  assert.equal(c.reason(), "the region listing was throttled");
});

test("R1.6 isCompleteness admits a constructed value and refuses every forgery", () => {
  assert.equal(isCompleteness(complete()), true);
  assert.equal(isCompleteness(incomplete("partial")), true);

  // THE FORGERIES, which are what the run-time guard exists for: TypeScript's
  // casts make the compile-time half evadable, so each of these reaches the
  // encoder in a real collector that used `as unknown as Completeness`.
  assert.equal(isCompleteness(undefined), false, "undefined asserts nothing");
  assert.equal(isCompleteness(null), false, "null asserts nothing");
  assert.equal(isCompleteness(true), false, "a bare boolean is the shape this type exists to refuse");
  assert.equal(isCompleteness(false), false);
  assert.equal(isCompleteness({}), false, "an object literal is not an assertion");
  assert.equal(
    isCompleteness({ isComplete: () => true, reason: () => "" }),
    false,
    "a structurally identical duck is still not one: the private field is the discriminator",
  );
  assert.equal(
    isCompleteness(Object.create(Completeness.prototype) as unknown),
    false,
    "a prototype-only forgery carries no private field and must be refused",
  );
});
