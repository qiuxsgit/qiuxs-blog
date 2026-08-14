import { fireEvent, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { articleMarkdownPlugins, createMilkdownEditor } from "./milkdown-adapter";

afterEach(() => { document.body.replaceChildren(); });

it("parses a whole GFM plain-text paste and reports the exact source Markdown", async () => {
  const root = document.createElement("div");
  document.body.append(root);
  const changed = vi.fn();
  const editor = createMilkdownEditor(root, "", changed);
  await editor.create();
  const canvas = root.querySelector<HTMLElement>("[contenteditable='true']");
  expect(canvas).not.toBeNull();
  const gfm = "| A | B |\n| - | - |\n| 1 | 2 |\n\n- [x] task\n\n~~done~~  \n\n";

  fireEvent.paste(canvas!, {
    clipboardData: {
      getData: (type: string) => type === "text/plain" ? gfm : "<pre>different HTML representation</pre>",
    },
  });

  expect(changed).toHaveBeenCalledWith(gfm);
  await waitFor(() => expect(root.querySelector("table")).not.toBeNull());
  expect(root.querySelector("li[data-item-type='task'][data-checked='true']")).not.toBeNull();
  expect(root.querySelector("del")).not.toBeNull();
  await editor.destroy();
});

describe("supported Markdown plugin collection", () => {
  it("contains no HTML, footnote, or full-GFM transformer plugin", () => {
    const names = articleMarkdownPlugins.flatMap((plugin) => plugin.meta?.displayName ?? []);
    expect(names).not.toEqual(expect.arrayContaining([
      "Remark<remarkHtmlTransformer>",
      "RemarkConfig<remarkHtmlTransformer>",
      "Remark<remarkGFMPlugin>",
      "RemarkConfig<remarkGFMPlugin>",
    ]));
    expect(names.some((name) => /html|footnode|footnote/iu.test(name))).toBe(false);
  });

  it("leaves autolink literals and footnote syntax inert", async () => {
    const root = document.createElement("div");
    document.body.append(root);
    const markdown = "Visit https://example.com\n\nReference [^1]\n\n[^1]: note\n";
    const editor = createMilkdownEditor(root, markdown, () => undefined);
    await editor.create();

    expect(root.querySelector("a[href='https://example.com']")).toBeNull();
    expect(root.querySelector("[data-type*='footnote']")).toBeNull();
    expect(root.textContent).toContain("https://example.com");
    await editor.destroy();
  });

  it("rejects unsupported raw HTML deterministically instead of creating an empty editor", async () => {
    const root = document.createElement("div");
    document.body.append(root);
    const editor = createMilkdownEditor(root, "before\n\n<div>raw</div>\n\nafter\n", () => undefined);

    await expect(editor.create()).rejects.toThrow();
    expect(root.querySelector("[contenteditable='true']")).toBeNull();
  });
});
