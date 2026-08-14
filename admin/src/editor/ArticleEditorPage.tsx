import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import type { ArticleDetail, TagView } from "../api/admin-api";
import { queryKeys } from "../api/query-keys";
import { requireEntityId } from "../api/ids";
import { useAuth } from "../auth/AuthProvider";
import { ProblemNotice } from "../components/ProblemNotice";
import { SaveIndicator } from "../components/SaveIndicator";
import { MarkdownEditor } from "./MarkdownEditor";
import { ConflictDialog } from "./ConflictDialog";
import { useAutosave } from "./useAutosave";
import { operationProblem } from "./operation-problem";
import {
  MAX_SELECTED_TAGS,
  fromArticleDetail,
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
  const creatingTag = useRef(false);
  const renamingTag = useRef(false);
  const [createError, setCreateError] = useState<unknown>();
  const [createdArticle, setCreatedArticle] = useState<ArticleDetail>();
  const [loadedDocument, setLoadedDocument] = useState<EditorDocument>();
  const [loadedLockVersion, setLoadedLockVersion] = useState<number>();
  const [mode, setMode] = useState<Mode>("visual");
  const [saveErrors, setSaveErrors] = useState<string[]>([]);
  const [newTagName, setNewTagName] = useState("");
  const [tagNameError, setTagNameError] = useState<string>();
  const [renameNames, setRenameNames] = useState<Record<number, string>>({});

  const createNewArticle = () => {
    if (creating.current) return;
    creating.current = true;
    setCreateError(undefined);
    void api.createArticle().then((article) => {
      const id = requireEntityId(article.id, "article.id");
      setCreatedArticle(article);
      queryClient.setQueryData(queryKeys.article(id), article);
      navigate(`/articles/${id}/edit`, { replace: true });
    }).catch((error: unknown) => {
      creating.current = false;
      setCreateError(error);
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
    setLoadedDocument(fromArticleDetail(article.data));
    setLoadedLockVersion(article.data.draft.lockVersion);
  }, [article.data]);
  const autosave = useAutosave({
    articleId: articleId ?? 1,
    initial: loadedDocument ?? { title: "", summary: "", coverMediaId: null, contentMd: "", tagIds: [] },
    initialLockVersion: loadedLockVersion ?? 1,
    delayMs: 2000,
    save: (input, signal) => api.saveArticleDraft(articleId ?? 1, input, signal),
    reload: (signal) => api.getArticle(articleId ?? 1, signal),
  });
  const document = autosave.document;
  const createTag = useMutation({
    mutationFn: (name: string) => api.createTag({ name }),
    onSuccess: (returned) => {
      queryClient.setQueryData(queryKeys.tags, (current: { items: TagView[] } | undefined) => ({ items: replaceTag(current?.items ?? [], returned) }));
      autosave.edit({ ...autosave.document, tagIds: toggleTagId(autosave.document.tagIds, returned.id, true) });
      setNewTagName("");
    },
    onSettled: () => { creatingTag.current = false; },
  });
  const renameTag = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => api.renameTag(id, { name }),
    onSuccess: (returned) => {
      queryClient.setQueryData(queryKeys.tags, (current: { items: TagView[] } | undefined) => ({ items: replaceTag(current?.items ?? [], returned) }));
      setRenameNames((current) => ({ ...current, [returned.id]: returned.name }));
    },
    onSettled: () => { renamingTag.current = false; },
  });

  if (isNew) {
    if (createError) return <section>
      <h1>New article</h1>
      <ProblemNotice problem={operationProblem(createError, "Unable to create article", "create_article_failed")} />
      <button className="editor-touch-target" onClick={createNewArticle} type="button">Retry creating article</button>
    </section>;
    return <section aria-busy="true" aria-label="Creating article"><h1>New article</h1><p aria-label="Creating article" role="status">Creating article</p></section>;
  }
  if (articleId === undefined) return sanitizeProblem("Invalid article ID");
  if (article.isError) return <section>
    <ProblemNotice problem={operationProblem(article.error, "Unable to load article", "load_article_failed")} />
    <button className="editor-touch-target" onClick={() => void article.refetch()} type="button">Retry loading article</button>
  </section>;
  if (article.isPending || !loadedDocument || loadedLockVersion === undefined) {
    return <section aria-busy="true" aria-label="Loading article"><p aria-label="Loading article" role="status">Loading article</p></section>;
  }

  const setField = <Key extends keyof EditorDocument>(key: Key, value: EditorDocument[Key]) => {
    autosave.edit({ ...autosave.document, [key]: value });
    setSaveErrors([]);
  };
  const submitSave = () => {
    const errors = validateEditorDocument(document, autosave.state.lockVersion);
    setSaveErrors(errors);
    if (errors.length === 0) autosave.retry();
  };
  const submitCreateTag = () => {
    const error = validateTagName(newTagName);
    setTagNameError(error);
    if (error || creatingTag.current) return;
    creatingTag.current = true;
    createTag.mutate(newTagName);
  };
  const submitRename = (tag: TagView) => {
    const name = renameNames[tag.id] ?? tag.name;
    const error = validateTagName(name);
    setTagNameError(error);
    if (error || renamingTag.current) return;
    renamingTag.current = true;
    renameTag.mutate({ id: tag.id, name });
  };

  return (
    <section aria-labelledby="editor-heading" className="article-editor">
      <div className="editor-heading">
        <h1 id="editor-heading">Edit article</h1>
        <button className="button touch-target" disabled={autosave.state.kind === "saving"} onClick={submitSave} type="button">
          {autosave.state.kind === "saving" ? "Saving draft" : "Save draft"}
        </button>
      </div>
      <SaveIndicator state={autosave.state} />
      {saveErrors.length > 0 && <div role="alert"><ul>{saveErrors.map((error) => <li key={error}>{error}</li>)}</ul></div>}
      {autosave.state.kind === "failed" && <><ProblemNotice problem={autosave.state.problem} /><button className="editor-touch-target" onClick={autosave.retry} type="button">Retry saving</button></>}
      {autosave.state.kind === "conflict" && <ConflictDialog problem={autosave.state.problem} local={autosave.state.local} onCopy={() => void navigator.clipboard?.writeText(autosave.copyMarkdown())} onReload={() => { if (window.confirm("Reload the server draft and discard local changes?")) void autosave.reload(true); }} />}

      <label className="editor-title">Title<input aria-label="Title" onChange={(event) => setField("title", event.currentTarget.value)} value={document.title} /></label>

      <details className="editor-metadata">
        <summary className="editor-touch-target">Metadata</summary>
        <label>Summary<textarea aria-label="Summary" onChange={(event) => setField("summary", event.currentTarget.value)} value={document.summary} /></label>
        <label>Cover media ID<input aria-label="Cover media ID" inputMode="numeric" onChange={(event) => setField("coverMediaId", event.currentTarget.value === "" ? null : Number(event.currentTarget.value))} value={document.coverMediaId ?? ""} /></label>
        <fieldset>
          <legend>Tags</legend>
          {tags.isPending && <p aria-label="Loading tags" role="status">Loading tags</p>}
          {tags.isError && <>
            <ProblemNotice problem={operationProblem(tags.error, "Unable to load tags", "load_tags_failed")} />
            <button className="editor-touch-target" onClick={() => void tags.refetch()} type="button">Retry tags</button>
          </>}
          {tags.data?.items.length === 0 && <p>No tags yet.</p>}
          {tags.data?.items.map((tag) => {
            const checked = document.tagIds.includes(tag.id);
            return <div className="tag-row" key={tag.id}>
              <label><input
                checked={checked}
                className="editor-touch-target"
                disabled={!checked && document.tagIds.length >= MAX_SELECTED_TAGS}
                onChange={(event) => setField("tagIds", toggleTagId(document.tagIds, tag.id, event.currentTarget.checked))}
                type="checkbox"
              />{tag.name}</label>
              <input aria-label={`Rename ${tag.name}`} className="editor-touch-target" onChange={(event) => {
                const name = event.currentTarget.value;
                setRenameNames((current) => ({ ...current, [tag.id]: name }));
              }} value={renameNames[tag.id] ?? tag.name} />
              <button className="editor-touch-target" disabled={renameTag.isPending} onClick={() => submitRename(tag)} type="button">Save rename {tag.name}</button>
            </div>;
          })}
          {createTag.isError && <ProblemNotice problem={operationProblem(createTag.error, "Unable to create tag", "create_tag_failed")} />}
          {renameTag.isError && <ProblemNotice problem={operationProblem(renameTag.error, "Unable to rename tag", "rename_tag_failed")} />}
          <div className="tag-create">
            <label>New tag name<input aria-label="New tag name" className="editor-touch-target" onChange={(event) => setNewTagName(event.currentTarget.value)} value={newTagName} /></label>
            <button className="editor-touch-target" disabled={createTag.isPending} onClick={submitCreateTag} type="button">Create tag</button>
          </div>
          {tagNameError && <p role="alert">{tagNameError}</p>}
        </fieldset>
      </details>

      <div aria-label="Editing mode" className="editor-mode" role="group">
        <button aria-pressed={mode === "visual"} className="editor-touch-target" onClick={() => setMode("visual")} type="button">Visual</button>
        <button aria-pressed={mode === "source"} className="editor-touch-target" onClick={() => setMode("source")} type="button">Source</button>
      </div>
      {mode === "visual" ? (
        <MarkdownEditor onChange={(value) => setField("contentMd", value)} value={document.contentMd} />
      ) : (
        <textarea aria-label="Markdown source" className="markdown-source" onChange={(event) => setField("contentMd", event.currentTarget.value)} value={document.contentMd} />
      )}
    </section>
  );
}
