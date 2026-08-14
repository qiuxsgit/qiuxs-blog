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
  restoreVersionRequest,
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

  it("only allows version creation from a saved autosave state", () => {
    const saved: SaveState = { kind: "saved", lockVersion: 8, savedAt: new Date() };
    expect(canCreateVersion(saved)).toBe(true);
    expect(canCreateVersion({ kind: "dirty", lockVersion: 8 })).toBe(false);
    expect(canCreateVersion({ kind: "saving", lockVersion: 8 })).toBe(false);
    expect(canCreateVersion({ kind: "conflict", lockVersion: 8, local: {} as never })).toBe(false);
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
