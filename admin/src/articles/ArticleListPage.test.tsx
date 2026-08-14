import { cleanup, screen, waitFor } from "@testing-library/react";
import { useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AdminApi, ArticleList, ArticleSummary, ReleaseView } from "../api/admin-api";
import { ApiProblem } from "../api/problem";
import { articleDetail, articleSummary, failedRelease } from "../test/fixtures";
import { renderWithProviders } from "../test/render";
import { ArticleListPage } from "./ArticleListPage";

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
    resolve({ items: [article({ draftTitle: "Exact draft title", draftUpdatedAt: "2026-08-14T01:02:03Z", createdAt: "2026-08-13T04:05:06Z", publishedRevisionId: 22 })] });

    expect(await screen.findByText("Exact draft title")).toBeInTheDocument();
    expect(screen.getByText("Draft updated: 2026-08-14T01:02:03Z")).toBeInTheDocument();
    expect(screen.getByText("Created: 2026-08-13T04:05:06Z")).toBeInTheDocument();
    expect(screen.getByText("State: active")).toBeInTheDocument();
    expect(screen.getByText("Online")).toBeInTheDocument();
    expect(api.listArticles).toHaveBeenCalledWith({ state: "active" });
  });

  it("uses active for missing or invalid state and sends trashed exactly when requested", async () => {
    api.listArticles.mockResolvedValue({ items: [] });
    renderPage("/articles?state=unknown");
    await screen.findByText("No active articles yet.");
    expect(api.listArticles).toHaveBeenCalledWith({ state: "active" });

    api.listArticles.mockClear();
    renderPage("/articles?state=trashed");
    await screen.findByText("No trashed articles.");
    expect(api.listArticles).toHaveBeenCalledWith({ state: "trashed" });
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

  it("confirms an unpublished trash action and invalidates the changed article", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: null })] });
    api.trashArticle.mockResolvedValue(undefined);
    const { queryClient, user } = renderPage();
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Trash" }));
    await user.click(screen.getByRole("button", { name: "Confirm trash" }));

    await waitFor(() => expect(api.trashArticle).toHaveBeenCalledWith(11));
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["articles", "list"] });
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["articles", "detail", 11] });
  });

  it("blocks trash for an article that remains published", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: 22 })] });
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));

    expect(screen.getByRole("button", { name: "Trash" })).toBeDisabled();
    expect(screen.getByText("Unpublish before trashing this article.")).toBeInTheDocument();
  });

  it("confirms a void untrash lifecycle call", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ state: "trashed" })] });
    api.untrashArticle.mockResolvedValue(undefined);
    const { user } = renderPage("/articles?state=trashed");
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Restore" }));
    await user.click(screen.getByRole("button", { name: "Confirm restore" }));

    await waitFor(() => expect(api.untrashArticle).toHaveBeenCalledWith(11));
  });

  it("creates an unpublish release once, then hands off to publishing", async () => {
    api.listArticles.mockResolvedValue({ items: [article({ publishedRevisionId: 22 })] });
    api.createRelease.mockResolvedValue({ release: release(), job: failedRelease.latestJob });
    const { user } = renderPage();
    await screen.findByText("Build log");

    await user.click(screen.getByRole("button", { name: "Actions for Build log" }));
    await user.click(screen.getByRole("button", { name: "Unpublish" }));
    await user.click(screen.getByRole("button", { name: "Confirm unpublish" }));

    await waitFor(() => expect(api.createRelease).toHaveBeenCalledWith({ mode: "unpublish_article", articleId: 11 }));
    expect(screen.getByLabelText("location")).toHaveTextContent("/publishing?release=71");
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
