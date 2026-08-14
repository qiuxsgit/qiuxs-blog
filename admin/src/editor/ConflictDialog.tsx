import type { EditorDocument } from "./editor-document";

export function ConflictDialog({ local, onCopy, onReload }: { local: EditorDocument; onCopy(): void; onReload(): void }) {
  return <dialog aria-labelledby="conflict-title" open>
    <h2 id="conflict-title">Version conflict</h2>
    <p>The article changed elsewhere. Copy your Markdown before reloading.</p>
    <button className="editor-touch-target" onClick={onCopy} type="button">Copy local Markdown</button>
    <button className="editor-touch-target" onClick={onReload} type="button">Reload server draft</button>
  </dialog>;
}
