import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { queryKeys } from "../api/query-keys";
import { ApiProblem } from "../api/problem";
import { useAuth } from "../auth/AuthProvider";
import { ProblemNotice } from "../components/ProblemNotice";
import { builderDraftFromView, builderLoadState, buildBuilderPutRequest, canTestBuilder, clearBuilderToken, defaults, validateBuilderDraft, type BuilderDraft } from "./builder-model";
import type { BuilderConfigView } from "../api/admin-api";

export function BuilderSettingsPage() {
  const { api } = useAuth();
  const queryClient = useQueryClient();
  const builder = useQuery({ queryKey: queryKeys.builder, queryFn: ({ signal }) => api.getBuilderConfig(signal), retry: false });
  const [draft, setDraft] = useState<BuilderDraft>(defaults);
  const [savedView, setSavedView] = useState<BuilderConfigView | null>(null);
  const [notice, setNotice] = useState<string>();
  const [problem, setProblem] = useState<unknown>();
  const load = builderLoadState(builder.error);
  useEffect(() => {
    if (builder.data) { setDraft(builderDraftFromView(builder.data)); setSavedView(builder.data); }
    else if (load.kind === "empty") { setDraft(defaults); setSavedView(null); }
  }, [builder.data, load.kind]);
  const errors = validateBuilderDraft(draft, Boolean(savedView?.tokenConfigured));
  const save = useMutation({
    mutationFn: () => api.putBuilderConfig(buildBuilderPutRequest(draft)),
    onSuccess: (view) => { const next = builderDraftFromView(view); setDraft(clearBuilderToken(next)); setSavedView(view); setNotice("Builder settings saved."); setProblem(undefined); queryClient.setQueryData(queryKeys.builder, view); },
    onError: setProblem,
  });
  const test = useMutation({
    mutationFn: () => api.testBuilderConfig(),
    onSuccess: () => { setNotice("Jenkins connection accepted."); setProblem(undefined); },
    onError: setProblem,
  });
  const update = <K extends keyof BuilderDraft>(field: K, value: BuilderDraft[K]) => setDraft((current) => ({ ...current, [field]: value }));
  if (builder.isPending) return <p role="status">Loading builder settings</p>;
  if (load.kind === "error") return problem instanceof ApiProblem ? <ProblemNotice problem={problem} /> : <p role="alert">Unable to load builder settings</p>;
  return <section aria-labelledby="builder-settings-heading">
    <div className="page-heading"><div><h1 id="builder-settings-heading">Builder settings</h1><p>Configure the Jenkins job used for static releases. Tokens are write-only.</p></div><button className="button button-secondary touch-target" disabled={!canTestBuilder(savedView, draft, Boolean(draft.tokenConfigured)) || test.isPending} onClick={() => test.mutate()} type="button">{test.isPending ? "Testing connection" : "Test connection"}</button></div>
    {problem instanceof ApiProblem && <ProblemNotice problem={problem} />}
    {notice && <p role="status">{notice}</p>}
    <form onSubmit={(event) => { event.preventDefault(); if (errors.length === 0 && !save.isPending) save.mutate(); }}>
      <div className="settings-grid">
        <label>Name<input className="touch-target" maxLength={100} value={draft.name} onChange={(event) => update("name", event.target.value)} /></label>
        <label>Jenkins HTTPS base URL<input className="touch-target" inputMode="url" maxLength={300} placeholder="https://jenkins.example.com" value={draft.baseUrl} onChange={(event) => update("baseUrl", event.target.value)} /></label>
        <label>Username<input className="touch-target" maxLength={255} value={draft.username} onChange={(event) => update("username", event.target.value)} /></label>
        <label>Job name<input className="touch-target" maxLength={128} placeholder="blog/site" value={draft.jobName} onChange={(event) => update("jobName", event.target.value)} /></label>
        <label>API token<input className="touch-target" maxLength={4096} type="password" autoComplete="new-password" placeholder={draft.tokenConfigured ? "Leave blank to keep the saved token" : "Required for first save"} value={draft.token} onChange={(event) => update("token", event.target.value)} /></label>
        <label><input type="checkbox" checked={draft.enabled} onChange={(event) => update("enabled", event.target.checked)} /> Enabled</label>
      </div>
      {errors.length > 0 && <p role="alert">Please correct: {errors.join(", ")}</p>}
      <button className="button touch-target" disabled={save.isPending || errors.length > 0} type="submit">{save.isPending ? "Saving" : "Save builder settings"}</button>
    </form>
  </section>;
}
