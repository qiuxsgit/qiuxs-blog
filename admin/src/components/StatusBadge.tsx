type StatusTone = "active" | "failed" | "pending" | "success" | "warning";

const indicators: Record<StatusTone, readonly [string, string]> = {
  active: ["●", "Active"],
  failed: ["✕", "Failed"],
  pending: ["◌", "Pending"],
  success: ["✓", "Success"],
  warning: ["⚠", "Warning"],
};

export function StatusBadge({ label, tone }: { label?: string; tone: StatusTone }) {
  const [icon, fallback] = indicators[tone];
  return <span className={`status-badge status-${tone}`}><span aria-hidden="true">{icon}</span> {label ?? fallback}</span>;
}
