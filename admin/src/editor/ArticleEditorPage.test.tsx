import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { useLocation, useNavigationType } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AdminApi, ArticleDetail, TagView } from "../api/admin-api";
import { ApiProblem } from "../api/problem";
import { articleDetail, draftView, tagView } from "../test/fixtures";
import { renderWithProviders } from "../test/render";
import { ArticleEditorPage } from "./ArticleEditorPage";
import { wholeDocumentPlainTextPaste } from "./milkdown-adapter";
import { Route, Routes } from "react-router-dom";

const api = {
  createArticle: vi.fn<AdminApi["createArticle"]>(),
  getArticle: vi.fn<AdminApi["getArticle"]>(),
  saveArticleDraft: vi.fn<AdminApi["saveArticleDraft"]>(),
  listTags: vi.fn<AdminApi["listTags"]>(),
  createTag: vi.fn<AdminApi["createTag"]>(),
  renameTag: vi.fn<AdminApi["renameTag"]>(),
};

const editorStyles = readFileSync(resolve(process.cwd(), "src/styles/editor.css"), "utf8");

vi.mock("../auth/AuthProvider", () => ({
  useAuth: () => ({ api: api as unknown as AdminApi }),
}));

vi.mock("./MarkdownEditor", () => ({
  MarkdownEditor: ({ onChange, value }: { onChange(value: string): void; value: string }) => (
    <textarea aria-label="Visual Markdown" onChange={(event) => onChange(event.currentTarget.value)} value={value} />
  ),
}));

function Location() {
  const location = useLocation();
  return <output aria-label="location">{location.pathname}:{useNavigationType()}</output>;
}

function renderPage(route = "/articles/11/edit") {
  return renderWithProviders(
    <>
      <Routes>
        <Route path="/articles/new" element={<ArticleEditorPage />} />
        <Route path="/articles/:articleId/edit" element={<ArticleEditorPage />} />
      </Routes>
      <Location />
    </>,
    { route },
  );
}

function tag(id: number, name = `Tag ${id}`): TagView {
  return { ...tagView, id, name, slug: `tag-${id}` };
}

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("ArticleEditorPage", () => {
  it("loads the exact ArticleDetail and tag-list items with visual mode and collapsed metadata", async () => {
    api.getArticle.mockResolvedValue(articleDetail);
    api.listTags.mockResolvedValue({ items: [tagView, tag(32, "React")] });
    const { user } = renderPage();

    expect(screen.getByRole("status", { name: "Loading article" })).toBeInTheDocument();
    expect(await screen.findByDisplayValue("Build log")).toHaveAccessibleName("Title");
    expect(screen.getByRole("button", { name: "Visual" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Visual Markdown")).toHaveValue("# Build log\n");
    const metadata = screen.getByText("Metadata").closest("details");
    expect(metadata).not.toHaveAttribute("open");

    await user.click(screen.getByText("Metadata"));
    expect(screen.getByLabelText("Summary")).toHaveValue("Summary");
    expect(screen.getByRole("checkbox", { name: "Go" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "React" })).not.toBeChecked();
    expect(api.getArticle).toHaveBeenCalledWith(11, expect.any(AbortSignal));
    expect(api.listTags).toHaveBeenCalledWith(expect.any(AbortSignal));
  });

  it("creates a bodyless article once and replaces the new route with its validated ID", async () => {
    api.createArticle.mockResolvedValue(articleDetail);
    api.listTags.mockResolvedValue({ items: [] });
    renderPage("/articles/new");

    await waitFor(() => expect(screen.getByLabelText("location")).toHaveTextContent("/articles/11/edit:REPLACE"));
    expect(api.createArticle).toHaveBeenCalledTimes(1);
    expect(api.createArticle).toHaveBeenCalledWith();
    expect(api.getArticle).not.toHaveBeenCalled();
  });

  it("shows a retryable load failure and rejects an invalid route ID without calling the API", async () => {
    api.getArticle.mockRejectedValue(new Error("secret"));
    api.listTags.mockResolvedValue({ items: [] });
    const failed = renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to load article");
    expect(screen.queryByText("secret")).not.toBeInTheDocument();
    await failed.user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.getArticle).toHaveBeenCalledTimes(2));
    failed.unmount();

    api.getArticle.mockClear();
    renderPage("/articles/not-an-id/edit");
    expect(await screen.findByRole("alert")).toHaveTextContent("Invalid article ID");
    expect(api.getArticle).not.toHaveBeenCalled();
  });

  it("synchronizes source and visual content and saves the exact current request", async () => {
    api.getArticle.mockResolvedValue(articleDetail);
    api.listTags.mockResolvedValue({ items: [tagView, tag(32, "React")] });
    api.saveArticleDraft.mockResolvedValue({ ...draftView, lockVersion: 8, contentMd: "# Source\n" });
    const { user } = renderPage();
    await screen.findByDisplayValue("Build log");

    await user.click(screen.getByRole("button", { name: "Source" }));
    fireEvent.change(screen.getByLabelText("Markdown source"), { target: { value: "# Source\n" } });
    await user.click(screen.getByRole("button", { name: "Visual" }));
    expect(screen.getByLabelText("Visual Markdown")).toHaveValue("# Source\n");
    fireEvent.change(screen.getByLabelText("Visual Markdown"), { target: { value: "# Visual\n" } });

    await user.click(screen.getByText("Metadata"));
    await user.click(screen.getByRole("checkbox", { name: "React" }));
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(api.saveArticleDraft).toHaveBeenCalledWith(11, {
      lockVersion: 7,
      title: "Build log",
      summary: "Summary",
      coverMediaId: null,
      contentMd: "# Visual\n",
      tagIds: [31, 32],
    });
  });

  it("preserves an exact whole GFM plain-text paste into an empty canvas", () => {
    const gfm = "| A | B |\n| - | - |\n| 1 | 2 |\n\n- [x] task\n\n~~done~~\n";
    expect(wholeDocumentPlainTextPaste({ currentMarkdown: "", html: "", plainText: gfm })).toBe(gfm);
    expect(wholeDocumentPlainTextPaste({ currentMarkdown: "Already here", html: "", plainText: gfm })).toBeUndefined();
    expect(wholeDocumentPlainTextPaste({ currentMarkdown: "", html: "<pre>different</pre>", plainText: gfm })).toBe(gfm);
  });

  it("creates and renames tags with only {name}, refreshes returned views, and retains selection by ID", async () => {
    api.getArticle.mockResolvedValue(articleDetail);
    api.listTags.mockResolvedValue({ items: [tagView] });
    api.createTag.mockResolvedValue(tag(32, "React"));
    api.renameTag.mockResolvedValue(tag(31, "Golang"));
    const { user } = renderPage();
    await screen.findByDisplayValue("Build log");
    await user.click(screen.getByText("Metadata"));

    await user.type(screen.getByLabelText("New tag name"), "React");
    await user.click(screen.getByRole("button", { name: "Create tag" }));
    expect(api.createTag).toHaveBeenCalledWith({ name: "React" });
    expect(await screen.findByRole("checkbox", { name: "React" })).toBeChecked();

    fireEvent.change(screen.getByLabelText("Rename Go"), { target: { value: "Golang" } });
    await user.click(screen.getByRole("button", { name: "Save rename Go" }));
    expect(api.renameTag).toHaveBeenCalledWith(31, { name: "Golang" });
    expect(await screen.findByRole("checkbox", { name: "Golang" })).toBeChecked();
  });

  it("keeps the 33rd tag unavailable until one of 32 selected IDs is removed", async () => {
    const tags = Array.from({ length: 33 }, (_, index) => tag(index + 1));
    const detail: ArticleDetail = {
      ...articleDetail,
      draft: {
        ...draftView,
        tags: tags.slice(0, 32).map((item, position) => ({ tagId: item.id, name: item.name, slug: item.slug, position })),
      },
    };
    api.getArticle.mockResolvedValue(detail);
    api.listTags.mockResolvedValue({ items: tags });
    const { user } = renderPage();
    await screen.findByDisplayValue("Build log");
    await user.click(screen.getByText("Metadata"));

    expect(screen.getByRole("checkbox", { name: "Tag 33" })).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Tag 1" }));
    expect(screen.getByRole("checkbox", { name: "Tag 33" })).not.toBeDisabled();
  });

  it("announces tag loading, presents a sanitized retryable Problem, and shows an explicit empty state", async () => {
    api.getArticle.mockResolvedValue(articleDetail);
    let rejectTags!: (error: unknown) => void;
    api.listTags.mockReturnValueOnce(new Promise((_, reject) => { rejectTags = reject; }));
    const problem = Object.assign(
      new ApiProblem(503, "tags_unavailable", "req-tags", "Tags unavailable"),
      { cause: new Error("secret backend detail") },
    );
    const { user } = renderPage();
    await screen.findByDisplayValue("Build log");
    await user.click(screen.getByText("Metadata"));
    expect(screen.getByRole("status", { name: "Loading tags" })).toBeInTheDocument();

    rejectTags(problem);
    expect(await screen.findByRole("heading", { name: "Tags unavailable" })).toBeInTheDocument();
    expect(screen.getByText(/Code: tags_unavailable · Request ID: req-tags/iu)).toBeInTheDocument();
    expect(screen.queryByText(/secret backend detail/iu)).not.toBeInTheDocument();

    api.listTags.mockResolvedValueOnce({ items: [] });
    await user.click(screen.getByRole("button", { name: "Retry tags" }));
    expect(await screen.findByText("No tags yet.")).toBeInTheDocument();
    expect(api.listTags).toHaveBeenLastCalledWith(expect.any(AbortSignal));
  });

  it("shows safe mutation Problems, retains retry inputs, and blocks duplicate submissions", async () => {
    api.getArticle.mockResolvedValue(articleDetail);
    api.listTags.mockResolvedValue({ items: [tagView] });
    let rejectCreate!: (error: unknown) => void;
    let rejectRename!: (error: unknown) => void;
    let rejectSave!: (error: unknown) => void;
    api.createTag.mockReturnValue(new Promise((_, reject) => { rejectCreate = reject; }));
    api.renameTag.mockReturnValue(new Promise((_, reject) => { rejectRename = reject; }));
    api.saveArticleDraft.mockReturnValue(new Promise((_, reject) => { rejectSave = reject; }));
    const { user } = renderPage();
    await screen.findByDisplayValue("Build log");
    await user.click(screen.getByText("Metadata"));

    await user.type(screen.getByLabelText("New tag name"), "Retry Create");
    const create = screen.getByRole("button", { name: "Create tag" });
    fireEvent.click(create);
    fireEvent.click(create);
    await waitFor(() => expect(api.createTag).toHaveBeenCalledTimes(1));
    expect(api.createTag).toHaveBeenCalledWith({ name: "Retry Create" });
    rejectCreate(new ApiProblem(409, "tag_exists", "req-create", "Tag already exists"));
    expect(await screen.findByRole("heading", { name: "Tag already exists" })).toBeInTheDocument();
    expect(screen.getByText(/Code: tag_exists · Request ID: req-create/iu)).toBeInTheDocument();
    expect(screen.getByLabelText("New tag name")).toHaveValue("Retry Create");

    fireEvent.change(screen.getByLabelText("Rename Go"), { target: { value: "Retry Rename" } });
    const rename = screen.getByRole("button", { name: "Save rename Go" });
    fireEvent.click(rename);
    fireEvent.click(rename);
    await waitFor(() => expect(api.renameTag).toHaveBeenCalledTimes(1));
    expect(api.renameTag).toHaveBeenCalledWith(31, { name: "Retry Rename" });
    rejectRename(new ApiProblem(503, "rename_failed", "req-rename", "Rename unavailable"));
    expect(await screen.findByRole("heading", { name: "Rename unavailable" })).toBeInTheDocument();
    expect(screen.getByText(/Code: rename_failed · Request ID: req-rename/iu)).toBeInTheDocument();
    expect(screen.getByLabelText("Rename Go")).toHaveValue("Retry Rename");

    const save = screen.getByRole("button", { name: "Save draft" });
    fireEvent.click(save);
    fireEvent.click(save);
    await waitFor(() => expect(api.saveArticleDraft).toHaveBeenCalledTimes(1));
    rejectSave(new ApiProblem(409, "draft_conflict", "req-save", "Draft changed elsewhere"));
    expect(await screen.findByRole("heading", { name: "Draft changed elsewhere" })).toBeInTheDocument();
    expect(screen.getByText(/Code: draft_conflict · Request ID: req-save/iu)).toBeInTheDocument();
    expect(screen.queryByText(/secret/iu)).not.toBeInTheDocument();
  });

  it("marks every narrow editor control for a 44 by 44 pixel hit target", async () => {
    api.getArticle.mockResolvedValue(articleDetail);
    api.listTags.mockResolvedValue({ items: [tagView] });
    const { user } = renderPage();
    await screen.findByDisplayValue("Build log");
    await user.click(screen.getByText("Metadata"));

    for (const control of [
      screen.getByRole("button", { name: "Visual" }),
      screen.getByRole("button", { name: "Source" }),
      screen.getByRole("checkbox", { name: "Go" }),
      screen.getByLabelText("Rename Go"),
      screen.getByRole("button", { name: "Save rename Go" }),
      screen.getByLabelText("New tag name"),
      screen.getByRole("button", { name: "Create tag" }),
    ]) expect(control).toHaveClass("editor-touch-target");

    expect(editorStyles).toMatch(/@media\s*\(max-width:\s*48rem\)[^{]*\{[\s\S]*\.editor-touch-target\s*\{[^}]*min-height:\s*44px[^}]*min-width:\s*44px/s);
  });
});
