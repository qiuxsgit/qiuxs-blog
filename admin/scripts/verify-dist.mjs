import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const protectedIdentifiers = Object.freeze([
  "BLOG_ADMIN_PASSWORD",
  "BLOG_REDIS_PASSWORD",
  "BLOG_GFS_APP_SECRET",
  "BLOG_GFS_PUBLIC_READ_SECRET",
  "BLOG_BUNDLE_TOKEN",
  "BLOG_CALLBACK_HMAC_KEY",
  "BLOG_BUILDER_MASTER_KEY",
]);

function filesIn(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    return statSync(path).isDirectory() ? filesIn(path) : [path];
  });
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

const dist = new URL("../dist/", import.meta.url);
const distPath = dist.pathname;
const indexPath = join(distPath, "index.html");

assert(existsSync(indexPath), "dist/index.html is required");

const files = filesIn(distPath);
const names = files.map((file) => file.slice(distPath.length));
assert(names.some((name) => /assets\/.+-[A-Za-z0-9_-]{8,}\.js$/.test(name)), "hashed JavaScript is required");
assert(names.some((name) => /assets\/.+-[A-Za-z0-9_-]{8,}\.css$/.test(name)), "hashed CSS is required");
assert(!names.some((name) => name.endsWith(".map")), "source maps are not allowed in dist");

for (const file of files) {
  const contents = readFileSync(file, "utf8");
  for (const identifier of protectedIdentifiers) {
    assert(!contents.includes(identifier), `protected identifier found: ${identifier}`);
  }
}
