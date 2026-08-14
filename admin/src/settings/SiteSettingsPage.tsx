import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { queryKeys } from "../api/query-keys";
import { ApiProblem } from "../api/problem";
import { useAuth } from "../auth/AuthProvider";
import { ProblemNotice } from "../components/ProblemNotice";
import { syncReleaseCache } from "../publishing/release-cache";
import { buildPutRequest, defaults, siteDraftFromView, validateSiteDraft, type SiteDraft } from "./site-model";
import { confirmedPutFields, conflictLocalDraft, publishSettingsRequest } from "./site-release";

export function SiteSettingsPage() {
  const { api } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const site = useQuery({ queryKey: queryKeys.site, queryFn: ({ signal }) => api.getSiteSettings(signal) });
  const [draft, setDraft] = useState<SiteDraft>(defaults);
  const [notice, setNotice] = useState<string>();
  const [problem, setProblem] = useState<unknown>();
  const [conflict, setConflict] = useState(false);
  useEffect(() => { if (site.data && !conflict) setDraft(siteDraftFromView(site.data)); }, [conflict, site.data]);
  const save = useMutation({
    mutationFn: () => api.putSiteSettings(buildPutRequest(draft)),
    onSuccess: (saved) => { setDraft(siteDraftFromView(saved)); setConflict(false); setNotice("Saved settings — pending publication."); setProblem(undefined); queryClient.setQueryData(queryKeys.site, saved); },
    onError: (error) => { setProblem(error); if (error instanceof ApiProblem && error.status === 409 && error.code === "settings_conflict") setConflict(true); },
  });
  const publish = useMutation({
    mutationFn: async () => { const result = await api.createRelease(publishSettingsRequest()); await syncReleaseCache(queryClient, result.release, "create"); return result.release.id; },
    onSuccess: (id) => navigate(`/publishing?release=${id}`),
    onError: setProblem,
  });
  const update = <K extends keyof SiteDraft>(field: K, value: SiteDraft[K]) => setDraft((current) => ({ ...current, [field]: value }));
  const errors = validateSiteDraft(draft);
  if (site.isPending) return <p role="status">Loading site settings</p>;
  if (site.isError) return site.error instanceof ApiProblem ? <ProblemNotice problem={site.error} /> : <p role="alert">Unable to load site settings</p>;
  return <section aria-labelledby="site-settings-heading">
    <div className="page-heading"><div><h1 id="site-settings-heading">Site settings</h1><p>Public identity, SEO, about content, and filing information.</p></div><button className="button touch-target" disabled={publish.isPending} onClick={() => publish.mutate()} type="button">{publish.isPending ? "Starting release" : "Publish saved settings"}</button></div>
    {problem instanceof ApiProblem && <ProblemNotice problem={problem} />}
    {notice && <p role="status">{notice}</p>}
    {conflict && site.data && <div className="async-page"><p>These settings changed on the server. Your local fields are preserved.</p><button className="button touch-target" onClick={() => { setDraft(conflictLocalDraft(draft, siteDraftFromView(site.data!))); setConflict(false); }} type="button">Keep my changes</button><button className="button button-secondary touch-target" onClick={() => { setDraft(confirmedPutFields(siteDraftFromView(site.data!))); setConflict(false); }} type="button">Reload confirmed settings</button></div>}
    <form onSubmit={(event) => { event.preventDefault(); if (errors.length === 0 && !save.isPending) save.mutate(); }}>
      <div className="settings-grid">
        <label>Site name<input className="touch-target" maxLength={100} value={draft.siteName} onChange={(event) => update("siteName", event.target.value)} /></label>
        <label>Author name<input className="touch-target" maxLength={100} value={draft.authorName} onChange={(event) => update("authorName", event.target.value)} /></label>
        <label>Author bio<textarea maxLength={1000} value={draft.authorBio} onChange={(event) => update("authorBio", event.target.value)} /></label>
        <label>Home status<textarea maxLength={500} value={draft.homeStatus} onChange={(event) => update("homeStatus", event.target.value)} /></label>
        <label>About Markdown<textarea maxLength={2 * 1024 * 1024} value={draft.aboutMd} onChange={(event) => update("aboutMd", event.target.value)} /></label>
        <label>SEO title<input className="touch-target" maxLength={100} value={draft.seoDefaultTitle} onChange={(event) => update("seoDefaultTitle", event.target.value)} /></label>
        <label>SEO description<textarea maxLength={300} value={draft.seoDefaultDescription} onChange={(event) => update("seoDefaultDescription", event.target.value)} /></label>
        <label>Default image media ID<input className="touch-target" inputMode="numeric" value={draft.seoDefaultImageMediaId ?? ""} onChange={(event) => update("seoDefaultImageMediaId", event.target.value === "" ? null : Number(event.target.value))} /></label>
        <label>Filing name<input className="touch-target" maxLength={100} value={draft.filingName} onChange={(event) => update("filingName", event.target.value)} /></label>
        <label>Filing number<input className="touch-target" maxLength={100} value={draft.filingNumber} onChange={(event) => update("filingNumber", event.target.value)} /></label>
        <p>Filing URL: <a href="https://beian.miit.gov.cn/" rel="noreferrer" target="_blank">https://beian.miit.gov.cn/</a></p>
      </div>
      <fieldset><legend>Social links</legend>{draft.socialLinks.map((link, index) => <div className="social-row" key={`${index}-${link.label}`}><input aria-label={`Social label ${index + 1}`} className="touch-target" value={link.label} onChange={(event) => setDraft((current) => ({ ...current, socialLinks: current.socialLinks.map((item, i) => i === index ? { ...item, label: event.target.value } : item) }))} /><input aria-label={`Social URL ${index + 1}`} className="touch-target" value={link.url} onChange={(event) => setDraft((current) => ({ ...current, socialLinks: current.socialLinks.map((item, i) => i === index ? { ...item, url: event.target.value } : item) }))} /><button className="touch-target" onClick={() => update("socialLinks", draft.socialLinks.filter((_, i) => i !== index))} type="button">Remove</button></div>)}<button className="button button-secondary touch-target" disabled={draft.socialLinks.length >= 16} onClick={() => update("socialLinks", [...draft.socialLinks, { label: "", url: "https://" }])} type="button">Add social link</button></fieldset>
      {errors.length > 0 && <p role="alert">Please correct: {errors.join(", ")}</p>}
      <button className="button touch-target" disabled={save.isPending || errors.length > 0} type="submit">{save.isPending ? "Saving" : "Save settings"}</button>
    </form>
  </section>;
}
