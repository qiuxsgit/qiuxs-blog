import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, normalize, relative, resolve } from "node:path";

const MAX_FILE_BYTES = 2 * 1024 * 1024;
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
  if (!condition) throw new Error(message);
}

function localPathInText(contents) {
  return /(?:file:\/\/|(?:^|[\s"'`])(?:\/Users\/|\/home\/|\/private\/|\/tmp\/)|(?:^|[\s"'`])[A-Za-z]:\\)/m.test(contents);
}

function assetReferences(file, contents) {
  const references = [];
  if (file.endsWith(".html")) {
    for (const match of contents.matchAll(/(?:src|href)=["']([^"']+)["']/g)) references.push(match[1]);
  }
  if (file.endsWith(".css")) {
    for (const match of contents.matchAll(/url\(["']?([^)'"\s]+)["']?\)/g)) references.push(match[1]);
  }
  return references;
}

function verifyReference(file, reference, distPath) {
  if (!reference || reference.startsWith("#") || reference.startsWith("data:") || reference.startsWith("http:") || reference.startsWith("https:") || reference.startsWith("mailto:")) return;
  const clean = reference.split(/[?#]/, 1)[0];
  if (!clean || clean === "/") return;
  const target = clean.startsWith("/") ? resolve(distPath, `.${clean}`) : resolve(file, "..", clean);
  const rel = relative(distPath, target);
  assert(!rel.startsWith("..") && !rel.includes(".." + normalize("/")), `asset reference escapes dist: ${reference}`);
  assert(existsSync(target) && statSync(target).isFile(), `missing asset reference: ${reference}`);
}

export function verifyDist(inputPath) {
  const distPath = resolve(inputPath);
  const indexPath = join(distPath, "index.html");
  assert(existsSync(indexPath), "dist/index.html is required");
  const files = filesIn(distPath);
  const names = files.map((file) => relative(distPath, file).replaceAll("\\", "/"));
  assert(names.some((name) => /^assets\/.+-[A-Za-z0-9_-]{8,}\.js$/.test(name)), "hashed JavaScript is required");
  assert(names.some((name) => /^assets\/.+-[A-Za-z0-9_-]{8,}\.css$/.test(name)), "hashed CSS is required");
  assert(!names.some((name) => name.endsWith(".map")), "source maps are not allowed in dist");
  assert(!names.some((name) => /^assets\/.+\.(?:js|css)$/.test(name) && !/-[A-Za-z0-9_-]{8,}\.(?:js|css)$/.test(name)), "all JavaScript and CSS assets must be hashed");

  for (const file of files) {
    const stats = statSync(file);
    assert(stats.size <= MAX_FILE_BYTES, `dist file exceeds 2 MiB: ${relative(distPath, file)}`);
    const contents = readFileSync(file, "utf8");
    assert(!localPathInText(contents), `absolute local path found: ${relative(distPath, file)}`);
    for (const identifier of protectedIdentifiers) assert(!contents.includes(identifier), `protected identifier found: ${identifier}`);
    for (const reference of assetReferences(file, contents)) verifyReference(file, reference, distPath);
  }
  return { files: names.length };
}

if (process.argv[1] === new URL(import.meta.url).pathname) {
  verifyDist(new URL("../dist/", import.meta.url).pathname);
  console.log("dist artifact verification passed");
}
