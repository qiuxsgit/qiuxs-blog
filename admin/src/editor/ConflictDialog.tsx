import type { EditorDocument } from "./editor-document";
import type { ApiProblem } from "../api/problem";

export function ConflictDialog({ local, problem, onCopy, onReload }: { local: EditorDocument; problem?: ApiProblem | undefined; onCopy(): void; onReload(): void }) {
  return <dialog aria-labelledby="conflict-title" open>
    <h2 id="conflict-title">Version conflict</h2>
    <p>The article changed elsewhere. Copy your Markdown before reloading.</p>
    {problem && <p role="alert">{problem.title} ({problem.code}, {problem.requestId})</p>}
    <button className="editor-touch-target" onClick={onCopy} type="button">Copy local Markdown</button>
    <button className="editor-touch-target" onClick={onReload} type="button">Reload server draft</button>
  </dialog>;
}
