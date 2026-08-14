import { describe, expect, it } from "vitest";

import {
  initialMarkdownUpdateState,
  markExactPasteDocument,
  reconcileMarkdownUpdate,
  stopMarkdownUpdates,
} from "./markdown-update-state";

describe("markdown update serialization state", () => {
  it("does not emit a queued local document that differs from the authoritative current document", () => {
    const result = reconcileMarkdownUpdate(
      initialMarkdownUpdateState,
      "# Local Initial\n",
      "# Server\n",
    );

    expect(result).toEqual({ state: initialMarkdownUpdateState });
  });

  it("suppresses only the matching normalized exact-paste callback", () => {
    const pasted = markExactPasteDocument(initialMarkdownUpdateState, "# Pasted\n");

    expect(reconcileMarkdownUpdate(pasted, "# Pasted\n", "# Pasted\n")).toEqual({
      state: initialMarkdownUpdateState,
    });
  });

  it("emits a newer edit and clears the pending exact-paste document", () => {
    const pasted = markExactPasteDocument(initialMarkdownUpdateState, "# Pasted\n");

    expect(reconcileMarkdownUpdate(pasted, "# Local Pasted\n", "# Local Pasted\n")).toEqual({
      state: initialMarkdownUpdateState,
      markdown: "# Local Pasted\n",
    });
  });

  it("does not emit after editor teardown", () => {
    const stopped = stopMarkdownUpdates();

    expect(reconcileMarkdownUpdate(stopped, "# Late\n", "# Late\n")).toEqual({ state: stopped });
  });
});
