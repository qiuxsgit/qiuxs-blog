import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import { requireEntityId } from "../api/ids";
import { queryKeys } from "../api/query-keys";
import { ProblemNotice } from "../components/ProblemNotice";
import { operationProblem } from "../editor/operation-problem";
import { renderMarkdown } from "./render-markdown";
import "../../../contracts/markdown/article-content.css";

export function ArticlePreviewPage() {
  const { api } = useAuth();
  const rawId = useParams().articleId;
  const articleId = rawId && /^\d+$/u.test(rawId) ? Number(rawId) : undefined;
  const query = useQuery({
    queryKey: queryKeys.article(articleId ?? 0),
    queryFn: ({ signal }) => api.getArticlePreview(requireEntityId(articleId, "articleId"), signal),
    enabled: articleId !== undefined && Number.isSafeInteger(articleId) && articleId > 0,
  });
  if (articleId === undefined) return <ProblemNotice problem={operationProblem(new Error("Invalid article ID"), "Invalid article ID", "invalid_article_id")} />;
  if (query.isPending) return <p role="status">Loading preview</p>;
  if (query.isError) return <ProblemNotice problem={operationProblem(query.error, "Unable to load preview", "load_preview_failed")} />;
  return <article className="article-content" data-slug={query.data.slug}><h1>{query.data.draft.title}</h1><p>{query.data.draft.summary}</p><PreviewBody markdown={query.data.draft.contentMd} /></article>;
}

function PreviewBody({ markdown }: { markdown: string }) {
  const [html, setHtml] = useState<string>();
  const [error, setError] = useState<unknown>();
  useEffect(() => { let active = true; void renderMarkdown(markdown).then((value) => { if (active) setHtml(value); }).catch((value: unknown) => { if (active) setError(value); }); return () => { active = false; }; }, [markdown]);
  if (error) return <ProblemNotice problem={operationProblem(error, "Unable to render preview", "render_preview_failed")} />;
  if (html === undefined) return <p role="status">Rendering preview</p>;
  return <div dangerouslySetInnerHTML={{ __html: html }} />;
}
