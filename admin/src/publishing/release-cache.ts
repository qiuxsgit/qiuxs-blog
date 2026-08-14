import type { QueryClient } from "@tanstack/react-query";

import type { ReleaseList, ReleaseView } from "../api/admin-api";
import { requireEntityId } from "../api/ids";
import { queryKeys } from "../api/query-keys";

export type ReleaseCacheSource = "create" | "retry" | "poll";

export async function syncReleaseCache(
  queryClient: QueryClient,
  release: ReleaseView,
  source: ReleaseCacheSource,
): Promise<void> {
  const releaseId = requireEntityId(release.id, "release.id");
  queryClient.setQueryData(queryKeys.release(releaseId), release);

  const invalidations: Promise<void>[] = [];
  if (source === "create") {
    invalidations.push(queryClient.invalidateQueries({ queryKey: queryKeys.releaseListsRoot }));
  } else {
    queryClient.setQueriesData<ReleaseList>(
      { queryKey: queryKeys.releaseListsRoot },
      (current) => {
        if (current === undefined) return current;
        let matched = false;
        const items = current.items.map((item) => {
          if (item.id !== releaseId) return item;
          matched = true;
          return release;
        });
        return matched ? { ...current, items } : current;
      },
    );
  }
  if (release.status === "success") {
    invalidations.push(queryClient.invalidateQueries({ queryKey: queryKeys.articlesRoot }));
  }
  await Promise.all(invalidations);
}
