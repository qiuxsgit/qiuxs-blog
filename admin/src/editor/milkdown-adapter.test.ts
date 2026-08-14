import { fireEvent, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { createMilkdownEditor } from "./milkdown-adapter";

afterEach(() => { document.body.replaceChildren(); });

it("parses a whole GFM plain-text paste and reports the exact source Markdown", async () => {
  const root = document.createElement("div");
  document.body.append(root);
  const changed = vi.fn();
  const editor = createMilkdownEditor(root, "", changed);
  await editor.create();
  const canvas = root.querySelector<HTMLElement>("[contenteditable='true']");
  expect(canvas).not.toBeNull();
  const gfm = "| A | B |\n| - | - |\n| 1 | 2 |\n\n- [x] task\n\n~~done~~\n";

  fireEvent.paste(canvas!, {
    clipboardData: {
      getData: (type: string) => type === "text/plain" ? gfm : "",
    },
  });

  expect(changed).toHaveBeenCalledWith(gfm);
  await waitFor(() => expect(root.querySelector("table")).not.toBeNull());
  expect(root.querySelector("li[data-item-type='task'][data-checked='true']")).not.toBeNull();
  expect(root.querySelector("del")).not.toBeNull();
  await editor.destroy();
});
