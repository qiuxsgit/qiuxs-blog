import { render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { MarkdownEditor } from "./MarkdownEditor";

const milkdown = vi.hoisted(() => ({
  create: vi.fn(),
  destroy: vi.fn(),
  factory: vi.fn(),
}));

vi.mock("./milkdown-adapter", () => ({
  createMilkdownEditor: (...args: unknown[]) => {
    milkdown.factory(...args);
    return { create: milkdown.create, destroy: milkdown.destroy };
  },
}));

afterEach(() => vi.resetAllMocks());

it("destroys an editor after delayed creation when visual mode unmounts", async () => {
  let finishCreate!: () => void;
  milkdown.create.mockReturnValue(new Promise<void>((resolve) => { finishCreate = resolve; }));
  milkdown.destroy.mockResolvedValue(undefined);
  const editor = render(<MarkdownEditor onChange={() => undefined} value={"# Draft\n"} />);

  expect(milkdown.factory).toHaveBeenCalledWith(expect.any(HTMLElement), "# Draft\n", expect.any(Function));
  editor.unmount();
  expect(milkdown.destroy).not.toHaveBeenCalled();

  finishCreate();
  await waitFor(() => expect(milkdown.destroy).toHaveBeenCalledTimes(1));
});
