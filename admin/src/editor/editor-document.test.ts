import { describe, expect, it } from "vitest";

import { articleDetail } from "../test/fixtures";
import {
  MAX_DOCUMENT_BYTES,
  fromArticleDetail,
  toSaveRequest,
  toggleTagId,
  validateEditorDocument,
  validateTagName,
  type EditorDocument,
} from "./editor-document";

const document: EditorDocument = {
  title: "Build log",
  summary: "Summary",
  coverMediaId: null,
  contentMd: "# Build log\n",
  tagIds: [31, 32],
};

describe("editor document", () => {
  it("converts the exact draft and extracts unique tag IDs by snapshot position", () => {
    const detail = {
      ...articleDetail,
      draft: {
        ...articleDetail.draft,
        tags: [
          { tagId: 32, name: "React", slug: "react", position: 2 },
          { tagId: 31, name: "Go", slug: "go", position: 0 },
          { tagId: 32, name: "React", slug: "react", position: 1 },
        ],
      },
    };

    expect(fromArticleDetail(detail)).toEqual(document);
  });

  it("constructs the exact SaveDraftRequest without tag names", () => {
    expect(toSaveRequest(document, 7)).toEqual({
      lockVersion: 7,
      title: "Build log",
      summary: "Summary",
      coverMediaId: null,
      contentMd: "# Build log\n",
      tagIds: [31, 32],
    });
  });

  it("keeps selected tag IDs ordered and unique and enforces the 32-tag cap", () => {
    expect(toggleTagId([31, 32], 31)).toEqual([32]);
    expect(toggleTagId([31, 32], 33)).toEqual([31, 32, 33]);
    expect(toggleTagId([31, 32], 32, true)).toEqual([31, 32]);
    const full = Array.from({ length: 32 }, (_, index) => index + 1);
    expect(toggleTagId(full, 33, true)).toEqual(full);
  });

  it("counts Unicode code points for title, summary, and tag-name limits", () => {
    expect(validateEditorDocument({ ...document, title: "😀".repeat(200), summary: "界".repeat(600) }, 7)).toEqual([]);
    expect(validateEditorDocument({ ...document, title: "😀".repeat(201) }, 7)).toContain("Title must be at most 200 characters.");
    expect(validateEditorDocument({ ...document, summary: "界".repeat(601) }, 7)).toContain("Summary must be at most 600 characters.");
    expect(validateTagName("界".repeat(64))).toBeUndefined();
    expect(validateTagName("")).toBe("Tag name must contain 1–64 characters.");
    expect(validateTagName("😀".repeat(65))).toBe("Tag name must contain 1–64 characters.");
  });

  it("allows an empty recoverable title but rejects invalid IDs, duplicates, and more than 32 tags", () => {
    expect(validateEditorDocument({ ...document, title: "" }, 7)).toEqual([]);
    expect(validateEditorDocument({ ...document, coverMediaId: 0, tagIds: [31, 31, -1] }, 0)).toEqual(expect.arrayContaining([
      "Lock version must be a positive integer.",
      "Cover media ID must be a positive integer or empty.",
      "Tag IDs must be positive, ordered, and unique.",
    ]));
    expect(validateEditorDocument({ ...document, tagIds: Array.from({ length: 33 }, (_, index) => index + 1) }, 7)).toContain("Select at most 32 tags.");
  });

  it("measures raw Markdown and the complete JSON request below the 2 MiB boundary", () => {
    expect(validateEditorDocument({ ...document, contentMd: "a".repeat(MAX_DOCUMENT_BYTES - 1) }, 7)).not.toContain("Markdown must be smaller than 2 MiB.");
    expect(validateEditorDocument({ ...document, contentMd: "a".repeat(MAX_DOCUMENT_BYTES) }, 7)).toContain("Markdown must be smaller than 2 MiB.");

    const jsonHeavy = { ...document, title: "t".repeat(200), summary: "s".repeat(600), contentMd: "a".repeat(MAX_DOCUMENT_BYTES - 1) };
    expect(validateEditorDocument(jsonHeavy, 7)).toContain("Draft request must be smaller than 2 MiB.");
  });

  it("gates blob URLs for version or publish while leaving draft text untouched", () => {
    const local = { ...document, contentMd: "![local](blob:https://admin.test/image)\n" };
    expect(validateEditorDocument(local, 7)).toEqual([]);
    expect(validateEditorDocument(local, 7, { rejectBlobUrls: true })).toContain("Upload local images before versioning or publishing.");
    expect(local.contentMd).toBe("![local](blob:https://admin.test/image)\n");
  });

  it("refuses to construct a request from invalid local input", () => {
    expect(() => toSaveRequest({ ...document, title: "x".repeat(201) }, 7)).toThrow("Title must be at most 200 characters.");
  });
});
