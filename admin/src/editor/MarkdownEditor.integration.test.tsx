import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { axe } from "jest-axe";
import { afterEach, expect, it, vi } from "vitest";

import { MarkdownEditor } from "./MarkdownEditor";

afterEach(cleanup);

it("updates a mounted visual document from an external value without echoing onChange", async () => {
  const changed = vi.fn();
  const view = render(<MarkdownEditor onChange={changed} value={"# Initial\n"} />);
  await waitFor(() => expect(view.container).toHaveTextContent("Initial"));

  view.rerender(<MarkdownEditor onChange={changed} value={"# Server\n"} />);

  await waitFor(() => expect(view.container).toHaveTextContent("Server"));
  expect(view.container).not.toHaveTextContent("Initial");
  expect(changed).not.toHaveBeenCalled();
});

it("names the actual ProseMirror textbox and has no automated accessibility violations", async () => {
  render(<main><MarkdownEditor onChange={() => undefined} value={"# Accessible\n"} /></main>);

  expect(await screen.findByRole("textbox", { name: "Article Markdown" })).toHaveAttribute("contenteditable", "true");
  expect((await axe(document.body)).violations).toEqual([]);
});

it("reports a fixed recoverable error when an external value contains unsupported raw HTML", async () => {
  const view = render(<MarkdownEditor onChange={() => undefined} value={"# Initial\n"} />);
  await waitFor(() => expect(view.container).toHaveTextContent("Initial"));

  view.rerender(<MarkdownEditor onChange={() => undefined} value={"<div>secret raw HTML</div>\n"} />);

  expect(await screen.findByRole("alert")).toHaveTextContent("Unable to open visual editor. Switch to source mode or retry.");
  expect(screen.queryByText(/secret raw HTML/iu)).not.toBeInTheDocument();
});
