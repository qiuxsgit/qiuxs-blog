import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocation, useNavigate, useParams } from "react-router-dom";

import { useAuth } from "../auth/AuthProvider";
import type { ArticleDetail } from "../api/admin-api";
import { requireEntityId } from "../api/ids";
import { queryKeys } from "../api/query-keys";
import { ProblemNotice } from "../components/ProblemNotice";
import { operationProblem } from "../editor/operation-problem";
import {
  canCreateVersion,
  createVersionRequest,
  mapRevisionRows,
  reasonLabel,
  replaceArticleDraft,
  restoreVersionDecision,
  restoreVersionRequest,
  versionResultDraft,
  type VersionCapability,
} from "./version-model";

function parseArticleId(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/u.test(value)) return undefined;
  const id = Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

export function ArticleVersionsPage() {
  const { api } = useAuth();
  const articleId = parseArticleId(useParams().articleId);
  const location = useLocation();
  const capability = (location.state as { versionCapability?: VersionCapability } | null)?.versionCapability;
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const article = useQuery({
    queryKey: queryKeys.article(articleId ?? 0),
    queryFn: ({ signal }) => api.getArticle(requireEntityId(articleId, "articleId"), signal),
    enabled: articleId !== undefined,
  });
  const versions = useQuery({
    queryKey: queryKeys.articleVersions(articleId ?? 0),
    queryFn: ({ signal }) => api.listArticleVersions(requireEntityId(articleId, "articleId"), signal),
    enabled: articleId !== undefined,
  });
  const create = useMutation({
    mutationFn: () => api.createArticleVersion(requireEntityId(articleId, "articleId"), createVersionRequest(article.data!.draft.lockVersion)),
    onSuccess: async (result) => {
      const draft = versionResultDraft(result);
      queryClient.setQueryData(queryKeys.article(requireEntityId(articleId, "articleId")), (current: ArticleDetail | undefined) => current ? replaceArticleDraft(current, draft) : current);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.article(requireEntityId(articleId, "articleId")) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.articleVersions(requireEntityId(articleId, "articleId")) }),
      ]);
    },
  });
  const restore = useMutation({
    mutationFn: ({ revisionId }: { revisionId: number }) => api.restoreArticleVersion(
      requireEntityId(articleId, "articleId"), revisionId, restoreVersionRequest(article.data!.draft.lockVersion),
    ),
    onSuccess: async (draft) => {
      const id = requireEntityId(articleId, "articleId");
      queryClient.setQueryData(queryKeys.article(id), (current: ArticleDetail | undefined) => current ? replaceArticleDraft(current, draft) : current);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.article(id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.articleVersions(id) }),
      ]);
      navigate(`/articles/${id}/edit`);
    },
  });

  if (articleId === undefined) return <ProblemNotice problem={operationProblem(new Error("Invalid article ID"), "Invalid article ID", "invalid_article_id")} />;
  if (article.isError || versions.isError) return <section>
    <ProblemNotice problem={operationProblem(article.error ?? versions.error, "Unable to load versions", "load_versions_failed")} />
    <button className="touch-target" onClick={() => { void article.refetch(); void versions.refetch(); }} type="button">Retry</button>
  </section>;
  if (article.isPending || versions.isPending) return <p aria-busy="true" role="status">Loading versions</p>;

  const rows = mapRevisionRows(versions.data.items);
  const canVersion = capability !== undefined && canCreateVersion(capability, article.data.draft.lockVersion);
  return <section aria-labelledby="versions-heading">
    <div className="editor-heading">
      <h1 id="versions-heading">Article versions</h1>
      <button className="button touch-target" disabled={!canVersion || create.isPending} onClick={() => create.mutate()} type="button">{create.isPending ? "Creating version" : "Create version"}</button>
    </div>
    {create.isError && <ProblemNotice problem={operationProblem(create.error, "Unable to create version", "create_version_failed")} />}
    {restore.isError && <ProblemNotice problem={operationProblem(restore.error, "Unable to restore version", "restore_version_failed")} />}
    {rows.length === 0 ? <p>No versions yet.</p> : <div className="version-list">
      {rows.map((version) => <article className="version-row" key={version.id}>
        <div><h2>{version.title || "Untitled"}</h2><p>{reasonLabel(version.reason)} · revision {version.revisionNo} · lock {version.lockVersion}</p><p>{version.summary}</p><p>Created {version.createdAt} · updated {version.updatedAt} · hash {version.contentHash}</p><p>Tags: {version.tags.map((tag) => tag.name).join(", ") || "None"} · Media: {version.media.map((media) => media.mediaId).join(", ") || "None"}</p><details><summary>Markdown snapshot</summary><pre>{version.contentMd}</pre></details></div>
        <button className="touch-target" disabled={restore.isPending} onClick={() => { const selected = restoreVersionDecision(window.confirm("Restore this immutable version into the current draft?"), version.id); if (selected !== undefined) restore.mutate({ revisionId: selected }); }} type="button">Restore</button>
      </article>)}
    </div>}
  </section>;
}
