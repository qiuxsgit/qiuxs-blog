import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import type { ReleaseList, ReleaseView } from "../api/admin-api";
import { failedRelease, releaseList } from "../test/fixtures";
import { queryKeys } from "../api/query-keys";
import { syncReleaseCache } from "./release-cache";

function createClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe("syncReleaseCache", () => {
  it("seeds the release detail and invalidates release lists once when a release is created", async () => {
    const client = createClient();
    client.setQueryData(queryKeys.releaseList(20, 0), releaseList);
    const invalidateQueries = vi.spyOn(client, "invalidateQueries");

    await syncReleaseCache(client, failedRelease, "create");

    expect(client.getQueryData(queryKeys.release(failedRelease.id))).toEqual(failedRelease);
    expect(invalidateQueries).toHaveBeenCalledTimes(1);
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: queryKeys.releaseListsRoot });
    expect(client.getQueryState(queryKeys.release(failedRelease.id))?.isInvalidated).toBe(false);
  });

  it.each(["retry", "poll"] as const)("patches cached release lists without refetching when source is %s", async (source) => {
    const client = createClient();
    const list: ReleaseList = { items: [failedRelease] };
    const updated: ReleaseView = { ...failedRelease, status: "queued", completedAt: null };
    client.setQueryData(queryKeys.releaseList(20, 0), list);
    const invalidateQueries = vi.spyOn(client, "invalidateQueries");

    await syncReleaseCache(client, updated, source);

    expect(client.getQueryData(queryKeys.release(updated.id))).toEqual(updated);
    expect(client.getQueryData<ReleaseList>(queryKeys.releaseList(20, 0))).toEqual({ items: [updated] });
    expect(client.getQueryData<ReleaseList>(queryKeys.releaseList(20, 0))).not.toBe(list);
    expect(invalidateQueries).not.toHaveBeenCalledWith({ queryKey: queryKeys.releaseListsRoot });
  });

  it("does not refetch cached release lists during repeated detail polling", async () => {
    const client = createClient();
    let listRequests = 0;
    await client.fetchQuery({
      queryKey: queryKeys.releaseList(20, 0),
      queryFn: async () => {
        listRequests += 1;
        return releaseList;
      },
    });

    await syncReleaseCache(client, { ...failedRelease, status: "queued", completedAt: null }, "poll");
    await syncReleaseCache(client, failedRelease, "poll");

    expect(listRequests).toBe(1);
  });

  it.each(["create", "retry", "poll"] as const)("invalidates the complete article cache after a successful %s result", async (source) => {
    const client = createClient();
    const invalidateQueries = vi.spyOn(client, "invalidateQueries");
    const successful: ReleaseView = { ...failedRelease, status: "success" };

    await syncReleaseCache(client, successful, source);

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: queryKeys.articlesRoot });
  });
});
