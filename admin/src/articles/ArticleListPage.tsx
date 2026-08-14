import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";

import type { ArticleSummary } from "../api/admin-api";
import { useAuth } from "../auth/AuthProvider";
import { AsyncPage } from "../components/AsyncPage";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { queryKeys, type ArticleListState } from "../api/query-keys";
import { syncReleaseCache } from "../publishing/release-cache";
import {
  articleActionProblem,
  createArticleAndGetId,
  createUnpublishRelease,
  invalidateArticleCache,
  trashArticle,
  untrashArticle,
} from "./article-actions";

type DialogAction = "trash" | "restore" | "unpublish";

interface PendingDialog {
  action: DialogAction;
  article: ArticleSummary;
}

function currentState(search: string): ArticleListState {
  return new URLSearchParams(search).get("state") === "trashed" ? "trashed" : "active";
}

function draftTitle(article: ArticleSummary): string {
  return article.draftTitle.trim().length === 0 ? "Untitled draft" : article.draftTitle;
}

function actionCopy(action: DialogAction): { confirmLabel: string; description: string; title: string } {
  switch (action) {
    case "trash":
      return {
        title: "Trash article",
        description: "This removes the article from the active list. You can restore it later.",
        confirmLabel: "Confirm trash",
      };
    case "restore":
      return {
        title: "Restore article",
        description: "This returns the article to the active list.",
        confirmLabel: "Confirm restore",
      };
    case "unpublish":
      return {
        title: "Unpublish article",
        description: "This starts a Release. The article stays online until that Release succeeds.",
        confirmLabel: "Confirm unpublish",
      };
  }
}

export function ArticleListPage() {
  const { api } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const state = currentState(location.search);
  const [pendingDialog, setPendingDialog] = useState<PendingDialog>();
  const [actionProblem, setActionProblem] = useState<ReturnType<typeof articleActionProblem>>();
  const [openActionsFor, setOpenActionsFor] = useState<number>();
  const creating = useRef(false);
  const confirmingAction = useRef(false);
  const newArticleButton = useRef<HTMLButtonElement>(null);
  const focusNewArticleOnDialogClose = useRef(false);

  useEffect(() => {
    if (!pendingDialog && focusNewArticleOnDialogClose.current) {
      newArticleButton.current?.focus();
      focusNewArticleOnDialogClose.current = false;
    }
  }, [pendingDialog]);

  const articles = useQuery({
    queryKey: queryKeys.articleList(state),
    queryFn: ({ signal }) => api.listArticles({ state }, signal),
  });
  const create = useMutation({
    mutationFn: () => createArticleAndGetId(api),
    onSuccess: (articleId) => navigate(`/articles/${articleId}/edit`),
    onError: (error) => setActionProblem(articleActionProblem(error, "Unable to create article")),
    onSettled: () => { creating.current = false; },
  });
  const lifecycle = useMutation({
    mutationFn: async (pending: PendingDialog) => {
      if (pending.action === "trash") return trashArticle(api, pending.article.id);
      return untrashArticle(api, pending.article.id);
    },
    onSuccess: async (articleId) => {
      await invalidateArticleCache(queryClient, articleId);
      focusNewArticleOnDialogClose.current = true;
      setPendingDialog(undefined);
    },
    onError: (error) => setActionProblem(articleActionProblem(error, "Unable to update article")),
    onSettled: () => { confirmingAction.current = false; },
  });
  const unpublish = useMutation({
    mutationFn: async (pending: PendingDialog) => {
      const result = await createUnpublishRelease(api, pending.article.id);
      await syncReleaseCache(queryClient, result.release, "create");
      return result.release.id;
    },
    onSuccess: (releaseId) => {
      setPendingDialog(undefined);
      navigate(`/publishing?release=${releaseId}`);
    },
    onError: (error) => setActionProblem(articleActionProblem(error, "Unable to start unpublish release")),
    onSettled: () => { confirmingAction.current = false; },
  });

  const confirmAction = () => {
    if (!pendingDialog || confirmingAction.current) return;
    confirmingAction.current = true;
    setActionProblem(undefined);
    if (pendingDialog.action === "unpublish") {
      unpublish.mutate(pendingDialog);
    } else {
      lifecycle.mutate(pendingDialog);
    }
  };

  const cancelAction = () => {
    if (confirmingAction.current) return;
    setPendingDialog(undefined);
  };

  const dialogCopy = pendingDialog ? actionCopy(pendingDialog.action) : undefined;
  const emptyMessage = state === "trashed" ? "No trashed articles." : "No active articles yet.";
  const actionPending = lifecycle.isPending || unpublish.isPending;

  return (
    <section aria-labelledby="articles-heading">
      <div className="page-heading">
        <div>
          <h1 id="articles-heading">Articles</h1>
          <p>Manage drafts, visibility, and article lifecycle.</p>
          <p><Link to="/publishing">View release history</Link></p>
        </div>
        <button className="button touch-target" disabled={create.isPending} onClick={() => {
          if (creating.current) return;
          creating.current = true;
          setActionProblem(undefined);
          create.mutate();
        }} ref={newArticleButton} type="button">
          {create.isPending ? "Creating article" : "New article"}
        </button>
      </div>

      <nav aria-label="Article state">
        <Link aria-current={state === "active" ? "page" : undefined} to="/articles?state=active">Active</Link>
        {" · "}
        <Link aria-current={state === "trashed" ? "page" : undefined} to="/articles?state=trashed">Trashed</Link>
      </nav>

      {actionProblem && <div className="article-action-problem"><AsyncPage error={actionProblem} label="Article action" loading={false} /></div>}

      {articles.isError ? (
        <>
          <AsyncPage error={articleActionProblem(articles.error, "Unable to load articles")} label="Loading articles" loading={false} />
          <button className="button button-secondary touch-target" onClick={() => void articles.refetch()} type="button">Retry</button>
        </>
      ) : (
        <AsyncPage
          empty={articles.data?.items.length === 0 ? <p>{emptyMessage}</p> : undefined}
          label="Loading articles"
          loading={articles.isPending}
        >
          <ul aria-label={`${state} articles`} className="article-list">
            {articles.data?.items.map((article) => {
              const title = draftTitle(article);
              const online = article.publishedRevisionId !== null;
              const published = online;
              return (
                <li className="article-list-item" key={article.id}>
                  <div>
                    <h2><Link aria-label={title} to={`/articles/${article.id}/edit`}>{title}</Link></h2>
                    <p>Draft updated: {article.draftUpdatedAt}</p>
                    <p>Created: {article.createdAt}</p>
                    <p>State: {article.state}</p>
                    <p>{online ? "Online" : "Draft"}</p>
                  </div>
                  <div className="article-actions">
                    <button
                      aria-expanded={openActionsFor === article.id}
                      aria-label={`Actions for ${title}`}
                      className="touch-target"
                      onClick={() => setOpenActionsFor((current) => current === article.id ? undefined : article.id)}
                      type="button"
                    >
                      Actions
                    </button>
                    {openActionsFor === article.id && <div>
                      <Link className="touch-target" to={`/articles/${article.id}/edit`}>Edit</Link>
                      {article.state === "trashed" ? (
                        <button className="touch-target" onClick={() => setPendingDialog({ action: "restore", article })} type="button">Restore</button>
                      ) : (
                        <>
                          <button className="touch-target" disabled={published} onClick={() => setPendingDialog({ action: "trash", article })} type="button">Trash</button>
                          {published && <p>Unpublish before trashing this article.</p>}
                          {published && <button className="touch-target" onClick={() => setPendingDialog({ action: "unpublish", article })} type="button">Unpublish</button>}
                        </>
                      )}
                    </div>}
                  </div>
                </li>
              );
            })}
          </ul>
        </AsyncPage>
      )}

      {pendingDialog && dialogCopy && (
        <ConfirmDialog
          cancelDisabled={actionPending}
          confirmDisabled={actionPending}
          confirmLabel={dialogCopy.confirmLabel}
          onCancel={cancelAction}
          onConfirm={confirmAction}
          open
          title={dialogCopy.title}
        >
          <p>{dialogCopy.description}</p>
        </ConfirmDialog>
      )}
    </section>
  );
}
