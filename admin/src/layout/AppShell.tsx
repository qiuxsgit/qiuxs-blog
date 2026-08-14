import { useEffect, useRef, useState, type PropsWithChildren } from "react";
import { NavLink } from "react-router-dom";

import { ApiProblem } from "../api/problem";
import { useOptionalAuth } from "../auth/AuthProvider";

const navigation = [
  ["Articles", "/articles"],
  ["Publishing", "/publishing"],
  ["Site", "/settings/site"],
  ["Builder", "/settings/builder"],
  ["Hotlink", "/settings/hotlink"],
] as const;

const tabbableSelector = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";

function tabbableChildren(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(tabbableSelector))
    .filter((element) => element.tabIndex >= 0 && element.getAttribute("aria-hidden") !== "true");
}

function NavigationLinks({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <ul className="navigation-links">
      {navigation.map(([label, to]) => (
        <li key={to}>
          <NavLink className="touch-target navigation-link" onClick={onNavigate} to={to}>{label}</NavLink>
        </li>
      ))}
    </ul>
  );
}

export function AppShell({ children }: PropsWithChildren) {
  const auth = useOptionalAuth();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string>();
  const menuButton = useRef<HTMLButtonElement>(null);
  const drawer = useRef<HTMLDivElement>(null);
  const drawerCloseButton = useRef<HTMLButtonElement>(null);
  const drawerOpener = useRef<HTMLElement | null>(null);

  const closeDrawer = () => {
    setDrawerOpen(false);
  };

  const openDrawer = () => {
    drawerOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : menuButton.current;
    setDrawerOpen(true);
  };

  const logout = async () => {
    if (!auth || loggingOut) return;
    setLoggingOut(true);
    setLogoutError(undefined);
    try {
      await auth.logout();
    } catch (error) {
      setLogoutError(error instanceof ApiProblem ? error.title : "Unable to log out");
      setLoggingOut(false);
    }
  };

  useEffect(() => {
    if (!drawerOpen) {
      drawerOpener.current?.focus();
      drawerOpener.current = null;
      return;
    }
    drawerCloseButton.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeDrawer();
        return;
      }
      if (event.key !== "Tab" || !drawer.current) return;
      const targets = tabbableChildren(drawer.current);
      if (targets.length === 0) return;
      const first = targets[0]!;
      const last = targets.at(-1)!;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [drawerOpen]);

  return (
    <div className="app-shell">
      <div className="app-shell-content" inert={drawerOpen}>
        <a className="skip-link" href="#main-content">Skip to content</a>
        <header className="app-header">
          <div className="shell-width header-content">
            <NavLink className="brand" to="/articles">QIUXS <span>ADMIN</span></NavLink>
            <div className="header-actions">
              {auth?.state.kind === "authenticated" && <span>{auth.state.admin.username}</span>}
              {auth && (
                <button className="button button-secondary touch-target" disabled={loggingOut} onClick={() => void logout()} type="button">
                  {loggingOut ? "Logging out" : "Log out"}
                </button>
              )}
              <button
                aria-controls="admin-navigation-drawer"
                aria-expanded={drawerOpen}
                aria-label="Open navigation"
                className="touch-target menu-button"
                onClick={openDrawer}
                ref={menuButton}
                type="button"
              >
                <span aria-hidden="true">☰</span><span className="sr-only">Open navigation</span>
              </button>
            </div>
          </div>
        </header>
        <div className="shell-width shell-grid">
          <aside className="desktop-sidebar">
            <nav aria-label="Admin"><NavigationLinks /></nav>
          </aside>
          <main id="main-content" tabIndex={-1}>
            {logoutError && <p role="alert">{logoutError}</p>}
            {children}
          </main>
        </div>
      </div>
      {drawerOpen && (
        <div aria-label="Admin navigation" aria-modal="true" className="navigation-drawer" id="admin-navigation-drawer" ref={drawer} role="dialog">
          <div className="drawer-header">
            <strong>Navigation</strong>
            <button aria-label="Close navigation" className="touch-target" onClick={closeDrawer} ref={drawerCloseButton} type="button">×</button>
          </div>
          <nav aria-label="Admin mobile"><NavigationLinks onNavigate={closeDrawer} /></nav>
        </div>
      )}
    </div>
  );
}
