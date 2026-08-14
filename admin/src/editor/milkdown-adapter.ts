import {
  defaultValueCtx,
  Editor,
  editorViewOptionsCtx,
  editorViewCtx,
  parserCtx,
  rootCtx,
  serializerCtx,
  type Editor as MilkdownEditor,
} from "@milkdown/kit/core";
import type { MilkdownPlugin } from "@milkdown/kit/ctx";
import { listener, listenerCtx } from "@milkdown/kit/plugin/listener";
import {
  blockquoteAttr,
  blockquoteSchema,
  bulletListAttr,
  bulletListSchema,
  codeBlockAttr,
  codeBlockSchema,
  commands as commonmarkCommands,
  docSchema,
  emphasisAttr,
  emphasisSchema,
  hardbreakAttr,
  hardbreakClearMarkPlugin,
  hardbreakFilterNodes,
  hardbreakFilterPlugin,
  hardbreakSchema,
  headingAttr,
  headingIdGenerator,
  headingSchema,
  hrAttr,
  hrSchema,
  imageAttr,
  imageSchema,
  inlineCodeAttr,
  inlineCodeSchema,
  inlineNodesCursorPlugin,
  inputRules as commonmarkInputRules,
  keymap as commonmarkKeymap,
  linkAttr,
  linkSchema,
  listItemAttr,
  listItemSchema,
  markInputRules as commonmarkMarkInputRules,
  orderedListAttr,
  orderedListSchema,
  paragraphAttr,
  paragraphSchema,
  remarkAddOrderInListPlugin,
  remarkInlineLinkPlugin,
  remarkLineBreak,
  remarkMarker,
  remarkPreserveEmptyLinePlugin,
  strongAttr,
  strongSchema,
  syncHeadingIdPlugin,
  syncListOrderPlugin,
  textSchema,
} from "@milkdown/kit/preset/commonmark";
import {
  autoInsertSpanPlugin,
  commands as gfmCommands,
  extendListItemSchemaForTask,
  inputRules as gfmInputRules,
  keepTableAlignPlugin,
  keymap as gfmKeymap,
  markInputRules as gfmMarkInputRules,
  pasteRules as gfmPasteRules,
  strikethroughAttr,
  strikethroughSchema,
  tableCellSchema,
  tableEditingPlugin,
  tableHeaderRowSchema,
  tableHeaderSchema,
  tableRowSchema,
  tableSchema,
} from "@milkdown/kit/preset/gfm";
import { Slice } from "@milkdown/kit/prose/model";
import { Plugin } from "@milkdown/kit/prose/state";
import { $prose, $remark } from "@milkdown/kit/utils";
import {
  gfmStrikethroughFromMarkdown,
  gfmStrikethroughToMarkdown,
} from "mdast-util-gfm-strikethrough";
import { gfmTableFromMarkdown, gfmTableToMarkdown } from "mdast-util-gfm-table";
import {
  gfmTaskListItemFromMarkdown,
  gfmTaskListItemToMarkdown,
} from "mdast-util-gfm-task-list-item";
import type { Root } from "mdast";
import { gfmStrikethrough } from "micromark-extension-gfm-strikethrough";
import { gfmTable } from "micromark-extension-gfm-table";
import { gfmTaskListItem } from "micromark-extension-gfm-task-list-item";
import type { Plugin as UnifiedPlugin } from "unified";

import {
  initialMarkdownUpdateState,
  markExactPasteDocument,
  reconcileMarkdownUpdate,
  stopMarkdownUpdates,
} from "./markdown-update-state";

export interface WholeDocumentPasteInput {
  currentMarkdown: string;
  html: string;
  plainText: string;
}

export function wholeDocumentPlainTextPaste({
  currentMarkdown,
  plainText,
}: WholeDocumentPasteInput): string | undefined {
  if (currentMarkdown.trim().length !== 0 || plainText.length === 0) return undefined;
  return plainText;
}

const selectiveGfmRemark = $remark("selectiveGfm", () => {
  const plugin: UnifiedPlugin<[], Root> = function selectiveGfm() {
    const data = this.data();
    const micromarkExtensions = data.micromarkExtensions || (data.micromarkExtensions = []);
    const fromMarkdownExtensions = data.fromMarkdownExtensions || (data.fromMarkdownExtensions = []);
    const toMarkdownExtensions = data.toMarkdownExtensions || (data.toMarkdownExtensions = []);
    micromarkExtensions.push(gfmTable(), gfmTaskListItem(), gfmStrikethrough());
    fromMarkdownExtensions.push(
      gfmTableFromMarkdown(),
      gfmTaskListItemFromMarkdown(),
      gfmStrikethroughFromMarkdown(),
    );
    toMarkdownExtensions.push(
      gfmTableToMarkdown(),
      gfmTaskListItemToMarkdown(),
      gfmStrikethroughToMarkdown(),
    );
  };
  return plugin;
});

export const articleMarkdownPlugins: MilkdownPlugin[] = [
  docSchema,
  paragraphAttr,
  paragraphSchema,
  headingIdGenerator,
  headingAttr,
  headingSchema,
  hardbreakAttr,
  hardbreakSchema,
  blockquoteAttr,
  blockquoteSchema,
  codeBlockAttr,
  codeBlockSchema,
  hrAttr,
  hrSchema,
  imageAttr,
  imageSchema,
  bulletListAttr,
  bulletListSchema,
  orderedListAttr,
  orderedListSchema,
  listItemAttr,
  listItemSchema,
  emphasisAttr,
  emphasisSchema,
  strongAttr,
  strongSchema,
  inlineCodeAttr,
  inlineCodeSchema,
  linkAttr,
  linkSchema,
  textSchema,
  commonmarkInputRules,
  commonmarkMarkInputRules,
  commonmarkCommands,
  commonmarkKeymap,
  hardbreakClearMarkPlugin,
  hardbreakFilterNodes,
  hardbreakFilterPlugin,
  inlineNodesCursorPlugin,
  remarkAddOrderInListPlugin,
  remarkInlineLinkPlugin,
  remarkLineBreak,
  remarkMarker,
  remarkPreserveEmptyLinePlugin,
  syncHeadingIdPlugin,
  syncListOrderPlugin,
  extendListItemSchemaForTask,
  tableSchema,
  tableHeaderRowSchema,
  tableRowSchema,
  tableHeaderSchema,
  tableCellSchema,
  strikethroughAttr,
  strikethroughSchema,
  gfmInputRules,
  gfmPasteRules,
  gfmMarkInputRules,
  gfmKeymap,
  gfmCommands,
  keepTableAlignPlugin,
  autoInsertSpanPlugin,
  selectiveGfmRemark,
  tableEditingPlugin,
].flat();

function wholeDocumentPastePlugin(
  onExactPaste: (markdown: string, serializedMarkdown: string) => void,
) {
  return $prose((ctx) => new Plugin({
    props: {
      handlePaste: (view, event) => {
        const clipboard = event.clipboardData;
        if (!clipboard) return false;
        const serializer = ctx.get(serializerCtx);
        const markdown = wholeDocumentPlainTextPaste({
          currentMarkdown: serializer(view.state.doc),
          html: clipboard.getData("text/html"),
          plainText: clipboard.getData("text/plain"),
        });
        if (markdown === undefined) return false;
        const parsed = ctx.get(parserCtx)(markdown);
        if (!parsed || typeof parsed === "string") return false;
        view.dispatch(view.state.tr.replace(0, view.state.doc.content.size, new Slice(parsed.content, 0, 0)));
        onExactPaste(markdown, serializer(view.state.doc));
        return true;
      },
    },
  }));
}

export function createMilkdownEditor(
  root: HTMLElement,
  markdown: string,
  onMarkdownChange: (markdown: string) => void,
): MilkdownEditor {
  let updateState = initialMarkdownUpdateState;
  return Editor.make()
    .config((ctx) => {
      ctx.set(rootCtx, root);
      ctx.set(defaultValueCtx, markdown);
      ctx.update(editorViewOptionsCtx, (current) => ({
        ...current,
        attributes: { "aria-label": "Article Markdown" },
      }));
      const listeners = ctx.get(listenerCtx);
      listeners.destroy(() => { updateState = stopMarkdownUpdates(); });
      listeners.updated((listenerContext, nextDocument) => {
        if (updateState.stopped) return;
        const serializer = listenerContext.get(serializerCtx);
        const nextMarkdown = serializer(nextDocument);
        const currentMarkdown = serializer(listenerContext.get(editorViewCtx).state.doc);
        const decision = reconcileMarkdownUpdate(updateState, nextMarkdown, currentMarkdown);
        updateState = decision.state;
        if (decision.markdown !== undefined) onMarkdownChange(decision.markdown);
      });
    })
    .use(articleMarkdownPlugins)
    .use(listener)
    .use(wholeDocumentPastePlugin((pasted, serializedMarkdown) => {
      updateState = markExactPasteDocument(updateState, serializedMarkdown);
      onMarkdownChange(pasted);
    }));
}

export function replaceMilkdownMarkdown(editor: MilkdownEditor, markdown: string): void {
  editor.action((ctx) => {
    const parsed = ctx.get(parserCtx)(markdown);
    if (!parsed || typeof parsed === "string") throw new Error("Unsupported Markdown");
    const view = ctx.get(editorViewCtx);
    view.dispatch(
      view.state.tr
        .replace(0, view.state.doc.content.size, new Slice(parsed.content, 0, 0))
        .setMeta("addToHistory", false),
    );
  });
}
