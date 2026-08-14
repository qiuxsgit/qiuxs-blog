import { useEffect, useRef, type PropsWithChildren } from "react";

interface ConfirmDialogProps extends PropsWithChildren {
  cancelDisabled?: boolean;
  confirmDisabled?: boolean;
  confirmLabel: string;
  onCancel: () => void;
  onConfirm: () => void;
  open: boolean;
  title: string;
}

const tabbableSelector = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";

function tabbableChildren(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(tabbableSelector))
    .filter((element) => element.tabIndex >= 0 && element.getAttribute("aria-hidden") !== "true");
}

export function ConfirmDialog({ cancelDisabled = false, children, confirmDisabled = false, confirmLabel, onCancel, onConfirm, open, title }: ConfirmDialogProps) {
  const dialog = useRef<HTMLElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const onCancelRef = useRef(onCancel);
  onCancelRef.current = onCancel;

  useEffect(() => {
    if (!open) {
      opener.current?.focus();
      opener.current = null;
      return;
    }
    opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    cancelButton.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onCancelRef.current();
        return;
      }
      if (event.key !== "Tab" || !dialog.current) return;
      const targets = tabbableChildren(dialog.current);
      if (targets.length === 0) return;
      const first = targets[0]!;
      const last = targets.at(-1)!;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open]);

  useEffect(() => {
    if (open && confirmDisabled && document.activeElement === confirmButton.current) {
      cancelButton.current?.focus();
    }
  }, [confirmDisabled, open]);

  if (!open) return null;
  return (
    <div className="dialog-backdrop">
      <section aria-labelledby="confirm-dialog-title" aria-modal="true" className="confirm-dialog" ref={dialog} role="alertdialog">
        <h2 id="confirm-dialog-title">{title}</h2>
        <div className="dialog-copy">{children}</div>
        <div className="dialog-actions">
          <button aria-disabled={cancelDisabled || undefined} className="button button-secondary touch-target" onClick={() => {
            if (!cancelDisabled) onCancel();
          }} ref={cancelButton} type="button">Cancel</button>
          <button className="button button-danger touch-target" disabled={confirmDisabled} onClick={onConfirm} ref={confirmButton} type="button">{confirmLabel}</button>
        </div>
      </section>
    </div>
  );
}
