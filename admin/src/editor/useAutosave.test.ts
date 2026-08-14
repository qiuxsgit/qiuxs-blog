import { describe, expect, it } from "vitest";
import { ApiProblem } from "../api/problem";
import { articleDetail, draftView } from "../test/fixtures";
import { autosaveInitialKey, createAutosaveState, autosaveReducer, copyLocalMarkdown, conflictReloadDecision, isAbortError, isCurrentAutosaveEpoch, isRevisionConflict, nextAutosaveDelay, reduceForAutosaveEpoch, reduceReloadForAutosaveEpoch, shouldBlockNavigation, type SaveState } from "./useAutosave";
import type { EditorDocument } from "./editor-document";

const local: EditorDocument = { title: "Draft", summary: "", coverMediaId: null, contentMd: "# local\n", tagIds: [31] };

describe("autosave state machine", () => {
  it("keeps initial data saved and marks each edit dirty", () => {
    const initial = createAutosaveState(local, 7, new Date("2026-01-01T00:00:00Z"));
    expect(initial.state.kind).toBe("saved");
    const dirty = autosaveReducer(initial, { type: "edit", document: { ...local, contentMd: "changed" } });
    expect(dirty.generation).toBe(1);
    expect(dirty.state).toEqual({ kind: "dirty", lockVersion: 7 });
    expect(shouldBlockNavigation(dirty.state)).toBe(true);
  });

  it("changes the load generation when metadata changes even if Markdown is unchanged", () => {
    expect(autosaveInitialKey(11, 7, local)).not.toBe(autosaveInitialKey(11, 7, { ...local, title: "other" }));
    expect(autosaveInitialKey(11, 7, local)).not.toBe(autosaveInitialKey(11, 7, { ...local, tagIds: [32] }));
  });

  it("keeps invalid local input dirty with validation errors and no save delay", () => {
    const invalid = autosaveReducer(createAutosaveState(local, 7), { type: "edit", document: { ...local, title: "x".repeat(201) } });
    const checked = autosaveReducer(invalid, { type: "invalid", errors: ["Title must be at most 200 characters."] });
    expect(checked.state.kind).toBe("invalid");
    expect((checked.state as Extract<SaveState, { kind: "invalid" }>).errors).toHaveLength(1);
    expect(nextAutosaveDelay(checked.state, 2000, 2000)).toBeNull();
  });

  it("does not reload until explicitly confirmed and keeps conflict on reload failure", () => {
    expect(conflictReloadDecision(false)).toBe("stay");
    expect(conflictReloadDecision(true)).toBe("reload");
    const conflict = autosaveReducer(createAutosaveState(local, 7), { type: "conflict", generation: 0 });
    const failed = autosaveReducer(conflict, { type: "reload_failure", problem: new ApiProblem(503, "network_error", "client", "Reload failed") });
    expect(failed.state.kind).toBe("conflict");
    expect((failed.state as Extract<SaveState, { kind: "conflict" }>).problem?.title).toBe("Reload failed");
  });

  it("recognizes browser and adapter AbortErrors without class-specific checks", () => {
    expect(isAbortError(new DOMException("cancelled", "AbortError"))).toBe(true);
    expect(isAbortError({ name: "AbortError" })).toBe(true);
    expect(isAbortError(new Error("cancelled"))).toBe(false);
    expect(isAbortError({ name: "NetworkError" })).toBe(false);
    expect(isAbortError(null)).toBe(false);
  });

  it("enforces the exact debounce boundary and schedules newer work after settlement", () => {
    const dirty = autosaveReducer(createAutosaveState(local, 7), { type: "edit", document: { ...local, contentMd: "new" } });
    expect(nextAutosaveDelay(dirty.state, 1999, 2000)).toBe(1);
    expect(nextAutosaveDelay(dirty.state, 2000, 2000)).toBe(0);
    expect(nextAutosaveDelay(dirty.state, 0, 2000)).toBe(2000);
    const saving = autosaveReducer(dirty, { type: "start", generation: 1, lockVersion: 7 });
    const newer = autosaveReducer(saving, { type: "edit", document: { ...local, contentMd: "newer" } });
    const settled = autosaveReducer(newer, { type: "success", generation: 1, draft: { ...draftView, lockVersion: 8 }, savedAt: new Date() });
    expect(nextAutosaveDelay(settled.state, 0, 2000)).toBe(0);
  });

  it("ignores old article save callbacks after switching articles", () => {
    const oldState = createAutosaveState(local, 7);
    const newDocument = { ...local, title: "New article", contentMd: "# new" };
    const newState = createAutosaveState(newDocument, 3);
    expect(isCurrentAutosaveEpoch(1, 2)).toBe(false);
    expect(reduceForAutosaveEpoch(newState, 1, 2, { type: "success", generation: 1, draft: { ...draftView, title: "OLD", contentMd: "old", lockVersion: 99 }, savedAt: new Date() })).toEqual(newState);
    expect(reduceForAutosaveEpoch(newState, 1, 2, { type: "failure", generation: 1, problem: new ApiProblem(503, "old", "old", "old") })).toEqual(newState);
    expect(oldState.document.title).toBe("Draft");
  });

  it("ignores old reload success/failure after switching articles but accepts the new reload", () => {
    const oldState = createAutosaveState(local, 7);
    const newState = createAutosaveState({ ...local, title: "New article" }, 3);
    expect(reduceReloadForAutosaveEpoch(newState, 1, 2, { type: "reload", detail: articleDetail, savedAt: new Date() })).toEqual(newState);
    expect(reduceReloadForAutosaveEpoch(newState, 1, 2, { type: "reload_failure", problem: new ApiProblem(503, "old", "old", "old") })).toEqual(newState);
    const accepted = reduceReloadForAutosaveEpoch(newState, 2, 2, { type: "reload", detail: { ...articleDetail, draft: { ...articleDetail.draft, title: "Fresh" } }, savedAt: new Date() });
    expect(accepted.document.title).toBe("Fresh");
    expect(oldState.document.title).toBe("Draft");
  });

  it("serializes a response and adopts its lock version", () => {
    const initial = createAutosaveState(local, 7);
    const dirty = autosaveReducer(initial, { type: "edit", document: local });
    const saving = autosaveReducer(dirty, { type: "start", generation: 1, lockVersion: 7 });
    const savedAt = new Date("2026-01-01T00:00:02Z");
    const saved = autosaveReducer(saving, { type: "success", generation: 1, draft: { ...draftView, title: "Draft", contentMd: "# saved\n", lockVersion: 8 }, savedAt });
    expect(saved.state).toEqual({ kind: "saved", lockVersion: 8, savedAt });
    expect(saved.document.contentMd).toBe("# saved\n");
    expect(shouldBlockNavigation(saved.state)).toBe(false);
  });

  it("never overwrites a newer local generation with a stale response", () => {
    const initial = createAutosaveState(local, 7);
    const saving = autosaveReducer(autosaveReducer(initial, { type: "edit", document: local }), { type: "start", generation: 1, lockVersion: 7 });
    const newer = autosaveReducer(saving, { type: "edit", document: { ...local, contentMd: "# newer\n" } });
    const settled = autosaveReducer(newer, { type: "success", generation: 1, draft: { ...draftView, contentMd: "# stale\n", lockVersion: 8 }, savedAt: new Date() });
    expect(settled.document.contentMd).toBe("# newer\n");
    expect(settled.state).toEqual({ kind: "dirty", lockVersion: 8, immediate: true });
  });

  it("distinguishes exact revision conflicts and keeps local markdown copy-only", () => {
    const conflict = new ApiProblem(409, "revision_conflict", "req-1", "Conflict");
    expect(isRevisionConflict(conflict)).toBe(true);
    expect(isRevisionConflict(new ApiProblem(409, "other", "req-1", "Conflict"))).toBe(false);
    const state = autosaveReducer(createAutosaveState(local, 7), { type: "conflict", generation: 0 });
    expect(state.state.kind).toBe("conflict");
    expect(copyLocalMarkdown(local)).toBe("# local\n");
  });

  it("retains a failure for retry and replaces every field on confirmed reload", () => {
    const initial = createAutosaveState(local, 7);
    const failed = autosaveReducer(initial, { type: "failure", generation: 0, problem: new ApiProblem(503, "network_error", "client", "Network request failed") });
    expect(failed.state.kind).toBe("failed");
    const reloaded = autosaveReducer(failed, { type: "reload", detail: { ...articleDetail, draft: { ...articleDetail.draft, lockVersion: 12, title: "Reloaded", summary: "new", contentMd: "# reload", tags: [] } }, savedAt: new Date("2026-01-01T00:00:03Z") });
    expect(reloaded.document).toMatchObject({ title: "Reloaded", summary: "new", contentMd: "# reload", tagIds: [] });
    expect(reloaded.state.kind).toBe("saved");
    expect((reloaded.state as Extract<SaveState, { kind: "saved" }>).lockVersion).toBe(12);
  });
});
