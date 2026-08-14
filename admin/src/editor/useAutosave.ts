import { useCallback, useEffect, useRef, useState } from "react";
import { unstable_usePrompt as usePrompt } from "react-router-dom";
import type { ArticleDetail, DraftView, SaveDraftRequest } from "../api/admin-api";
import type { EntityId } from "../api/ids";
import { ApiProblem } from "../api/problem";
import { fromArticleDetail, toSaveRequest, validateEditorDocument, type EditorDocument } from "./editor-document";
import { operationProblem } from "./operation-problem";

export type SaveState =
  | { kind: "saved"; savedAt: Date; lockVersion: number }
  | { kind: "dirty"; lockVersion: number }
  | { kind: "saving"; lockVersion: number }
  | { kind: "failed"; lockVersion: number; problem: ApiProblem }
  | { kind: "conflict"; lockVersion: number; local: EditorDocument };

export interface AutosaveState {
  document: EditorDocument;
  generation: number;
  state: SaveState;
}

export type AutosaveAction =
  | { type: "edit"; document: EditorDocument }
  | { type: "start"; generation: number; lockVersion: number }
  | { type: "success"; generation: number; draft: DraftView; savedAt: Date }
  | { type: "failure"; generation: number; problem: ApiProblem }
  | { type: "conflict"; generation: number }
  | { type: "reload"; detail: ArticleDetail; savedAt: Date };

export function createAutosaveState(document: EditorDocument, lockVersion: number, savedAt = new Date()): AutosaveState {
  return { document, generation: 0, state: { kind: "saved", lockVersion, savedAt } };
}

function documentFromDraft(current: EditorDocument, draft: DraftView): EditorDocument {
  const seen = new Set<number>();
  const tagIds = [...draft.tags].sort((a, b) => a.position - b.position).flatMap((tag) => {
    if (seen.has(tag.tagId)) return [];
    seen.add(tag.tagId);
    return [tag.tagId];
  });
  return { title: draft.title, summary: draft.summary, coverMediaId: draft.coverMediaId, contentMd: draft.contentMd, tagIds };
}

export function autosaveReducer(current: AutosaveState, action: AutosaveAction): AutosaveState {
  switch (action.type) {
    case "edit":
      return { document: action.document, generation: current.generation + 1, state: { kind: "dirty", lockVersion: current.state.lockVersion } };
    case "start":
      return { ...current, state: { kind: "saving", lockVersion: action.lockVersion } };
    case "success": {
      const document = documentFromDraft(current.document, action.draft);
      if (action.generation !== current.generation) {
        return { document: current.document, generation: current.generation, state: { kind: "dirty", lockVersion: action.draft.lockVersion } };
      }
      return { document, generation: current.generation, state: { kind: "saved", lockVersion: action.draft.lockVersion, savedAt: action.savedAt } };
    }
    case "failure":
      return { ...current, state: { kind: "failed", lockVersion: current.state.lockVersion, problem: action.problem } };
    case "conflict":
      return { ...current, state: { kind: "conflict", lockVersion: current.state.lockVersion, local: current.document } };
    case "reload":
      return { document: fromArticleDetail(action.detail), generation: current.generation + 1, state: { kind: "saved", lockVersion: action.detail.draft.lockVersion, savedAt: action.savedAt } };
  }
}

export function isRevisionConflict(error: unknown): error is ApiProblem {
  return error instanceof ApiProblem && error.status === 409 && error.code === "revision_conflict";
}

export function shouldBlockNavigation(state: SaveState): boolean {
  return state.kind !== "saved";
}

export function copyLocalMarkdown(document: EditorDocument): string {
  return document.contentMd;
}

export interface AutosaveOptions {
  articleId: EntityId;
  initial: EditorDocument;
  initialLockVersion: number;
  delayMs: number;
  save(input: SaveDraftRequest, signal?: AbortSignal): Promise<DraftView>;
  reload(signal?: AbortSignal): Promise<ArticleDetail>;
}

export function useAutosave(options: AutosaveOptions) {
  const [machine, setMachine] = useState(() => createAutosaveState(options.initial, options.initialLockVersion));
  const machineRef = useRef(machine);
  const mounted = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const inFlight = useRef(false);
  const abort = useRef<AbortController | undefined>(undefined);
  const initialKey = `${options.articleId}:${options.initialLockVersion}:${options.initial.contentMd}`;
  usePrompt({ when: shouldBlockNavigation(machine.state), message: "You have unsaved changes. Leave this page?" });

  const update = useCallback((action: AutosaveAction) => {
    setMachine((current) => {
      const next = autosaveReducer(current, action);
      machineRef.current = next;
      return next;
    });
  }, []);

  useEffect(() => {
    machineRef.current = createAutosaveState(options.initial, options.initialLockVersion);
    setMachine(machineRef.current);
  // Initial data changes only when navigating to another article.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialKey]);

  const run = useCallback(() => {
    if (!mounted.current || inFlight.current) return;
    const current = machineRef.current;
    if (current.state.kind === "saved" || current.state.kind === "saving" || current.state.kind === "conflict") return;
    const errors = validateEditorDocument(current.document, current.state.lockVersion);
    if (errors.length > 0) {
      update({ type: "failure", generation: current.generation, problem: new ApiProblem(422, "invalid_draft", "client", errors[0] ?? "Invalid draft") });
      return;
    }
    const generation = current.generation;
    const lockVersion = current.state.lockVersion;
    const controller = new AbortController();
    abort.current = controller;
    inFlight.current = true;
    update({ type: "start", generation, lockVersion });
    void options.save(toSaveRequest(current.document, lockVersion), controller.signal).then((draft) => {
      if (!mounted.current) return;
      update({ type: "success", generation, draft, savedAt: new Date() });
    }).catch((error: unknown) => {
      if (!mounted.current || error instanceof DOMException && error.name === "AbortError") return;
      if (isRevisionConflict(error)) update({ type: "conflict", generation });
      else update({ type: "failure", generation, problem: operationProblem(error, "Unable to save draft", "save_draft_failed") });
    }).finally(() => {
      inFlight.current = false;
      abort.current = undefined;
      if (!mounted.current) return;
      const latest = machineRef.current;
      if (latest.state.kind === "dirty" && latest.generation !== generation) {
        timer.current = setTimeout(run, 0);
      }
    });
  }, [options, update]);

  const edit = useCallback((document: EditorDocument) => {
    update({ type: "edit", document });
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(run, options.delayMs);
  }, [options.delayMs, run, update]);

  const retry = useCallback(() => {
    if (inFlight.current) return;
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(run, 0);
  }, [run]);

  const reload = useCallback(async () => {
    try {
      const detail = await options.reload(abort.current?.signal);
      if (mounted.current) update({ type: "reload", detail, savedAt: new Date() });
      return detail;
    } catch (error) {
      if (mounted.current) update({ type: "failure", generation: machineRef.current.generation, problem: operationProblem(error, "Unable to reload article", "reload_article_failed") });
      throw error;
    }
  }, [options, update]);

  useEffect(() => {
    mounted.current = true;
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!shouldBlockNavigation(machineRef.current.state)) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      mounted.current = false;
      if (timer.current) clearTimeout(timer.current);
      abort.current?.abort();
      window.removeEventListener("beforeunload", onBeforeUnload);
    };
  }, []);

  return { document: machine.document, state: machine.state, edit, retry, reload, copyMarkdown: () => copyLocalMarkdown(machine.document), canPublish: machine.state.kind === "saved", canPreview: machine.state.kind === "saved", canVersion: machine.state.kind === "saved" };
}
