import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { queryKeys } from "../api/query-keys";
import { ApiProblem } from "../api/problem";
import { useAuth } from "../auth/AuthProvider";
import { ProblemNotice } from "../components/ProblemNotice";
import {
  applyHotlinkCache,
  buildHotlinkPutRequest,
  defaults,
  draftFromHotlinkView,
  hotlinkConflictState,
  validateHotlinkDraft,
  type HotlinkDraft,
} from "./hotlink-model";

export function HotlinkSettingsPage() {
  const { api } = useAuth();
  const queryClient = useQueryClient();
  const hotlink = useQuery({ queryKey: queryKeys.hotlink, queryFn: ({ signal }) => api.getHotlinkSettings(signal), retry: false });
  const [draft, setDraft] = useState<HotlinkDraft>(defaults);
  const [conflict, setConflict] = useState(false);
  const [notice, setNotice] = useState<string>();
  const [problem, setProblem] = useState<unknown>();

  useEffect(() => {
    if (hotlink.data && !conflict) setDraft(draftFromHotlinkView(hotlink.data));
  }, [conflict, hotlink.data]);

  const save = useMutation({
    mutationFn: () => api.putHotlinkSettings(buildHotlinkPutRequest(draft)),
    onSuccess: (saved) => {
      setDraft(draftFromHotlinkView(saved));
      setConflict(false);
      setNotice("Hotlink settings saved and active immediately.");
      setProblem(undefined);
      queryClient.setQueryData(queryKeys.hotlink, applyHotlinkCache(hotlink.data ?? defaults, saved));
    },
    onError: (error) => {
      setProblem(error);
      const state = hotlinkConflictState(error, draft);
      setConflict(state.conflict);
      if (state.message) setNotice(state.message);
    },
  });

  const errors = validateHotlinkDraft(draft);
  const updateEntry = (index: number, patch: Partial<HotlinkDraft["entries"][number]>) => {
    setDraft((current) => ({ ...current, entries: current.entries.map((entry, currentIndex) => currentIndex === index ? { ...entry, ...patch } : entry) }));
  };

  if (hotlink.isPending) return <p role="status">Loading hotlink settings</p>;
  if (hotlink.isError) return hotlink.error instanceof ApiProblem ? <ProblemNotice problem={hotlink.error} /> : <p role="alert">Unable to load hotlink settings</p>;

  return <section aria-labelledby="hotlink-settings-heading">
    <div className="page-heading">
      <div><h1 id="hotlink-settings-heading">Hotlink protection</h1><p>Changes take effect immediately for media proxy requests. Empty Referer can be allowed independently.</p></div>
    </div>
    {problem instanceof ApiProblem && <ProblemNotice problem={problem} />}
    {notice && <p role="status">{notice}</p>}
    {conflict && <div className="async-page"><p>Your local fields are preserved after a server-side settings conflict.</p><button className="button button-secondary touch-target" onClick={() => { setDraft(hotlink.data ? draftFromHotlinkView(hotlink.data) : defaults); setConflict(false); setNotice("Reloaded server settings."); }} type="button">Reload server settings</button></div>}
    <form onSubmit={(event) => { event.preventDefault(); if (errors.length === 0 && !save.isPending) save.mutate(); }}>
      <label className="checkbox-row"><input type="checkbox" checked={draft.allowEmptyReferer} onChange={(event) => setDraft((current) => ({ ...current, allowEmptyReferer: event.target.checked }))} /> Allow empty Referer</label>
      <div className="table-wrap">
        <table><caption className="sr-only">Allowed Referer hostnames</caption><thead><tr><th scope="col">Hostname</th><th scope="col">Enabled</th><th scope="col"><span className="sr-only">Actions</span></th></tr></thead><tbody>
          {draft.entries.map((entry, index) => <tr key={`${index}-${entry.hostname}`}><td><input aria-label={`Hostname ${index + 1}`} className="touch-target" maxLength={253} value={entry.hostname} onChange={(event) => updateEntry(index, { hostname: event.target.value })} /></td><td><label><input aria-label={`Enabled ${index + 1}`} type="checkbox" checked={entry.enabled} onChange={(event) => updateEntry(index, { enabled: event.target.checked })} /> Enabled</label></td><td><button className="button button-secondary touch-target" onClick={() => setDraft((current) => ({ ...current, entries: current.entries.filter((_, currentIndex) => currentIndex !== index) }))} type="button">Delete</button></td></tr>)}
        </tbody></table>
      </div>
      <button className="button button-secondary touch-target" onClick={() => setDraft((current) => ({ ...current, entries: [...current.entries, { hostname: "", enabled: true }] }))} type="button">Add hostname</button>
      {errors.length > 0 && <p role="alert">Please correct: {errors.join(", ")}</p>}
      <button className="button touch-target" disabled={save.isPending || errors.length > 0} type="submit">{save.isPending ? "Saving" : "Save hotlink settings"}</button>
    </form>
  </section>;
}
