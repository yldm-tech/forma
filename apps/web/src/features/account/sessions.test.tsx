import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { UserSession } from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { SessionsCard } from "./sessions";

// The sessions card on the profile page, against GET /api/app/auth/sessions, DELETE /api/app/auth/sessions/:id and POST /api/app/auth/sessions/revoke-others (apps/server/internal/auth/handler.go).

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods; these stand in for what the Dialog reads of them.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const MAC_CHROME =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36";
const IPHONE_SAFARI =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";

const session = (overrides: Partial<UserSession>): UserSession => ({
  id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01",
  current: false,
  createdAt: "2026-10-01T09:30:00Z",
  lastActiveAt: "2026-10-02T10:45:00Z",
  expiresAt: "2026-10-03T09:30:00Z",
  ipAddress: "198.51.100.7",
  userAgent: MAC_CHROME,
  ...overrides,
});

const CURRENT = session({
  id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d00",
  current: true,
  ipAddress: "203.0.113.10",
});
const PHONE = session({ id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d02", userAgent: IPHONE_SAFARI });
const UNKNOWN = session({ id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d03", userAgent: null, ipAddress: null });

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server's sessions: DELETE ends one (204, or the given failure) and revoke-others ends all but the current one. */
const stubServer = (sessions: UserSession[], failDelete?: Response) => {
  let current = sessions;
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path === "/api/app/auth/sessions" && init.method === "GET") return json(200, { sessions: current });
      if (path === "/api/app/auth/sessions/revoke-others" && init.method === "POST") {
        calls.push("revoke-others");
        const revoked = current.filter((s) => !s.current).length;
        current = current.filter((s) => s.current);
        return json(200, { revoked });
      }
      if (path.startsWith("/api/app/auth/sessions/") && init.method === "DELETE") {
        const id = path.split("/").pop() ?? "";
        calls.push(`delete ${id}`);
        if (failDelete) return failDelete;
        current = current.filter((s) => s.id !== id);
        return new Response(null, { status: 204 });
      }
      return json(404, { code: "not_found", message: path });
    })
  );
  return calls;
};

const renderCard = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SessionsCard locale="en-US" />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const rows = async () => (await screen.findAllByRole("row")).slice(1);
const signOutAll = () => t("workspace.settings.profile.sessions.sign_out_other_sessions");

describe("SessionsCard", () => {
  test("lists each session by browser, system and address, and marks this browser's", async () => {
    stubServer([CURRENT, PHONE, UNKNOWN]);
    renderCard();

    const [mine, phone, unknown] = (await rows()) as [HTMLElement, HTMLElement, HTMLElement];
    expect(within(mine).getByText("Chrome on macOS")).toBeTruthy();
    // The badge keeps to one line however narrow the Device column gets.
    expect(within(mine).getByText(t("workspace.settings.profile.sessions.this_device")).className).toContain(
      "whitespace-nowrap"
    );
    expect(within(mine).getByText("203.0.113.10")).toBeTruthy();
    expect(within(mine).getByText(t("workspace.settings.profile.sessions.active_now"))).toBeTruthy();
    // This browser signs out from the account menu, not here.
    expect(within(mine).queryByRole("button")).toBeNull();

    expect(within(phone).getByText("Safari on iOS")).toBeTruthy();
    expect(within(phone).getByText("198.51.100.7")).toBeTruthy();
    expect(within(phone).getByRole("button", { name: "Sign out Safari on iOS" })).toBeTruthy();

    expect(within(unknown).getByText(t("workspace.settings.profile.sessions.unknown_device"))).toBeTruthy();
    expect(within(unknown).getByText(t("workspace.settings.profile.sessions.unknown"))).toBeTruthy();
  });

  test("signs one other session out and drops it from the list", async () => {
    const calls = stubServer([CURRENT, PHONE]);
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Sign out Safari on iOS" }));

    await waitFor(() => expect(calls).toEqual([`delete ${PHONE.id}`]));
    await waitFor(async () => expect(await rows()).toHaveLength(1));
    expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
      t("workspace.settings.profile.sessions.session_signed_out")
    );
    // With no other session left, there is nothing to sign out everywhere else.
    expect(screen.queryByRole("button", { name: signOutAll() })).toBeNull();
  });

  test("a failed sign-out shows the server's message and keeps the session", async () => {
    stubServer([CURRENT, PHONE], json(404, { code: "not_found", message: "Session not found" }));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Sign out Safari on iOS" }));

    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toContain("Session not found")
    );
    expect(await rows()).toHaveLength(2);
  });

  test("signs every other session out after a confirmation that says how many", async () => {
    const calls = stubServer([CURRENT, PHONE, UNKNOWN]);
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: signOutAll() }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(
        t("workspace.settings.profile.sessions.sign_out_other_sessions_confirmation", { count: 2 })
      )
    ).toBeTruthy();
    expect(calls).toEqual([]);
    fireEvent.click(within(dialog).getByRole("button", { name: signOutAll() }));

    await waitFor(() => expect(calls).toEqual(["revoke-others"]));
    await waitFor(async () => expect(await rows()).toHaveLength(1));
    expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
      t("workspace.settings.profile.sessions.other_sessions_signed_out", { count: 2 })
    );
  });

  test("offers no sign-out everywhere else when this is the only session", async () => {
    stubServer([CURRENT]);
    renderCard();

    expect(await rows()).toHaveLength(1);
    expect(screen.queryByRole("button", { name: signOutAll() })).toBeNull();
  });
});
