import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";

import { ApiProblem } from "../api/problem";

export function editorRouteIsDirty(): boolean {
  return /^\/articles\/[^/]+\/edit\/?$/.test(window.location.pathname)
    && document.documentElement.dataset.editorDirty === "true";
}

export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: {
        retry: (failureCount, error) => !(error instanceof ApiProblem && error.status === 401) && failureCount < 1,
        refetchOnWindowFocus: () => !editorRouteIsDirty(),
      },
    },
  });
}

const queryClient = createAppQueryClient();

export function AppProviders({ children }: PropsWithChildren) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
