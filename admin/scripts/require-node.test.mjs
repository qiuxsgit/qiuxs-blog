import test from "node:test";
import assert from "node:assert/strict";
import { assertNodeVersion } from "./require-node.mjs";

test("accepts only Node 20.19.4", () => {
  assert.doesNotThrow(() => assertNodeVersion("v20.19.4"));
  assert.throws(() => assertNodeVersion("v20.19.3"), /Node 20\.19\.4 required/);
  assert.throws(() => assertNodeVersion("v22.20.0"), /Node 20\.19\.4 required/);
});
