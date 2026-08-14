import { Navigate, Outlet, RouterProvider, createBrowserRouter, type RouteObject } from "react-router-dom";

import { LoginPage } from "../auth/LoginPage";
import { RequireSession } from "../auth/RequireSession";
import { AppShell } from "../layout/AppShell";
import { RouteErrorPage } from "./RouteErrorPage";

function ShellLayout() {
  return <AppShell><Outlet /></AppShell>;
}

function ShellRouteErrorPage() {
  return <AppShell><RouteErrorPage /></AppShell>;
}

export const appRoutes: RouteObject[] = [
  { path: "/login", element: <LoginPage /> },
  {
    element: <RequireSession />,
    children: [
      {
        path: "/",
        element: <ShellLayout />,
        errorElement: <ShellRouteErrorPage />,
        children: [
          { index: true, element: <Navigate to="/articles" replace /> },
          { path: "articles", element: <h1>Articles</h1> },
          { path: "articles/new", element: <h1>New article</h1> },
          { path: "articles/:articleId/edit", element: <h1>Edit article</h1> },
          { path: "articles/:articleId/preview", element: <h1>Article preview</h1> },
          { path: "articles/:articleId/versions", element: <h1>Article versions</h1> },
          { path: "publishing", element: <h1>Publishing</h1> },
          { path: "settings/site", element: <h1>Site</h1> },
          { path: "settings/builder", element: <h1>Builder</h1> },
          { path: "settings/hotlink", element: <h1>Hotlink</h1> },
          { path: "*", element: <h1>Page not found</h1> },
        ],
      },
    ],
  },
];

const router = createBrowserRouter(appRoutes);

export function AppRouter() {
  return <RouterProvider router={router} />;
}
