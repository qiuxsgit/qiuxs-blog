import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { useLocation, useNavigationType } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AdminApi, ArticleDetail, TagView } from "../api/admin-api";
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
    expect(wholeDocumentPlainTextPaste({ currentMarkdown: "", html: "<table></table>", plainText: gfm })).toBeUndefined();
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
});
