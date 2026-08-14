import { Navigate, Outlet, RouterProvider, createBrowserRouter } from "react-router-dom";

import { AppShell } from "../layout/AppShell";
import { RouteErrorPage } from "./RouteErrorPage";

function ShellLayout() {
  return <AppShell><Outlet /></AppShell>;
}

function ShellRouteErrorPage() {
  return <AppShell><RouteErrorPage /></AppShell>;
}

const router = createBrowserRouter([
  {
    path: "/",
    element: <ShellLayout />,
    errorElement: <ShellRouteErrorPage />,
    children: [
      { index: true, element: <Navigate to="/articles" replace /> },
      { path: "articles", element: <h1>Articles</h1> },
      { path: "publishing", element: <h1>Publishing</h1> },
      { path: "site", element: <h1>Site</h1> },
      { path: "builder", element: <h1>Builder</h1> },
      { path: "hotlink", element: <h1>Hotlink</h1> },
      { path: "*", element: <h1>Page not found</h1> },
    ],
  },
]);

export function AppRouter() {
  return <RouterProvider router={router} />;
}
