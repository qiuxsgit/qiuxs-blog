import type { SaveState } from "../editor/useAutosave";

export function SaveIndicator({ state }: { state: SaveState }) {
  const labels = {
    saved: ["✓", "Saved"],
    dirty: ["●", "Unsaved changes"],
    saving: ["◌", "Saving changes"],
    failed: ["⚠", "Save failed"],
    conflict: ["⚠", "Version conflict"],
  } as const;
  const [icon, label] = labels[state.kind];
  return <p aria-label={label} aria-live="polite" className={`save-indicator save-${state.kind}`} role="status"><span aria-hidden="true">{icon}</span> {label}{state.kind === "saved" ? ` · ${state.savedAt.toLocaleTimeString()}` : ""}</p>;
}
