import type { ArticleDetail, CreateReleaseRequest, PublishJobView, ReleaseView } from "../api/admin-api";
import type { ApiProblem } from "../api/problem";

export function isActiveJobStatus(status: PublishJobView["status"]): boolean {
  return status === "pending" || status === "queued" || status === "building" || status === "deploying";
}

export function releaseStatusLabel(status: ReleaseView["status"]): string {
  return status === "queued" ? "Release queued" : status === "success" ? "Release published" : "Release failed";
}

export function jobStatusLabel(status: PublishJobView["status"]): string {
  return ({ pending: "Trigger pending", queued: "Jenkins queued", building: "Building", deploying: "Deploying", success: "Succeeded", failed: "Failed" })[status];
}

export function releaseListQuery(offset: number, limit = 20): { limit: number; offset: number } {
  return { limit, offset: Math.max(0, Math.trunc(offset)) };
}

export function previousReleaseOffset(offset: number, limit = 20): number { return Math.max(0, offset - limit); }
export function nextReleaseOffset(offset: number, count: number, limit = 20): number | undefined { return count === limit ? offset + limit : undefined; }

export function selectedReleaseId(search: string): number | undefined {
  const raw = new URLSearchParams(search).get("release");
  if (!raw || !/^\d+$/u.test(raw)) return undefined;
  const id = Number(raw);
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

export function publishArticleRequest(article: Pick<ArticleDetail, "id">): CreateReleaseRequest {
  return { mode: "publish_article", articleId: article.id };
}

export function publishSettingsRequest(): CreateReleaseRequest {
  return { mode: "publish_settings", articleId: null };
}

export function releaseProblemMessage(error: unknown): string {
  if (error && typeof error === "object" && "status" in error && "code" in error) {
    const problem = error as Pick<ApiProblem, "status" | "code">;
    if (problem.status === 409 && problem.code === "release_conflict") return "Another release is being processed. Keep this release selected and try again later.";
    if (problem.status === 412 && problem.code === "precondition_failed") return "Service reconciliation or saved builder prerequisites require operator action.";
  }
  return "The release operation failed. Check the request ID and try again.";
}

export function builderTargetText(job: PublishJobView): string {
  const target = job.builderTarget;
  return `${target.name} · ${target.baseUrl} · ${target.username} · ${target.jobName}`;
}
