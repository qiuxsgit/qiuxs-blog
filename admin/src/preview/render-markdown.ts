import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import rehypeStringify from "rehype-stringify";
import rehypeShiki from "@shikijs/rehype";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { unified } from "unified";
import GithubSlugger from "github-slugger";

const MAX_MARKDOWN_BYTES = 2 * 1024 * 1024;
const MAX_HTML_BYTES = 4 * 1024 * 1024;
const IMAGE_PATH = /^\/img\/proxy\/([a-z0-9_-]+)$/u;
const ALLOWED_LANGUAGES = new Set(["go", "typescript", "ts", "javascript", "js", "json", "bash", "sh", "yaml", "yml", "markdown", "md", "css", "text", "txt"]);

export function rewriteImageSource(source: string): string | undefined {
  const match = IMAGE_PATH.exec(source);
  return match ? `https://qiuxs.com/img/proxy/${match[1]}` : undefined;
}

export function isSafeExternalUrl(value: string): boolean {
  try { return new URL(value).protocol === "https:"; } catch { return false; }
}

function rehypePolicy() {
  return (tree: { children?: unknown[] }) => {
    const slugger = new GithubSlugger();
    (tree as any).__slugger = slugger;
    const walk = (node: any) => {
      if (!node || typeof node !== "object") return;
      if (node.type === "element") {
        const properties = node.properties ?? {};
        if (node.tagName === "img") {
          const source = typeof properties.src === "string" ? rewriteImageSource(properties.src) : undefined;
          if (source) properties.src = source;
          else delete properties.src;
          delete properties.srcSet;
          delete properties.onError;
          delete properties.onLoad;
        }
        if (node.tagName === "a") {
          const href = typeof properties.href === "string" ? properties.href : "";
          if (isSafeExternalUrl(href)) {
            properties.target = "_blank";
            properties.rel = "noopener noreferrer";
          } else if (href) {
            delete properties.href;
          }
        }
        if (node.tagName === "code") {
          const className = Array.isArray(properties.className) ? properties.className : [];
          const language = className.find((item: unknown) => typeof item === "string" && item.startsWith("language-"));
          if (language && !ALLOWED_LANGUAGES.has(String(language).slice("language-".length).toLowerCase())) {
            properties.className = className.filter((item: unknown) => item !== language);
          }
        }
        if (node.tagName === "h2" || node.tagName === "h3") {
          const text = collectText(node);
          const id = slugger.slug(text || "section");
          properties.id = id;
        }
      }
      for (const child of node.children ?? []) walk(child);
    };
    for (const child of tree.children ?? []) walk(child);
  };
}

function collectText(node: any): string {
  if (node?.type === "text") return typeof node.value === "string" ? node.value : "";
  return (node?.children ?? []).map(collectText).join("");
}

const sanitizeSchema = {
  ...defaultSchema,
  tagNames: defaultSchema.tagNames?.filter((tag) => tag !== "script" && tag !== "style" && tag !== "iframe"),
  attributes: {
    ...defaultSchema.attributes,
    a: [["href"], ["target"], ["rel"]],
    img: [["src"], ["alt"], ["title"]],
    code: [["className"]],
    h2: [["id"]],
    h3: [["id"]],
  },
  protocols: { ...defaultSchema.protocols, href: ["https"] },
  clobberPrefix: "",
};

export async function renderMarkdown(markdown: string): Promise<string> {
  if (typeof markdown !== "string") throw new Error("Invalid Markdown");
  if (new TextEncoder().encode(markdown).byteLength > MAX_MARKDOWN_BYTES) throw new Error("Markdown is too large");
  const tree = unified().use(remarkParse).use(remarkGfm).parse(markdown);
  const hast = await unified().use(remarkRehype, { allowDangerousHtml: false }).run(tree);
  rehypePolicy()(hast as any);
  const safeTree = await unified().use(rehypeSanitize, sanitizeSchema as any).run(hast);
  const highlightedTree = await unified().use(rehypeShiki, {
    theme: "github-dark",
    langs: ["go", "typescript", "javascript", "json", "bash", "yaml", "markdown", "css"],
  }).run(safeTree);
  const result = String(unified().use(rehypeStringify).stringify(highlightedTree));
  if (new TextEncoder().encode(result).byteLength > MAX_HTML_BYTES) throw new Error("Rendered Markdown is too large");
  return result;
}

export { ALLOWED_LANGUAGES, MAX_MARKDOWN_BYTES };
