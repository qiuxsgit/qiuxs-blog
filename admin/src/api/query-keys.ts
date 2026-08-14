import type { ListArticlesQuery } from "./admin-api";

export type ArticleListState = NonNullable<ListArticlesQuery["state"]>;

export const queryKeys = {
  me: ["me"] as const,
  tags: ["tags"] as const,
  site: ["settings", "site"] as const,
  hotlink: ["settings", "hotlink"] as const,
  builder: ["builder"] as const,

  articlesRoot: ["articles"] as const,
  articleListsRoot: ["articles", "list"] as const,
  articleList: (state: ArticleListState = "active") => ["articles", "list", state] as const,
  article: (id: number) => ["articles", "detail", id] as const,
  articlePreview: (id: number) => ["articles", "detail", id, "preview"] as const,
  articleVersions: (id: number) => ["articles", "detail", id, "versions"] as const,

  releasesRoot: ["releases"] as const,
  releaseListsRoot: ["releases", "list"] as const,
  releaseList: (limit: number, offset: number) => ["releases", "list", limit, offset] as const,
  release: (id: number) => ["releases", "detail", id] as const,
};
