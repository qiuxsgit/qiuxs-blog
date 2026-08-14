import { useEffect, useRef } from "react";

import { createMilkdownEditor } from "./milkdown-adapter";

export interface MarkdownEditorProps {
  value: string;
  onChange(value: string): void;
}

export function MarkdownEditor({ onChange, value }: MarkdownEditorProps) {
  const root = useRef<HTMLDivElement>(null);
  const onChangeRef = useRef(onChange);

  useEffect(() => { onChangeRef.current = onChange; }, [onChange]);

  useEffect(() => {
    if (!root.current) return;
    const editor = createMilkdownEditor(root.current, value, (markdown) => onChangeRef.current(markdown));
    const creation = editor.create();
    void creation.catch(() => undefined);
    return () => {
      void creation.then(() => editor.destroy()).catch(() => undefined);
    };
  }, []);

  return <div aria-label="Markdown canvas" className="markdown-canvas" ref={root} />;
}
