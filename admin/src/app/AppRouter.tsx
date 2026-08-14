import { Navigate, Outlet, RouterProvider, createBrowserRouter, type RouteObject } from "react-router-dom";

import { LoginPage } from "../auth/LoginPage";
import { ArticleListPage } from "../articles/ArticleListPage";
import { RequireSession } from "../auth/RequireSession";
import { AppShell } from "../layout/AppShell";
import { ArticleEditorPage } from "../editor/ArticleEditorPage";
import { RouteErrorPage } from "./RouteErrorPage";
import { ArticlePreviewPage } from "../preview/ArticlePreviewPage";
import { ArticleVersionsPage } from "../versions/ArticleVersionsPage";
import { PublishingPage } from "../publishing/PublishingPage";
import { SiteSettingsPage } from "../settings/SiteSettingsPage";

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
          { path: "articles", element: <ArticleListPage /> },
          { path: "articles/new", element: <ArticleEditorPage /> },
          { path: "articles/:articleId/edit", element: <ArticleEditorPage /> },
          { path: "articles/:articleId/preview", element: <ArticlePreviewPage /> },
          { path: "articles/:articleId/versions", element: <ArticleVersionsPage /> },
          { path: "publishing", element: <PublishingPage /> },
          { path: "settings/site", element: <SiteSettingsPage /> },
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
