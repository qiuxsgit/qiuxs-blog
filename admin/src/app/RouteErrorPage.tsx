import { isRouteErrorResponse, useRouteError } from "react-router-dom";

import { ApiProblem } from "../api/problem";
import { ProblemNotice } from "../components/ProblemNotice";

export function RouteErrorPage() {
  const error = useRouteError();

  if (error instanceof ApiProblem) {
    return <section className="route-error"><ProblemNotice problem={error} /></section>;
  }
  const message = isRouteErrorResponse(error) ? error.statusText : "The requested page could not be loaded.";
  return (
    <section className="route-error">
      <h1>Unable to load this page</h1>
      <p>{message || "The requested page could not be loaded."}</p>
    </section>
  );
}
