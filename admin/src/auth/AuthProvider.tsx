import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";

import {
  ApiProblem,
  createAdminApi,
  type AdminApi,
  type AdminView,
  type LoginRequest,
} from "../api/admin-api";

export interface AuthContextValue {
  api: AdminApi;
  state:
    | { kind: "loading" }
    | { kind: "anonymous" }
    | { kind: "authenticated"; admin: AdminView }
    | { kind: "unavailable"; problem: ApiProblem };
  login(input: LoginRequest): Promise<void>;
  logout(): Promise<void>;
  retry(): void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

function isAbortError(error: unknown): boolean {
  return typeof error === "object" && error !== null && "name" in error && error.name === "AbortError";
}

export function AuthProvider({ children }: PropsWithChildren) {
  const queryClient = useQueryClient();
  const mounted = useRef(false);
  const bootstrapGeneration = useRef(0);
  const bootstrapController = useRef<AbortController | undefined>(undefined);
  const actionController = useRef<AbortController | undefined>(undefined);
  const [state, setState] = useState<AuthContextValue["state"]>({ kind: "loading" });
  const [retryVersion, setRetryVersion] = useState(0);

  const expireSession = useCallback(() => {
    if (!mounted.current) return;
    bootstrapGeneration.current += 1;
    queryClient.clear();
    setState({ kind: "anonymous" });
  }, [queryClient]);

  const [api] = useState(() => createAdminApi({ onUnauthenticated: expireSession }));

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      bootstrapController.current?.abort();
      actionController.current?.abort();
    };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    const generation = ++bootstrapGeneration.current;
    let active = true;
    bootstrapController.current = controller;

    queueMicrotask(async () => {
      if (!active) return;
      try {
        const admin = await api.getCurrentAdmin(controller.signal);
        if (active && mounted.current && generation === bootstrapGeneration.current) {
          setState({ kind: "authenticated", admin });
        }
      } catch (error) {
        if (!active || !mounted.current || generation !== bootstrapGeneration.current || isAbortError(error)) return;
        if (error instanceof ApiProblem && error.status === 401) {
          expireSession();
        } else {
          const problem = error instanceof ApiProblem
            ? error
            : new ApiProblem(503, "session_unavailable", "client", "Session service unavailable");
          setState({ kind: "unavailable", problem });
        }
      }
    });

    return () => {
      active = false;
      controller.abort();
      if (bootstrapController.current === controller) bootstrapController.current = undefined;
    };
  }, [api, expireSession, retryVersion]);

  const login = useCallback(async (input: LoginRequest) => {
    actionController.current?.abort();
    const controller = new AbortController();
    actionController.current = controller;
    try {
      const admin = await api.loginAdmin(input, controller.signal);
      if (mounted.current && actionController.current === controller) {
        setState({ kind: "authenticated", admin });
      }
    } finally {
      if (actionController.current === controller) actionController.current = undefined;
    }
  }, [api]);

  const logout = useCallback(async () => {
    actionController.current?.abort();
    const controller = new AbortController();
    actionController.current = controller;
    try {
      await api.logoutAdmin(controller.signal);
      if (mounted.current && actionController.current === controller) expireSession();
    } finally {
      if (actionController.current === controller) actionController.current = undefined;
    }
  }, [api, expireSession]);

  const retry = useCallback(() => {
    setState({ kind: "loading" });
    setRetryVersion((version) => version + 1);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ api, state, login, logout, retry }),
    [api, login, logout, retry, state],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used within AuthProvider");
  return value;
}

export function useOptionalAuth(): AuthContextValue | undefined {
  return useContext(AuthContext);
}
