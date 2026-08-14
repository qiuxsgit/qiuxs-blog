import { describe, expect, it } from "vitest";
import { articleDetail } from "../test/fixtures";
import { fromArticleDetail, canPublishArticle, type EditorDocument } from "./editor-document";

const saved = { kind: "saved" as const, lockVersion: 7 };
const draft = fromArticleDetail(articleDetail);
function document(overrides: Partial<EditorDocument> = {}): EditorDocument { return { ...draft, ...overrides }; }

describe("canPublishArticle", () => {
  it.each([
    ["saved valid", saved, document(), true],
    ["dirty", { kind: "dirty" as const, lockVersion: 7 }, document(), false],
    ["saving", { kind: "saving" as const, lockVersion: 7 }, document(), false],
    ["blank title", saved, document({ title: "  \n" }), false],
    ["blob URL", saved, document({ contentMd: "![x](BLOB:https://local)" }), false],
    ["oversized title", saved, document({ title: "x".repeat(201) }), false],
  ])("requires a publishable saved draft: %s", (_name, state, value, expected) => {
    expect(canPublishArticle(value, state)).toBe(expected);
  });
});
