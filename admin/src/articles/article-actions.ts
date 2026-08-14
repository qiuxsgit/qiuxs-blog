import type { QueryClient } from "@tanstack/react-query";

import type { AdminApi } from "../api/admin-api";
import { requireEntityId, type EntityId } from "../api/ids";
import { ApiProblem } from "../api/problem";
import { queryKeys } from "../api/query-keys";

export async function createArticleAndGetId(api: Pick<AdminApi, "createArticle">): Promise<EntityId> {
  const detail = await api.createArticle();
  return requireEntityId(detail.id, "article.id");
}

export async function trashArticle(api: Pick<AdminApi, "trashArticle">, articleId: unknown): Promise<EntityId> {
  const safeArticleId = requireEntityId(articleId, "article.id");
  await api.trashArticle(safeArticleId);
  return safeArticleId;
}

export async function untrashArticle(api: Pick<AdminApi, "untrashArticle">, articleId: unknown): Promise<EntityId> {
  const safeArticleId = requireEntityId(articleId, "article.id");
  await api.untrashArticle(safeArticleId);
  return safeArticleId;
}

export async function createUnpublishRelease(
  api: Pick<AdminApi, "createRelease">,
  articleId: unknown,
) {
  const safeArticleId = requireEntityId(articleId, "article.id");
  return api.createRelease({ mode: "unpublish_article", articleId: safeArticleId });
}

export async function invalidateArticleCache(queryClient: QueryClient, articleId: EntityId): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.articleListsRoot }),
    queryClient.invalidateQueries({ queryKey: queryKeys.article(articleId) }),
  ]);
}

export function articleActionProblem(error: unknown, fallback: string): ApiProblem {
  if (error instanceof ApiProblem) return error;
  return new ApiProblem(503, "article_action_failed", "client", fallback);
}
