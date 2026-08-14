import { describe, expect, it } from "vitest";

import type { ArticleDetail, DraftView, RevisionView, RevisionList, VersionResult } from "../api/admin-api";
import type { SaveState } from "../editor/useAutosave";
import { articleDetail } from "../test/fixtures";
import {
  canCreateVersion,
  createVersionRequest,
  mapRevisionRows,
  reasonLabel,
  replaceArticleDraft,
  restoreVersionDecision,
  restoreVersionRequest,
  versionCapabilityFromAutosave,
  versionResultDraft,
} from "./version-model";

const revision: RevisionView = {
  id: 17, articleId: 9, revisionNo: 4, lockVersion: 8, status: "frozen", reason: "manual_version",
  title: "Saved title", summary: "Saved summary", coverMediaId: null, contentMd: "# saved",
  contentHash: "hash", tags: [], media: [], createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
};

const draft: DraftView = { ...articleDetail.draft, id: 18, revisionNo: 5, lockVersion: 9, title: "Restored" };

describe("version model", () => {
  it("builds exact lock requests and rejects invalid IDs", () => {
    expect(createVersionRequest(8)).toEqual({ lockVersion: 8 });
    expect(restoreVersionRequest(9)).toEqual({ lockVersion: 9 });
    expect(() => createVersionRequest(0)).toThrow();
    expect(() => restoreVersionRequest(Number.MAX_SAFE_INTEGER + 1)).toThrow();
  });

  it("only allows version creation from a saved, titled, non-blob autosave snapshot", () => {
    const kinds: SaveState["kind"][] = ["dirty", "saving", "invalid", "failed", "conflict"];
    const saved: SaveState = { kind: "saved", lockVersion: 8, savedAt: new Date() };
    const capability = versionCapabilityFromAutosave(saved, { title: "Title", summary: "", coverMediaId: null, contentMd: "# body", tagIds: [] });
    expect(canCreateVersion(capability, 8)).toBe(true);
    for (const kind of kinds) expect(canCreateVersion({ ...capability, kind }, 8)).toBe(false);
    expect(canCreateVersion({ ...capability, title: "   " }, 8)).toBe(false);
    expect(canCreateVersion({ ...capability, contentMd: "![x](blob:https://local)" }, 8)).toBe(false);
    expect(canCreateVersion(capability, 9)).toBe(false);
  });

  it("requires explicit restore confirmation", () => {
    expect(restoreVersionDecision(false, 17)).toBeUndefined();
    expect(restoreVersionDecision(true, 17)).toBe(17);
    expect(() => restoreVersionDecision(true, 0)).toThrow();
  });

  it("preserves delivered ordering and every revision field", () => {
    const list: RevisionList = { items: [revision, { ...revision, id: 16, reason: "publish_snapshot" }] };
    const rows = mapRevisionRows(list.items);
    expect(rows.map((row) => row.id)).toEqual([17, 16]);
    expect(rows[0]).toEqual(revision);
    expect(() => mapRevisionRows([{ ...revision, id: 0 }])).toThrow();
  });

  it("uses stable labels for revision reasons", () => {
    expect(reasonLabel("manual_version")).toBe("Manual version");
    expect(reasonLabel("publish_snapshot")).toBe("Publish snapshot");
  });

  it("extracts only the replacement draft from a version result", () => {
    const result: VersionResult = { version: revision, draft };
    expect(versionResultDraft(result)).toBe(draft);
  });

  it("replaces the detail draft immutably and advances its draft pointer", () => {
    const next = replaceArticleDraft(articleDetail, draft);
    expect(next).not.toBe(articleDetail);
    expect(next.draft).toBe(draft);
    expect(next.draftRevisionId).toBe(draft.id);
    expect(articleDetail.draft).not.toBe(draft);
  });
});
