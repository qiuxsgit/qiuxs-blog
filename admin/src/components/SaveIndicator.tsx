type SaveState = "idle" | "saved" | "saving" | "error";

const labels: Record<SaveState, readonly [string, string]> = {
  idle: ["", ""],
  saving: ["◌", "Saving changes"],
  saved: ["✓", "All changes saved"],
  error: ["⚠", "Unable to save changes"],
};

export function SaveIndicator({ state }: { state: SaveState }) {
  const [icon, label] = labels[state];
  if (!label) return null;
  return <p aria-label={label} aria-live="polite" className={`save-indicator save-${state}`} role="status"><span aria-hidden="true">{icon}</span> {label}</p>;
}
