import { Navigate, Outlet, useLocation } from "react-router-dom";

import { ProblemNotice } from "../components/ProblemNotice";
import { useAuth } from "./AuthProvider";

export function RequireSession() {
  const { retry, state } = useAuth();
  const location = useLocation();

  if (state.kind === "loading") {
    return <p aria-label="Checking session" role="status">Checking session</p>;
  }
  if (state.kind === "unavailable") {
    return (
      <section className="session-state">
        <ProblemNotice problem={state.problem} />
        <button className="button button-secondary touch-target" onClick={retry} type="button">Retry</button>
      </section>
    );
  }
  if (state.kind === "anonymous") {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return <Outlet />;
}
