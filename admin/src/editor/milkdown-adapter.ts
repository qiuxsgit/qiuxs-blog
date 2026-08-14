import {
  defaultValueCtx,
  Editor,
  parserCtx,
  rootCtx,
  serializerCtx,
  type Editor as MilkdownEditor,
} from "@milkdown/kit/core";
import type { MilkdownPlugin } from "@milkdown/kit/ctx";
import { listener, listenerCtx } from "@milkdown/kit/plugin/listener";
import { commonmark } from "@milkdown/kit/preset/commonmark";
import { gfm } from "@milkdown/kit/preset/gfm";
import { Slice } from "@milkdown/kit/prose/model";
import { Plugin } from "@milkdown/kit/prose/state";
import { $prose } from "@milkdown/kit/utils";

export interface WholeDocumentPasteInput {
  currentMarkdown: string;
  html: string;
  plainText: string;
}

export function wholeDocumentPlainTextPaste({
  currentMarkdown,
  html,
  plainText,
}: WholeDocumentPasteInput): string | undefined {
  if (currentMarkdown.trim().length !== 0 || html.length !== 0 || plainText.length === 0) return undefined;
  return plainText;
}

function supported(plugin: MilkdownPlugin): boolean {
  const group = plugin.meta?.group?.toLowerCase();
  return group !== "html" && group !== "footnote";
}

export const articleMarkdownPlugins = [
  ...commonmark.filter(supported),
  ...gfm.filter(supported),
];

function wholeDocumentPastePlugin(onExactPaste: (markdown: string) => void) {
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
        onExactPaste(markdown);
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
  let exactPaste: string | undefined;
  return Editor.make()
    .config((ctx) => {
      ctx.set(rootCtx, root);
      ctx.set(defaultValueCtx, markdown);
      ctx.get(listenerCtx).markdownUpdated((_ctx, nextMarkdown) => {
        if (exactPaste !== undefined) {
          exactPaste = undefined;
          return;
        }
        onMarkdownChange(nextMarkdown);
      });
    })
    .use(articleMarkdownPlugins)
    .use(listener)
    .use(wholeDocumentPastePlugin((pasted) => {
      exactPaste = pasted;
      onMarkdownChange(pasted);
    }));
}
