import { describe, expect, it } from "vitest";
import { builderTargetText, isActiveJobStatus, jobStatusLabel, nextReleaseOffset, previousReleaseOffset, publishArticleRequest, publishSettingsRequest, releaseListQuery, releaseStatusLabel, selectedReleaseId } from "./release-status";
import { articleDetail, failedJob, failedRelease } from "../test/fixtures";

describe("release status pure model", () => {
  it("keeps release and job domains separate", () => {
    expect(["queued", "success", "failed"] as const).toEqual(expect.arrayContaining(["queued", "success", "failed"]));
    expect(((["queued", "success", "failed"] as const).map(releaseStatusLabel))).toEqual(["Release queued", "Release published", "Release failed"]);
    expect(((["pending", "queued", "building", "deploying", "success", "failed"] as const).map(jobStatusLabel))).toEqual(["Trigger pending", "Jenkins queued", "Building", "Deploying", "Succeeded", "Failed"]);
    expect(isActiveJobStatus("building")).toBe(true);
    expect(isActiveJobStatus("success")).toBe(false);
  });
  it("builds offset and selection values without page metadata", () => {
    expect(releaseListQuery(0)).toEqual({ limit: 20, offset: 0 });
    expect(previousReleaseOffset(21)).toBe(1);
    expect(nextReleaseOffset(0, 20)).toBe(20);
    expect(nextReleaseOffset(0, 19)).toBeUndefined();
    expect(selectedReleaseId("?release=71")).toBe(71);
    expect(selectedReleaseId("?release=0")).toBeUndefined();
    expect(selectedReleaseId("?release=abc")).toBeUndefined();
  });
  it("builds exact publish requests and safe builder text", () => {
    expect(publishArticleRequest(articleDetail)).toEqual({ mode: "publish_article", articleId: 11 });
    expect(publishSettingsRequest()).toEqual({ mode: "publish_settings", articleId: null });
    expect(builderTargetText(failedJob)).toContain("home-jenkins");
    expect(failedRelease.id).toBe(failedJob.releaseId);
  });
});
