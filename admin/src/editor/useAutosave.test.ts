import { describe, expect, it } from "vitest";
import { ApiProblem } from "../api/problem";
import { articleDetail, draftView } from "../test/fixtures";
import { createAutosaveState, autosaveReducer, copyLocalMarkdown, isRevisionConflict, shouldBlockNavigation, type SaveState } from "./useAutosave";
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
    expect(settled.state).toEqual({ kind: "dirty", lockVersion: 8 });
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
