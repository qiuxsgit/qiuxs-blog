import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import test from "node:test";

test("npm ci accepts the lockfile on Node 20.19.4 without engine warnings", () => {
  const result = spawnSync("npm", ["ci", "--dry-run", "--ignore-scripts"], {
    cwd: new URL("..", import.meta.url),
    encoding: "utf8",
  });

  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.doesNotMatch(result.stderr, /EBADENGINE/, result.stderr);
});
