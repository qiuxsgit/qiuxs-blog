import { axe } from "jest-axe";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { AsyncPage } from "../components/AsyncPage";
import { ProblemNotice } from "../components/ProblemNotice";
import { SaveIndicator } from "../components/SaveIndicator";
import { ApiProblem } from "../api/problem";
import { AppShell } from "./AppShell";

function renderApp(ui: React.ReactElement, route = "/articles") {
  return render(<MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>);
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

  it("offers every desktop destination and returns focus after an escape-closed drawer", async () => {
    const user = userEvent.setup();
    renderApp(<AppShell><h1>Articles</h1></AppShell>);

    for (const label of ["Articles", "Publishing", "Site", "Builder", "Hotlink"]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }

    const menu = screen.getByRole("button", { name: "Open navigation" });
    expect(menu).toHaveAttribute("aria-expanded", "false");
    await user.click(menu);
    expect(menu).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("dialog", { name: "Admin navigation" })).toBeInTheDocument();
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

  it("traps confirmation focus and only cancels after the dialog closes", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();
    renderApp(<ConfirmDialog open title="Delete article" confirmLabel="Delete" onConfirm={() => undefined} onCancel={onCancel}>This cannot be undone.</ConfirmDialog>);

    const dialog = screen.getByRole("alertdialog", { name: "Delete article" });
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toHaveFocus();
    await user.keyboard("{Tab}");
    expect(within(dialog).getByRole("button", { name: "Delete" })).toHaveFocus();
    await user.keyboard("{Tab}");
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("keeps interactive targets at least 44px tall on narrow screens", () => {
    renderApp(<AppShell><h1>Articles</h1></AppShell>);

    for (const target of [screen.getByRole("button", { name: "Open navigation" }), screen.getByRole("link", { name: "Articles" })]) {
      expect(target).toHaveClass("touch-target");
    }
  });
});
