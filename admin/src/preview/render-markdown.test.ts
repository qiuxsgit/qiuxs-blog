import { describe, expect, it } from "vitest";
import { renderMarkdown, rewriteImageSource } from "./render-markdown";

describe("safe markdown renderer", () => {
  it("supports approved GFM syntax and does not mutate source", async () => {
    const source = "## Hello\n\n- [x] done\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n~~old~~\n\n```go\npackage main\n```";
    const copy = source;
    const html = await renderMarkdown(source);
    expect(html).toContain("<table>");
    expect(html).toContain("<input type=\"checkbox\" checked disabled>");
    expect(html).toContain("<del>old</del>");
    expect(html).toContain("class=\"shiki github-dark\"");
    expect(source).toBe(copy);
  });

  it("removes raw HTML, scripts, handlers and unsafe protocols", async () => {
    const html = await renderMarkdown('<script>alert(1)</script><div onclick="x">x</div> [bad](javascript:alert(1)) ![x](javascript:bad)');
    expect(html).not.toMatch(/script|onclick|javascript:/iu);
  });

  it("rewrites only the exact private image path", () => {
    expect(rewriteImageSource("/img/proxy/abc_123")).toBe("https://qiuxs.com/img/proxy/abc_123");
    expect(rewriteImageSource("/img/proxy/")).toBeUndefined();
    expect(rewriteImageSource("/img/proxy/key?x=1")).toBeUndefined();
    expect(rewriteImageSource("https://example.com/x")).toBeUndefined();
  });

  it("gives duplicate h2/h3 headings deterministic unique IDs and safe links", async () => {
    const html = await renderMarkdown("## Same\n\n## Same\n\n[docs](https://example.com/docs)\n\n![ok](/img/proxy/key)");
    expect(html).toContain('id="same"');
    expect(html).toContain('id="same-1"');
    expect(html).toContain('target="_blank"');
    expect(html).toContain('rel="noopener noreferrer"');
    expect(html).toContain('src="https://qiuxs.com/img/proxy/key"');
  });
});
