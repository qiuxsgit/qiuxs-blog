import { axe } from "jest-axe";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter, Outlet, RouterProvider, createMemoryRouter } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { AsyncPage } from "../components/AsyncPage";
import { ProblemNotice } from "../components/ProblemNotice";
import { SaveIndicator } from "../components/SaveIndicator";
import { StatusBadge } from "../components/StatusBadge";
import { ApiProblem } from "../api/problem";
import { RouteErrorPage } from "../app/RouteErrorPage";
import { AppShell } from "./AppShell";

const componentStyles = readFileSync(resolve(process.cwd(), "src/styles/components.css"), "utf8");

function renderApp(ui: React.ReactElement, route = "/articles") {
  return render(<MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>);
}

function ThrowingRoute(): never {
  throw new Response("", { status: 500, statusText: "Route exploded" });
}

function ConfirmDialogHarness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)} type="button">Delete article</button>
      <ConfirmDialog confirmLabel="Delete" onCancel={() => setOpen(false)} onConfirm={() => setOpen(false)} open={open} title="Delete article">
        <a href="#details">Details</a>
      </ConfirmDialog>
    </>
  );
}

describe("AppShell", () => {
  afterEach(() => document.body.replaceChildren());

  it("exposes one keyboard-navigable shell", async () => {
    renderApp(<AppShell><h1>Articles</h1></AppShell>);

    expect(screen.getByRole("link", { name: "Skip to content" })).toHaveAttribute("href", "#main-content");
    expect(screen.getByRole("navigation", { name: "Admin" })).toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
    expect(screen.getByRole("link", { name: "Articles" })).toHaveAttribute("aria-current", "page");
    expect((await axe(document.body)).violations).toEqual([]);
  });

  it("contains router errors in the one shell main landmark", async () => {
    const router = createMemoryRouter([
      {
        path: "/",
        element: <AppShell><Outlet /></AppShell>,
        errorElement: <AppShell><RouteErrorPage /></AppShell>,
        children: [{ index: true, element: <ThrowingRoute /> }],
      },
    ]);
    render(<RouterProvider router={router} />);

    await screen.findByRole("heading", { name: "Unable to load this page" });
    expect(screen.getAllByRole("main")).toHaveLength(1);
    expect(document.querySelectorAll("#main-content")).toHaveLength(1);
    expect((await axe(document.body)).violations).toEqual([]);
  });

  it("offers every destination and enforces the drawer focus contract", async () => {
    const user = userEvent.setup();
    renderApp(<AppShell><h1>Articles</h1></AppShell>);

    for (const label of ["Articles", "Publishing", "Site", "Builder", "Hotlink"]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("link", { name: "Site" })).toHaveAttribute("href", "/settings/site");
    expect(screen.getByRole("link", { name: "Builder" })).toHaveAttribute("href", "/settings/builder");
    expect(screen.getByRole("link", { name: "Hotlink" })).toHaveAttribute("href", "/settings/hotlink");

    const menu = screen.getByRole("button", { name: "Open navigation" });
    expect(menu).toHaveAttribute("aria-expanded", "false");
    await user.click(menu);
    expect(menu).toHaveAttribute("aria-expanded", "true");
    const drawer = screen.getByRole("dialog", { name: "Admin navigation" });
    const close = within(drawer).getByRole("button", { name: "Close navigation" });
    const lastLink = within(drawer).getByRole("link", { name: "Hotlink" });
    expect(close).toHaveFocus();
    expect(screen.getByRole("banner").closest("[inert]")).toBeInTheDocument();
    await user.keyboard("{Shift>}{Tab}{/Shift}");
    expect(lastLink).toHaveFocus();
    await user.keyboard("{Tab}");
    expect(close).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(menu).toHaveFocus();
    expect(menu).toHaveAttribute("aria-expanded", "false");
  });

  it("announces loading and saving state", () => {
    renderApp(<><AsyncPage loading label="Loading articles"><p>Ready</p></AsyncPage><SaveIndicator state="saving" /></>);

    expect(screen.getByRole("status", { name: "Loading articles" })).toHaveTextContent("Loading articles");
    expect(screen.getByRole("status", { name: "Saving changes" })).toHaveTextContent("Saving changes");
  });

  it("presents a known builder conflict without relying on color", () => {
    renderApp(<ProblemNotice problem={new ApiProblem(409, "builder_conflict", "req-builder", "Builder conflict")} />);

    const notice = screen.getByRole("alert");
    expect(notice).toHaveTextContent("Builder configuration changed elsewhere");
    expect(notice).toHaveTextContent("Request ID: req-builder");
    expect(within(notice).getByText("⚠", { selector: "span" })).toHaveAttribute("aria-hidden", "true");
  });

  it("keeps an unknown service problem generic and traceable", () => {
    renderApp(<ProblemNotice problem={new ApiProblem(503, "future_service_code", "req-future", "Future service unavailable")} />);

    const notice = screen.getByRole("alert");
    expect(notice).toHaveTextContent("Future service unavailable");
    expect(notice).toHaveTextContent("Code: future_service_code");
    expect(notice).toHaveTextContent("Request ID: req-future");
  });

  it("restores trigger focus and traps every confirmation control", async () => {
    const user = userEvent.setup();
    renderApp(<ConfirmDialogHarness />);

    const trigger = screen.getByRole("button", { name: "Delete article" });
    await user.click(trigger);
    const dialog = screen.getByRole("alertdialog", { name: "Delete article" });
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toHaveFocus();
    await user.keyboard("{Tab}");
    expect(within(dialog).getByRole("button", { name: "Delete" })).toHaveFocus();
    await user.keyboard("{Tab}");
    expect(within(dialog).getByRole("link", { name: "Details" })).toHaveFocus();
    await user.keyboard("{Shift>}{Tab}{/Shift}");
    expect(within(dialog).getByRole("button", { name: "Delete" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
  });

  it("announces status badge transitions", () => {
    const { rerender } = render(<StatusBadge tone="pending" />);

    expect(screen.getByRole("status")).toHaveTextContent("Pending");
    rerender(<StatusBadge tone="success" />);
    expect(screen.getByRole("status")).toHaveTextContent("Success");
  });

  it("keeps 44px targets and exposes the menu at the narrow breakpoint", () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 600 });
    renderApp(<AppShell><h1>Articles</h1></AppShell>);

    expect(window.innerWidth).toBeLessThanOrEqual(768);
    for (const target of [screen.getByRole("button", { name: "Open navigation" }), screen.getByRole("link", { name: "Articles" })]) {
      expect(target).toHaveClass("touch-target");
    }
    expect(componentStyles).toMatch(/\.touch-target\s*\{[^}]*min-height:\s*44px[^}]*min-width:\s*44px/s);
    expect(componentStyles).toMatch(/@media\s*\(max-width:\s*48rem\)\s*\{[\s\S]*?\.menu-button\s*\{[^}]*display:\s*inline-flex/s);
  });
});
