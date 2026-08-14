import { useEffect, useRef, useState, type FormEvent } from "react";
import { Navigate, useLocation } from "react-router-dom";

import { ApiProblem } from "../api/problem";
import { FormField } from "../components/FormField";
import { ProblemNotice } from "../components/ProblemNotice";
import { useAuth } from "./AuthProvider";

function intendedPath(state: unknown): string {
  if (typeof state !== "object" || state === null || !("from" in state)) return "/articles";
  const from = (state as { from?: unknown }).from;
  return typeof from === "string" && /^\/(?!\/)/.test(from) && from !== "/login" ? from : "/articles";
}

export function LoginPage() {
  const auth = useAuth();
  const location = useLocation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const errorRef = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  if (auth.state.kind === "loading") {
    return <p aria-label="Checking session" role="status">Checking session</p>;
  }
  if (auth.state.kind === "unavailable") {
    return (
      <main className="login-page">
        <ProblemNotice problem={auth.state.problem} />
        <button className="button button-secondary touch-target" onClick={auth.retry} type="button">Retry</button>
      </main>
    );
  }
  if (auth.state.kind === "authenticated") {
    return <Navigate to={intendedPath(location.state)} replace />;
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submittingRef.current) return;
    submittingRef.current = true;
    setSubmitting(true);
    setError(undefined);
    try {
      await auth.login({ username, password });
      setPassword("");
    } catch (cause) {
      setPassword("");
      setError(cause instanceof ApiProblem && cause.status === 401
        ? "Invalid username or password"
        : cause instanceof ApiProblem ? cause.title : "Unable to sign in");
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  return (
    <main className="login-page">
      <form className="login-card" onSubmit={(event) => void submit(event)}>
        <h1>Admin sign in</h1>
        {error && <p className="login-error" ref={errorRef} role="alert" tabIndex={-1}>{error}</p>}
        <FormField
          autoComplete="username"
          label="Username"
          name="username"
          onChange={(event) => setUsername(event.target.value)}
          required
          value={username}
        />
        <FormField
          autoComplete="current-password"
          label="Password"
          name="password"
          onChange={(event) => setPassword(event.target.value)}
          required
          type="password"
          value={password}
        />
        <button className="button login-submit touch-target" disabled={submitting} type="submit">
          {submitting ? "Signing in" : "Sign in"}
        </button>
      </form>
    </main>
  );
}
