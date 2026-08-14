import { useEffect, useRef, type PropsWithChildren } from "react";

interface ConfirmDialogProps extends PropsWithChildren {
  confirmLabel: string;
  onCancel: () => void;
  onConfirm: () => void;
  open: boolean;
  title: string;
}

export function ConfirmDialog({ children, confirmLabel, onCancel, onConfirm, open, title }: ConfirmDialogProps) {
  const cancelButton = useRef<HTMLButtonElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    cancelButton.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onCancel();
      }
      if (event.key === "Tab") {
        const backwards = event.shiftKey;
        if ((!backwards && document.activeElement === confirmButton.current) || (backwards && document.activeElement === cancelButton.current)) {
          event.preventDefault();
          (backwards ? confirmButton : cancelButton).current?.focus();
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onCancel, open]);

  if (!open) return null;
  return (
    <div className="dialog-backdrop">
      <section aria-labelledby="confirm-dialog-title" aria-modal="true" className="confirm-dialog" role="alertdialog">
        <h2 id="confirm-dialog-title">{title}</h2>
        <div className="dialog-copy">{children}</div>
        <div className="dialog-actions">
          <button className="button button-secondary touch-target" onClick={onCancel} ref={cancelButton} type="button">Cancel</button>
          <button className="button button-danger touch-target" onClick={onConfirm} ref={confirmButton} type="button">{confirmLabel}</button>
        </div>
      </section>
    </div>
  );
}
