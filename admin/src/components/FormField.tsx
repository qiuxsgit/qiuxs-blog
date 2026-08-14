import type { InputHTMLAttributes, ReactNode } from "react";

interface FormFieldProps extends InputHTMLAttributes<HTMLInputElement> {
  error?: string;
  hint?: ReactNode;
  label: string;
}

export function FormField({ error, hint, id, label, ...input }: FormFieldProps) {
  const fieldId = id ?? label.toLowerCase().replace(/\s+/g, "-");
  const hintId = hint ? `${fieldId}-hint` : undefined;
  const errorId = error ? `${fieldId}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;
  return (
    <div className="form-field">
      <label htmlFor={fieldId}>{label}</label>
      <input {...input} aria-describedby={describedBy} aria-invalid={Boolean(error)} id={fieldId} />
      {hint && <p className="field-hint" id={hintId}>{hint}</p>}
      {error && <p className="field-error" id={errorId} role="alert"><span aria-hidden="true">⚠</span> {error}</p>}
    </div>
  );
}
