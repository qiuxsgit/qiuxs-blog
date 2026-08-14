import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import type { ArticleDetail, TagView } from "../api/admin-api";
import { queryKeys } from "../api/query-keys";
import { requireEntityId } from "../api/ids";
import { useAuth } from "../auth/AuthProvider";
import { MarkdownEditor } from "./MarkdownEditor";
import {
  MAX_SELECTED_TAGS,
  fromArticleDetail,
  toSaveRequest,
  toggleTagId,
  validateEditorDocument,
  validateTagName,
  type EditorDocument,
} from "./editor-document";
import "../styles/editor.css";

type Mode = "visual" | "source";

function safeArticleId(raw: string | undefined): number | undefined {
  if (!raw || !/^\d+$/u.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

function replaceTag(items: readonly TagView[], returned: TagView): TagView[] {
  const index = items.findIndex((item) => item.id === returned.id);
  if (index < 0) return [...items, returned];
  return items.map((item) => item.id === returned.id ? returned : item);
}

function sanitizeProblem(title: string) {
  return <div role="alert"><p>{title}</p></div>;
}

export function ArticleEditorPage() {
  const { api } = useAuth();
  const { articleId: rawArticleId } = useParams();
  const articleId = safeArticleId(rawArticleId);
  const isNew = rawArticleId === undefined;
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const creating = useRef(false);
  const [createError, setCreateError] = useState(false);
  const [createdArticle, setCreatedArticle] = useState<ArticleDetail>();
  const [document, setDocument] = useState<EditorDocument>();
  const [lockVersion, setLockVersion] = useState<number>();
  const [mode, setMode] = useState<Mode>("visual");
  const [saveErrors, setSaveErrors] = useState<string[]>([]);
  const [newTagName, setNewTagName] = useState("");
  const [tagNameError, setTagNameError] = useState<string>();
  const [renameNames, setRenameNames] = useState<Record<number, string>>({});

  const createNewArticle = () => {
    if (creating.current) return;
    creating.current = true;
    setCreateError(false);
    void api.createArticle().then((article) => {
      const id = requireEntityId(article.id, "article.id");
      setCreatedArticle(article);
      queryClient.setQueryData(queryKeys.article(id), article);
      navigate(`/articles/${id}/edit`, { replace: true });
    }).catch(() => {
      creating.current = false;
      setCreateError(true);
    });
  };

  useEffect(() => {
    if (isNew) createNewArticle();
  }, [isNew]);

  const article = useQuery({
    queryKey: queryKeys.article(articleId ?? 0),
    queryFn: ({ signal }) => createdArticle?.id === articleId ? createdArticle : api.getArticle(articleId!, signal),
    enabled: !isNew && articleId !== undefined,
  });
  const tags = useQuery({
    queryKey: queryKeys.tags,
    queryFn: ({ signal }) => api.listTags(signal),
  });

  useEffect(() => {
    if (!article.data) return;
    setDocument(fromArticleDetail(article.data));
    setLockVersion(article.data.draft.lockVersion);
  }, [article.data]);

  const save = useMutation({
    mutationFn: (input: { articleId: number; document: EditorDocument; lockVersion: number }) => (
      api.saveArticleDraft(input.articleId, toSaveRequest(input.document, input.lockVersion))
    ),
    onSuccess: (draft) => {
      setLockVersion(draft.lockVersion);
      setDocument((current) => current ? { ...current, ...fromArticleDetail({
        ...(article.data as ArticleDetail),
        draft,
      }) } : current);
      queryClient.setQueryData<ArticleDetail>(queryKeys.article(draft.articleId), (current) => current ? { ...current, draft } : current);
    },
  });
  const createTag = useMutation({
    mutationFn: (name: string) => api.createTag({ name }),
    onSuccess: (returned) => {
      queryClient.setQueryData(queryKeys.tags, (current: { items: TagView[] } | undefined) => ({ items: replaceTag(current?.items ?? [], returned) }));
      setDocument((current) => current ? { ...current, tagIds: toggleTagId(current.tagIds, returned.id, true) } : current);
      setNewTagName("");
    },
  });
  const renameTag = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => api.renameTag(id, { name }),
    onSuccess: (returned) => {
      queryClient.setQueryData(queryKeys.tags, (current: { items: TagView[] } | undefined) => ({ items: replaceTag(current?.items ?? [], returned) }));
      setRenameNames((current) => ({ ...current, [returned.id]: returned.name }));
    },
  });

  if (isNew) {
    if (createError) return <section><h1>New article</h1>{sanitizeProblem("Unable to create article")}<button onClick={createNewArticle} type="button">Retry</button></section>;
    return <section aria-busy="true" aria-label="Creating article"><h1>New article</h1><p aria-label="Creating article" role="status">Creating article</p></section>;
  }
  if (articleId === undefined) return sanitizeProblem("Invalid article ID");
  if (article.isError) return <section>{sanitizeProblem("Unable to load article")}<button onClick={() => void article.refetch()} type="button">Retry</button></section>;
  if (article.isPending || !document || lockVersion === undefined) {
    return <section aria-busy="true" aria-label="Loading article"><p aria-label="Loading article" role="status">Loading article</p></section>;
  }

  const setField = <Key extends keyof EditorDocument>(key: Key, value: EditorDocument[Key]) => {
    setDocument((current) => current ? { ...current, [key]: value } : current);
    setSaveErrors([]);
  };
  const submitSave = () => {
    const errors = validateEditorDocument(document, lockVersion);
    setSaveErrors(errors);
    if (errors.length > 0 || save.isPending) return;
    save.mutate({ articleId, document, lockVersion });
  };
  const submitCreateTag = () => {
    const error = validateTagName(newTagName);
    setTagNameError(error);
    if (!error) createTag.mutate(newTagName);
  };
  const submitRename = (tag: TagView) => {
    const name = renameNames[tag.id] ?? tag.name;
    const error = validateTagName(name);
    setTagNameError(error);
    if (!error) renameTag.mutate({ id: tag.id, name });
  };

  return (
    <section aria-labelledby="editor-heading" className="article-editor">
      <div className="editor-heading">
        <h1 id="editor-heading">Edit article</h1>
        <button className="button touch-target" disabled={save.isPending} onClick={submitSave} type="button">
          {save.isPending ? "Saving draft" : "Save draft"}
        </button>
      </div>
      {(saveErrors.length > 0 || save.isError) && <div role="alert">
        {saveErrors.length > 0 ? <ul>{saveErrors.map((error) => <li key={error}>{error}</li>)}</ul> : <p>Unable to save draft</p>}
      </div>}

      <label className="editor-title">Title<input aria-label="Title" onChange={(event) => setField("title", event.currentTarget.value)} value={document.title} /></label>

      <details className="editor-metadata">
        <summary>Metadata</summary>
        <label>Summary<textarea aria-label="Summary" onChange={(event) => setField("summary", event.currentTarget.value)} value={document.summary} /></label>
        <label>Cover media ID<input aria-label="Cover media ID" inputMode="numeric" onChange={(event) => setField("coverMediaId", event.currentTarget.value === "" ? null : Number(event.currentTarget.value))} value={document.coverMediaId ?? ""} /></label>
        <fieldset>
          <legend>Tags</legend>
          {tags.isError && sanitizeProblem("Unable to load tags")}
          {tags.data?.items.map((tag) => {
            const checked = document.tagIds.includes(tag.id);
            return <div className="tag-row" key={tag.id}>
              <label><input
                checked={checked}
                disabled={!checked && document.tagIds.length >= MAX_SELECTED_TAGS}
                onChange={(event) => setField("tagIds", toggleTagId(document.tagIds, tag.id, event.currentTarget.checked))}
                type="checkbox"
              />{tag.name}</label>
              <input aria-label={`Rename ${tag.name}`} onChange={(event) => {
                const name = event.currentTarget.value;
                setRenameNames((current) => ({ ...current, [tag.id]: name }));
              }} value={renameNames[tag.id] ?? tag.name} />
              <button disabled={renameTag.isPending} onClick={() => submitRename(tag)} type="button">Save rename {tag.name}</button>
            </div>;
          })}
          <div className="tag-create">
            <label>New tag name<input aria-label="New tag name" onChange={(event) => setNewTagName(event.currentTarget.value)} value={newTagName} /></label>
            <button disabled={createTag.isPending} onClick={submitCreateTag} type="button">Create tag</button>
          </div>
          {tagNameError && <p role="alert">{tagNameError}</p>}
        </fieldset>
      </details>

      <div aria-label="Editing mode" className="editor-mode" role="group">
        <button aria-pressed={mode === "visual"} onClick={() => setMode("visual")} type="button">Visual</button>
        <button aria-pressed={mode === "source"} onClick={() => setMode("source")} type="button">Source</button>
      </div>
      {mode === "visual" ? (
        <MarkdownEditor onChange={(value) => setField("contentMd", value)} value={document.contentMd} />
      ) : (
        <textarea aria-label="Markdown source" className="markdown-source" onChange={(event) => setField("contentMd", event.currentTarget.value)} value={document.contentMd} />
      )}
    </section>
  );
}
