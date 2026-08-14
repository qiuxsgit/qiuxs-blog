import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

it("shows a fixed recoverable error and retries when visual editor creation fails", async () => {
  milkdown.create.mockRejectedValue(new Error("<div>secret draft content</div>"));
  const editor = render(<MarkdownEditor onChange={() => undefined} value={"<div>secret draft content</div>"} />);

  expect(await screen.findByRole("alert")).toHaveTextContent("Unable to open visual editor. Switch to source mode or retry.");
  expect(screen.queryByText(/secret draft content/iu)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Retry visual editor" }));
  await waitFor(() => expect(milkdown.create).toHaveBeenCalledTimes(2));
  editor.unmount();
});
