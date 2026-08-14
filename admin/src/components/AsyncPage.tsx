import type { PropsWithChildren, ReactNode } from "react";

import { ProblemNotice } from "./ProblemNotice";
import type { ApiProblem } from "../api/problem";

interface AsyncPageProps extends PropsWithChildren {
  error?: ApiProblem;
  label: string;
  loading: boolean;
  empty?: ReactNode;
}

export function AsyncPage({ children, empty, error, label, loading }: AsyncPageProps) {
  if (loading) {
    return <section aria-busy="true" aria-label={label} className="async-page"><p aria-label={label} role="status">{label}</p></section>;
  }
  if (error) {
    return <ProblemNotice problem={error} />;
  }
  if (empty) {
    return <section className="async-page">{empty}</section>;
  }
  return <>{children}</>;
}
