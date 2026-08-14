import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import test from "node:test";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { verifyDist } from "./verify-dist.mjs";

async function fixture(index = '<link rel="stylesheet" href="/assets/app-12345678.css"><script src="/assets/app-12345678.js"></script>') {
  const root = await mkdtemp(join(tmpdir(), "blog-admin-dist-"));
  await mkdir(join(root, "assets"));
  await writeFile(join(root, "index.html"), `<html><head>${index}</head><body></body></html>`);
  await writeFile(join(root, "assets", "app-12345678.js"), "console.log('ok');");
  await writeFile(join(root, "assets", "app-12345678.css"), "body{color:black}");
  return root;
}

async function rejects(message, mutate) {
  const root = await fixture();
  try {
    await mutate(root);
    assert.throws(() => verifyDist(root), new RegExp(message));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

test("accepts a complete hashed static artifact", async () => {
  const root = await fixture();
  try { assert.deepEqual(verifyDist(root), { files: 3 }); } finally { await rm(root, { recursive: true, force: true }); }
});

test("rejects missing index, unhashed assets, maps, local paths, protected identifiers, oversized files and missing references", async () => {
  await rejects("dist/index.html", async (root) => { await rm(join(root, "index.html")); });
  await rejects("all JavaScript", async (root) => { await writeFile(join(root, "assets", "plain.js"), "ok"); });
  await rejects("source maps", async (root) => { await writeFile(join(root, "assets", "app-12345678.js.map"), "{}"); });
  await rejects("absolute local path", async (root) => { await writeFile(join(root, "assets", "app-12345678.js"), "const x='/Users/qiuxs/private';"); });
  await rejects("protected identifier", async (root) => { await writeFile(join(root, "assets", "app-12345678.js"), "BLOG_REDIS_PASSWORD"); });
  await rejects("exceeds 2 MiB", async (root) => { await writeFile(join(root, "assets", "app-12345678.js"), "x".repeat(2 * 1024 * 1024 + 1)); });
  await rejects("missing asset reference", async (root) => { await writeFile(join(root, "index.html"), '<script src="/assets/missing-12345678.js"></script>'); });
});

test("does not reject generic secret, token or password words", async () => {
  const root = await fixture();
  try { await writeFile(join(root, "assets", "app-12345678.js"), "const password = 'token'; const secret = true;"); assert.doesNotThrow(() => verifyDist(root)); } finally { await rm(root, { recursive: true, force: true }); }
});
