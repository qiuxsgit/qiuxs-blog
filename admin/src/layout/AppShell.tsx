import { useEffect, useRef, useState, type PropsWithChildren } from "react";
import { NavLink } from "react-router-dom";

const navigation = [
  ["Articles", "/articles"],
  ["Publishing", "/publishing"],
  ["Site", "/site"],
  ["Builder", "/builder"],
  ["Hotlink", "/hotlink"],
] as const;

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
  const [drawerOpen, setDrawerOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);

  const closeDrawer = () => {
    setDrawerOpen(false);
    requestAnimationFrame(() => menuButton.current?.focus());
  };

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (drawerOpen && event.key === "Escape") {
        event.preventDefault();
        closeDrawer();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [drawerOpen]);

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">Skip to content</a>
      <header className="app-header">
        <div className="shell-width header-content">
          <NavLink className="brand" to="/articles">QIUXS <span>ADMIN</span></NavLink>
          <button
            aria-controls="admin-navigation-drawer"
            aria-expanded={drawerOpen}
            aria-label="Open navigation"
            className="touch-target menu-button"
            onClick={() => setDrawerOpen(true)}
            ref={menuButton}
            type="button"
          >
            <span aria-hidden="true">☰</span><span className="sr-only">Open navigation</span>
          </button>
        </div>
      </header>
      <div className="shell-width shell-grid">
        <aside className="desktop-sidebar">
          <nav aria-label="Admin"><NavigationLinks /></nav>
        </aside>
        <main id="main-content" tabIndex={-1}>{children}</main>
      </div>
      {drawerOpen && (
        <div aria-label="Admin navigation" aria-modal="true" className="navigation-drawer" id="admin-navigation-drawer" role="dialog">
          <div className="drawer-header">
            <strong>Navigation</strong>
            <button aria-label="Close navigation" className="touch-target" onClick={closeDrawer} type="button">×</button>
          </div>
          <nav aria-label="Admin mobile"><NavigationLinks onNavigate={closeDrawer} /></nav>
        </div>
      )}
    </div>
  );
}
