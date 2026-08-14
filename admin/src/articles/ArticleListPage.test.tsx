import { cleanup, screen, waitFor, within } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AdminApi, ArticleList, ArticleSummary, ReleaseView } from "../api/admin-api";
import { ApiProblem } from "../api/problem";
import { articleDetail, articleSummary, failedRelease } from "../test/fixtures";
import { renderWithProviders } from "../test/render";
import { ArticleListPage } from "./ArticleListPage";

const componentStyles = readFileSync(resolve(process.cwd(), "src/styles/components.css"), "utf8");

const api = {
  listArticles: vi.fn<AdminApi["listArticles"]>(),
  createArticle: vi.fn<AdminApi["createArticle"]>(),
  trashArticle: vi.fn<AdminApi["trashArticle"]>(),
  untrashArticle: vi.fn<AdminApi["untrashArticle"]>(),
  createRelease: vi.fn<AdminApi["createRelease"]>(),
};

vi.mock("../auth/AuthProvider", () => ({
  useAuth: () => ({ api: api as unknown as AdminApi }),
}));

function article(overrides: Partial<ArticleSummary> = {}): ArticleSummary {
  return { ...articleSummary, ...overrides };
}

function release(overrides: Partial<ReleaseView> = {}): ReleaseView {
  return { ...failedRelease, ...overrides };
}

function Location() {
  return <output aria-label="location">{useLocation().pathname}{useLocation().search}</output>;
}

function renderPage(route = "/articles") {
  return renderWithProviders(<><ArticleListPage /><Location /></>, { route });
}

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("ArticleListPage", () => {
  it("shows loading before rendering exact draft data and the online state", async () => {
    let resolve!: (value: ArticleList) => void;
    api.listArticles.mockReturnValue(new Promise<ArticleList>((done) => { resolve = done; }));

    renderPage();

    expect(screen.getByRole("status", { name: "Loading articles" })).toBeInTheDocument();
    const title = "  Exact draft title  ";
    resolve({ items: [article({ draftTitle: title, draftUpdatedAt: "2026-08-14T01:02:03Z", createdAt: "2026-08-13T04:05:06Z", publishedRevisionId: 22 })] });

    const articleLink = await screen.findByText((_, element) => element?.tagName === "A" && element.textContent === title);
    expect(articleLink.textContent).toBe(title);
    expect(articleLink).toHaveAttribute("aria-label", title);
    expect(screen.getByText("Draft updated: 2026-08-14T01:02:03Z")).toBeInTheDocument();
    expect(screen.getByText("Created: 2026-08-13T04:05:06Z")).toBeInTheDocument();
    expect(screen.getByText("State: active")).toBeInTheDocument();
    expect(screen.getByText("Online")).toBeInTheDocument();
    expect(api.listArticles).toHaveBeenCalledWith({ state: "active" }, expect.any(AbortSignal));
  });

  it("forwards React Query's AbortSignal and aborts an obsolete list request on unmount", async () => {
    let signal: AbortSignal | undefined;
    api.listArticles.mockImplementation(async (_query, requestSignal) => {
      signal = requestSignal;
      return new Promise<ArticleList>(() => undefined);
    });
    const { unmount } = renderPage();

    await waitFor(() => expect(signal).toBeInstanceOf(AbortSignal));
    unmount();

    expect(signal?.aborted).toBe(true);
  });

  it("uses active for missing or invalid state and sends trashed exactly when requested", async () => {
    api.listArticles.mockResolvedValue({ items: [] });
    renderPage("/articles?state=unknown");
    await screen.findByText("No active articles yet.");
    expect(api.listArticles).toHaveBeenCalledWith({ state: "active" }, expect.any(AbortSignal));

    api.listArticles.mockClear();
    renderPage("/articles?state=trashed");
    await screen.findByText("No trashed articles.");
    expect(api.listArticles).toHaveBeenCalledWith({ state: "trashed" }, expect.any(AbortSignal));
  });

  it("offers a retryable, sanitized problem when article loading fails", async () => {
    api.listArticles.mockRejectedValue(new Error("secret internal detail"));
    const { user } = renderPage();

    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to load articles");
    expect(screen.queryByText("secret internal detail")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.listArticles).toHaveBeenCalledTimes(2));
  });

  it("shows an untitled draft fallback and a narrow article action menu", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ draftTitle: "", publishedRevisionId: null })] });
    const { user } = renderPage();

    expect(await screen.findByText("Untitled draft")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Actions for Untitled draft" }));
    expect(screen.getByRole("link", { name: "Edit" })).toHaveAttribute("href", "/articles/11/edit");
    expect(screen.getByRole("button", { name: "Trash" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Unpublish" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Publish" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View release history" })).toHaveAttribute("href", "/publishing");
  });

  it("gives every row operation a 44px touch target", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: 22 })] });
    const active = renderPage();
    await screen.findByText("Build log");
    await active.user.click(screen.getByRole("button", { name: "Actions for Build log" }));

    for (const target of [
      screen.getByRole("button", { name: "Actions for Build log" }),
      screen.getByRole("link", { name: "Edit" }),
      screen.getByRole("button", { name: "Trash" }),
      screen.getByRole("button", { name: "Unpublish" }),
    ]) {
      expect(target).toHaveClass("touch-target");
    }
    active.unmount();

    api.listArticles.mockResolvedValue({ items: [article({ state: "trashed" })] });
    const trashed = renderPage("/articles?state=trashed");
    await screen.findByText("Build log");
    await trashed.user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    expect(screen.getByRole("button", { name: "Restore" })).toHaveClass("touch-target");
    expect(componentStyles).toMatch(/\.touch-target\s*\{[^}]*min-height:\s*44px[^}]*min-width:\s*44px/s);
    expect(componentStyles).toMatch(/\.article-actions\s+\.touch-target\s*\{[^}]*display:\s*inline-flex/s);
  });

  it("creates a bodyless article once and navigates to its editor", async () => {
    api.listArticles.mockResolvedValue({ items: [] });
    let resolve!: (value: typeof articleDetail) => void;
    api.createArticle.mockReturnValue(new Promise<typeof articleDetail>((done) => { resolve = done; }));
    const { user } = renderPage();
    await screen.findByText("No active articles yet.");

    await user.click(screen.getByRole("button", { name: "New article" }));
    expect(screen.getByRole("button", { name: "Creating article" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Creating article" }));

    expect(api.createArticle).toHaveBeenCalledTimes(1);
    expect(api.createArticle).toHaveBeenCalledWith();
    resolve(articleDetail);
    await waitFor(() => expect(screen.getByLabelText("location")).toHaveTextContent("/articles/11/edit"));
  });

  it("disables a pending trash confirmation, refetches the removed row, and focuses New article", async () => {
    api.listArticles.mockResolvedValueOnce({ items: [article({ publishedRevisionId: null })] }).mockResolvedValueOnce({ items: [] });
    let resolveTrash!: () => void;
    api.trashArticle.mockReturnValue(new Promise<void>((done) => { resolveTrash = done; }));
    const { queryClient, user } = renderPage();
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Trash" }));
    const dialog = screen.getByRole("alertdialog", { name: "Trash article" });
    const confirm = within(dialog).getByRole("button", { name: "Confirm trash" });
    await user.click(confirm);
    expect(confirm).toBeDisabled();
    await user.click(confirm);

    expect(api.trashArticle).toHaveBeenCalledTimes(1);
    expect(api.trashArticle).toHaveBeenCalledWith(11);
    resolveTrash();
    await waitFor(() => expect(screen.queryByText("Build log")).not.toBeInTheDocument());
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "New article" })).toHaveFocus();
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["articles", "list"] });
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["articles", "detail", 11] });
  });

  it("keeps a pending trash dialog open when Cancel is pressed, then focuses New article after the row is removed", async () => {
    api.listArticles.mockResolvedValueOnce({ items: [article({ publishedRevisionId: null })] }).mockResolvedValueOnce({ items: [] });
    let resolveTrash!: () => void;
    api.trashArticle.mockReturnValue(new Promise<void>((done) => { resolveTrash = done; }));
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Trash" }));
    const dialog = screen.getByRole("alertdialog", { name: "Trash article" });
    await user.click(within(dialog).getByRole("button", { name: "Confirm trash" }));
    const cancel = within(dialog).getByRole("button", { name: "Cancel" });
    expect(cancel).toBeDisabled();
    await user.click(cancel);
    expect(screen.getByRole("alertdialog", { name: "Trash article" })).toBeInTheDocument();

    resolveTrash();
    await waitFor(() => expect(screen.queryByText("Build log")).not.toBeInTheDocument());
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "New article" })).toHaveFocus();
  });

  it("keeps a pending trash dialog open when Escape is pressed, then focuses New article after the row is removed", async () => {
    api.listArticles.mockResolvedValueOnce({ items: [article({ publishedRevisionId: null })] }).mockResolvedValueOnce({ items: [] });
    let resolveTrash!: () => void;
    api.trashArticle.mockReturnValue(new Promise<void>((done) => { resolveTrash = done; }));
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Trash" }));
    const dialog = screen.getByRole("alertdialog", { name: "Trash article" });
    await user.click(within(dialog).getByRole("button", { name: "Confirm trash" }));
    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog", { name: "Trash article" })).toBeInTheDocument();

    resolveTrash();
    await waitFor(() => expect(screen.queryByText("Build log")).not.toBeInTheDocument());
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "New article" })).toHaveFocus();
  });

  it("re-enables cancellation after a failed lifecycle action", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: null })] });
    api.trashArticle.mockRejectedValue(new Error("service unavailable"));
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Trash" }));
    const dialog = screen.getByRole("alertdialog", { name: "Trash article" });
    await user.click(within(dialog).getByRole("button", { name: "Confirm trash" }));
    const cancel = within(dialog).getByRole("button", { name: "Cancel" });
    await waitFor(() => expect(cancel).not.toBeDisabled());

    await user.click(cancel);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("blocks trash for an article that remains published", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: 22 })] });
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));

    expect(screen.getByRole("button", { name: "Trash" })).toBeDisabled();
    expect(screen.getByText("Unpublish before trashing this article.")).toBeInTheDocument();
  });

  it("disables a pending restore confirmation, refetches the removed row, and focuses New article", async () => {
    api.listArticles.mockResolvedValueOnce({ items: [article({ state: "trashed" })] }).mockResolvedValueOnce({ items: [] });
    let resolveRestore!: () => void;
    api.untrashArticle.mockReturnValue(new Promise<void>((done) => { resolveRestore = done; }));
    const { user } = renderPage("/articles?state=trashed");
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Restore" }));
    const dialog = screen.getByRole("alertdialog", { name: "Restore article" });
    const confirm = within(dialog).getByRole("button", { name: "Confirm restore" });
    await user.click(confirm);
    expect(confirm).toBeDisabled();
    await user.click(confirm);

    expect(api.untrashArticle).toHaveBeenCalledTimes(1);
    expect(api.untrashArticle).toHaveBeenCalledWith(11);
    resolveRestore();
    await waitFor(() => expect(screen.queryByText("Build log")).not.toBeInTheDocument());
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "New article" })).toHaveFocus();
  });

  it("disables a pending unpublish confirmation and creates one Release before handoff", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: 22 })] });
    let resolveRelease!: (value: { release: ReleaseView; job: ReleaseView["latestJob"] }) => void;
    api.createRelease.mockReturnValue(new Promise((done) => { resolveRelease = done; }));
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Unpublish" }));
    const dialog = screen.getByRole("alertdialog", { name: "Unpublish article" });
    const confirm = within(dialog).getByRole("button", { name: "Confirm unpublish" });
    await user.click(confirm);
    expect(confirm).toBeDisabled();
    await user.click(confirm);

    expect(api.createRelease).toHaveBeenCalledTimes(1);
    expect(api.createRelease).toHaveBeenCalledWith({ mode: "unpublish_article", articleId: 11 });
    resolveRelease({ release: release(), job: failedRelease.latestJob });
    await waitFor(() => expect(screen.getByLabelText("location")).toHaveTextContent("/publishing?release=71"));
  });

  it("reports contract-safe ID failures", async () => {
    api.listArticles.mockResolvedValue({ items: [] });
    api.createArticle.mockResolvedValue({ ...articleDetail, id: 0 });
    const { user } = renderPage();
    await screen.findByText("No active articles yet.");

    await user.click(screen.getByRole("button", { name: "New article" }));

    expect(api.createArticle).toHaveBeenCalledTimes(1);
    expect(await screen.findByRole("alert")).toHaveTextContent("Invalid article.id");
  });
});
