import { useEffect, useRef, useState } from "react";

import { createMilkdownEditor, replaceMilkdownMarkdown } from "./milkdown-adapter";
import { useEditorImageUpload } from "../media/useEditorImageUpload";

export interface MarkdownEditorProps {
  value: string;
  onChange(value: string): void;
}

export function MarkdownEditor({ onChange, value }: MarkdownEditorProps) {
  const root = useRef<HTMLDivElement>(null);
  const onChangeRef = useRef(onChange);
  const editorRef = useRef<ReturnType<typeof createMilkdownEditor> | undefined>(undefined);
  const creationRef = useRef<ReturnType<ReturnType<typeof createMilkdownEditor>["create"]> | undefined>(undefined);
  const visualValueRef = useRef(value);
  const [attempt, setAttempt] = useState(0);
  const [failed, setFailed] = useState(false);
  const imageUpload = useEditorImageUpload({
    getMarkdown: () => visualValueRef.current,
    getInsertionOffset: () => visualValueRef.current.length,
    onInsert: (markdown) => onChangeRef.current(markdown),
  });

  useEffect(() => { onChangeRef.current = onChange; }, [onChange]);

  useEffect(() => {
    if (!root.current) return;
    setFailed(false);
    const editor = createMilkdownEditor(root.current, value, (markdown) => {
      visualValueRef.current = markdown;
      onChangeRef.current(markdown);
    });
    editorRef.current = editor;
    const creation = editor.create();
    creationRef.current = creation;
    void creation.catch(() => setFailed(true));
    return () => {
      if (editorRef.current === editor) editorRef.current = undefined;
      if (creationRef.current === creation) creationRef.current = undefined;
      void creation.then(() => editor.destroy()).catch(() => undefined);
    };
  }, [attempt]);

  useEffect(() => {
    if (visualValueRef.current === value) return;
    visualValueRef.current = value;
    const editor = editorRef.current;
    const creation = creationRef.current;
    if (!editor || !creation) return;
    void creation.then(() => {
      if (editorRef.current === editor) replaceMilkdownMarkdown(editor, value);
    }).catch(() => {
      if (editorRef.current === editor) setFailed(true);
    });
  }, [value]);

  return <>
    {failed && <div role="alert">
      <p>Unable to open visual editor. Switch to source mode or retry.</p>
      <button className="editor-touch-target" onClick={() => setAttempt((current) => current + 1)} type="button">Retry visual editor</button>
    </div>}
    <div aria-label="Markdown canvas" className="markdown-canvas" ref={root} onPaste={(event) => {
      const file = [...event.clipboardData.files].find((candidate) => candidate.type.startsWith("image/"));
      if (file) { event.preventDefault(); void imageUpload.upload(file); }
    }} onDrop={(event) => {
      const file = [...event.dataTransfer.files].find((candidate) => candidate.type.startsWith("image/"));
      if (file) { event.preventDefault(); void imageUpload.upload(file); }
    }} onDragOver={(event) => event.preventDefault()} />
    <label className="editor-touch-target">Add image<input accept="image/jpeg,image/png,image/webp,image/gif" hidden type="file" onChange={(event) => {
      const file = event.currentTarget.files?.[0];
      event.currentTarget.value = "";
      if (file) void imageUpload.upload(file);
    }} /></label>
    {imageUpload.uploading && <p role="status">Uploading image: {imageUpload.progress}%</p>}
    {imageUpload.error && <p role="alert">{imageUpload.error}</p>}
    {imageUpload.uploading && <button className="editor-touch-target" onClick={imageUpload.cancel} type="button">Cancel upload</button>}
  </>;
}
