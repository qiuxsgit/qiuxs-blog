import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import { ProblemNotice } from "../components/ProblemNotice";
import { queryKeys } from "../api/query-keys";
import { operationProblem } from "../editor/operation-problem";
import { syncReleaseCache } from "./release-cache";
import { builderTargetText, isActiveJobStatus, jobStatusLabel, nextReleaseOffset, previousReleaseOffset, publishSettingsRequest, releaseListQuery, releaseProblemMessage, releaseStatusLabel, selectedReleaseId } from "./release-status";
import type { ReleaseView } from "../api/admin-api";

export function PublishingPage() {
  const { api } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [offset, setOffset] = useState(0);
  const selectedId = selectedReleaseId(location.search);
  const list = useQuery({ queryKey: queryKeys.releaseList(20, offset), queryFn: ({ signal }) => api.listReleases(releaseListQuery(offset), signal) });
  const selectedFromList = useMemo(() => list.data?.items.find((item) => item.id === selectedId), [list.data, selectedId]);
  const detail = useQuery({ queryKey: queryKeys.release(selectedId ?? 0), queryFn: ({ signal }) => api.getRelease(selectedId!, signal), enabled: selectedId !== undefined, initialData: selectedFromList });
  const [actionError, setActionError] = useState<unknown>();
  const publishSettings = useMutation({
    mutationFn: async () => {
      const result = await api.createRelease(publishSettingsRequest());
      await syncReleaseCache(queryClient, result.release, "create");
      return result.release.id;
    },
    onSuccess: (id) => navigate(`/publishing?release=${id}`),
    onError: setActionError,
  });
  const retry = useMutation({
    mutationFn: async (release: ReleaseView) => {
      const result = await api.retryRelease(release.id);
      await syncReleaseCache(queryClient, result.release, "retry");
      return result.release.id;
    },
    onSuccess: (id) => navigate(`/publishing?release=${id}`),
    onError: setActionError,
  });

  useEffect(() => {
    if (!selectedId || !detail.data || !isActiveJobStatus(detail.data.latestJob.status)) return;
    let disposed = false;
    const poll = async () => {
      try {
        const current = await api.getRelease(selectedId);
        if (!disposed) await syncReleaseCache(queryClient, current, "poll");
      } catch (error) {
        if (!disposed) setActionError(error);
      }
    };
    const timer = window.setInterval(() => void poll(), 3000);
    return () => { disposed = true; window.clearInterval(timer); };
  }, [api, detail.data, queryClient, selectedId]);

  const active = detail.data;
  return <section aria-labelledby="publishing-heading">
    <div className="page-heading"><div><h1 id="publishing-heading">Publishing</h1><p>Immutable releases and Jenkins job history.</p></div><button className="button touch-target" disabled={publishSettings.isPending} onClick={() => { setActionError(undefined); publishSettings.mutate(); }} type="button">{publishSettings.isPending ? "Starting release" : "Publish saved site settings"}</button></div>
    {actionError !== undefined && <><ProblemNotice problem={operationProblem(actionError, releaseProblemMessage(actionError), "release_failed")} /><p>{releaseProblemMessage(actionError)}</p></>}
    {list.isError ? <><ProblemNotice problem={operationProblem(list.error, "Unable to load releases", "list_releases_failed")} /><button className="button touch-target" onClick={() => void list.refetch()} type="button">Retry</button></> : <>
      {list.isPending ? <p role="status">Loading releases</p> : <>
        {list.data?.items.length === 0 && <p>No releases yet.</p>}
        <ol aria-label="Release history">{list.data?.items.map((release) => <li key={release.id}><Link to={`/publishing?release=${release.id}`}>Release #{release.id}</Link> — {releaseStatusLabel(release.status)} — {release.createdAt}</li>)}</ol>
        <nav aria-label="Release pagination"><button className="touch-target" disabled={offset === 0} onClick={() => setOffset(previousReleaseOffset(offset))} type="button">Previous</button><button className="touch-target" disabled={nextReleaseOffset(offset, list.data?.items.length ?? 0) === undefined} onClick={() => { const next = nextReleaseOffset(offset, list.data?.items.length ?? 0); if (next !== undefined) setOffset(next); }} type="button">Next</button></nav>
      </>}
    </>}
    {active && <article aria-labelledby="release-detail-heading"><h2 id="release-detail-heading">Release #{active.id}</h2><p>Status: {releaseStatusLabel(active.status)}</p><p>Checksum: {active.checksum}</p><p>Created: {active.createdAt}</p><p>Completed: {active.completedAt ?? "—"}</p><h3>Jobs</h3><ol>{active.jobs.map((job) => <li key={job.id}><p>Job #{job.id} · release #{job.releaseId} · {jobStatusLabel(job.status)}</p><p>Builder: {builderTargetText(job)}</p><p>Stage: {job.stage} · Build: {job.buildNumber ?? "—"}</p><p>{job.errorSummary}</p><p>Finished: {job.finishedAt ?? "—"}</p></li>)}</ol>{(active.status === "failed" || active.latestJob.status === "failed") && <button className="button touch-target" disabled={retry.isPending} onClick={() => { setActionError(undefined); retry.mutate(active); }} type="button">{retry.isPending ? "Retrying" : "Retry release"}</button>}</article>}
  </section>;
}
