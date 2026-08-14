import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { StrictMode, useEffect, type ReactElement } from "react";
import {
  MemoryRouter,
  Navigate,
  Outlet,
  Route,
  RouterProvider,
  Routes,
  createMemoryRouter,
} from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { appRoutes } from "../app/AppRouter";
import { server } from "../test/server";
import { AuthProvider, useAuth } from "./AuthProvider";
import { LoginPage } from "./LoginPage";
import { RequireSession } from "./RequireSession";

const adminView = { id: 1, username: "admin" };

function problem(status: number, code: string, title: string) {
  return HttpResponse.json(
    {
      type: `https://qiuxs.com/problems/${code}`,
      title,
      status,
      code,
      requestId: `req-${code}`,
    },
    { status, headers: { "Content-Type": "application/problem+json" } },
  );
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
}

function renderApplication(route: string, strict = false) {
  const queryClient = createQueryClient();
  const router = createMemoryRouter(appRoutes, { initialEntries: [route] });
  const tree = (
    <QueryClientProvider client={queryClient}>
      <AuthProvider><RouterProvider router={router} /></AuthProvider>
    </QueryClientProvider>
  );
  return {
    ...render(strict ? <StrictMode>{tree}</StrictMode> : tree),
    queryClient,
    router,
    user: userEvent.setup(),
  };
}

function renderCustomRoutes(ui: ReactElement, route: string) {
  const queryClient = createQueryClient();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route element={<RequireSession />}>
              <Route element={<Outlet />}>
                <Route path="/articles/:articleId/edit" element={ui} />
              </Route>
            </Route>
            <Route path="*" element={<Navigate to="/articles/11/edit" replace />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

afterEach(() => {
  cleanup();
  localStorage.clear();
  sessionStorage.clear();
  delete document.documentElement.dataset.editorDirty;
  vi.restoreAllMocks();
});

describe("session bootstrap and protected routing", () => {
  it("bootstraps /me exactly once in StrictMode before rendering an authenticated route", async () => {
    let requests = 0;
    server.use(http.get("*/api/admin/v1/me", () => {
      requests += 1;
      return HttpResponse.json(adminView);
    }));

    renderApplication("/articles", true);

    expect(await screen.findByRole("heading", { name: "Articles" })).toBeInTheDocument();
    expect(requests).toBe(1);
  });

  it("aborts an in-flight bootstrap when the provider unmounts", async () => {
    let requestSignal: AbortSignal | undefined;
    server.use(http.get("*/api/admin/v1/me", async ({ request }) => {
      requestSignal = request.signal;
      await new Promise<void>((resolve) => request.signal.addEventListener("abort", () => resolve(), { once: true }));
      return HttpResponse.json(adminView);
    }));

    const view = renderApplication("/articles");
    await waitFor(() => expect(requestSignal).toBeDefined());
    view.unmount();

    await waitFor(() => expect(requestSignal?.aborted).toBe(true));
  });

  it.each([
    ["/articles", "Articles"],
    ["/articles/new", "New article"],
    ["/articles/11/edit", "Edit article"],
    ["/articles/11/preview", "Article preview"],
    ["/articles/11/versions", "Article versions"],
    ["/publishing", "Publishing"],
    ["/settings/site", "Site"],
    ["/settings/builder", "Builder"],
    ["/settings/hotlink", "Hotlink"],
  ])("protects the exact route %s", async (route, heading) => {
    renderApplication(route);
    expect(await screen.findByRole("heading", { name: heading })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Admin" })).toBeInTheDocument();
  });

  it("preserves the intended pathname through a 401 bootstrap and successful login without storing secrets", async () => {
    const password = "correct horse battery staple";
    const cookie = "session-cookie-secret";
    server.use(
      http.get("*/api/admin/v1/me", () => problem(401, "unauthenticated", "Authentication required")),
      http.post("*/api/admin/v1/session", () => HttpResponse.json(adminView, {
        headers: { "Set-Cookie": `qx_blog_session=${cookie}; HttpOnly; Secure; SameSite=Strict` },
      })),
    );
    const { router, user } = renderApplication("/articles/11/preview");

    const username = await screen.findByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");
    expect(username).toHaveAttribute("autocomplete", "username");
    expect(passwordInput).toHaveAttribute("autocomplete", "current-password");
    await user.type(username, "admin");
    await user.type(passwordInput, password);
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("heading", { name: "Article preview" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/articles/11/preview");
    expect(router.state.location.search).toBe("");
    const applicationStorage = Object.entries(localStorage)
      .filter(([key]) => key !== "__msw-cookie-store__");
    expect(JSON.stringify(applicationStorage)).not.toContain(password);
    expect(JSON.stringify(applicationStorage)).not.toContain(cookie);
    expect(JSON.stringify(Object.entries(sessionStorage))).not.toContain(password);
    expect(JSON.stringify(Object.entries(sessionStorage))).not.toContain(cookie);
    expect(window.location.href).not.toContain(password);
    expect(window.location.href).not.toContain(cookie);
  });

  it("shows dependency unavailability with retry instead of redirecting to login", async () => {
    let requests = 0;
    server.use(http.get("*/api/admin/v1/me", () => {
      requests += 1;
      return requests === 1
        ? problem(503, "dependency_unavailable", "Session service unavailable")
        : HttpResponse.json(adminView);
    }));
    const { user } = renderApplication("/publishing");

    expect(await screen.findByRole("heading", { name: "Session service unavailable" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign in" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByRole("heading", { name: "Publishing" })).toBeInTheDocument();
    expect(requests).toBe(2);
  });

  it("retains the username, clears the password, focuses the announced error, and blocks duplicate login submissions", async () => {
    const password = "never-store-this-password";
    let loginRequests = 0;
    server.use(
      http.get("*/api/admin/v1/me", () => problem(401, "unauthenticated", "Authentication required")),
      http.post("*/api/admin/v1/session", async () => {
        loginRequests += 1;
        await Promise.resolve();
        return problem(401, "invalid_credentials", `Invalid ${password}`);
      }),
    );
    const { router, user } = renderApplication("/articles/new");

    const username = await screen.findByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");
    await user.type(username, "admin");
    await user.type(passwordInput, password);
    const submit = screen.getByRole("button", { name: "Sign in" });
    await user.dblClick(submit);

    const error = await screen.findByRole("alert");
    expect(error).toHaveFocus();
    expect(error).toHaveTextContent("Invalid username or password");
    expect(error).not.toHaveTextContent(password);
    expect(username).toHaveValue("admin");
    expect(passwordInput).toHaveValue("");
    expect(loginRequests).toBe(1);
    expect(router.state.location.pathname).toBe("/login");
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(window.location.href).not.toContain(password);
  });

  it("logs out from an authenticated shell after 204 and clears cached server state", async () => {
    let logoutRequests = 0;
    server.use(http.delete("*/api/admin/v1/session", () => {
      logoutRequests += 1;
      return new HttpResponse(null, { status: 204 });
    }));
    const { queryClient, user } = renderApplication("/articles");
    queryClient.setQueryData(["articles"], { items: [adminView] });

    await screen.findByRole("heading", { name: "Articles" });
    await user.click(screen.getByRole("button", { name: "Log out" }));

    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(logoutRequests).toBe(1);
    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
  });

  it("centralizes session expiry, clears query state, and preserves dirty-editor recovery data", async () => {
    localStorage.setItem("article:11:recovery", "local markdown recovery");
    document.documentElement.dataset.editorDirty = "true";
    server.use(http.get("*/api/admin/v1/articles", () => problem(401, "unauthenticated", "Session expired")));

    function SessionExpiryProbe() {
      const { api } = useAuth();
      useEffect(() => {
        void api.listArticles().catch(() => undefined);
      }, [api]);
      return <h1>Edit article</h1>;
    }

    const { queryClient } = renderCustomRoutes(<SessionExpiryProbe />, "/articles/11/edit");
    queryClient.setQueryData(["article", 11], { content: "server markdown" });

    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
    expect(localStorage.getItem("article:11:recovery")).toBe("local markdown recovery");
    expect(document.documentElement.dataset.editorDirty).toBe("true");
  });
});
