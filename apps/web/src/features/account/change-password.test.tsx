import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { type AuthOptions, authKeys } from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { ChangePassword } from "./change-password";

// The password part of the profile page's Security card, against POST /api/app/auth/password/change (apps/server/internal/auth/password.go).

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The change endpoint answering with `answer`; every body it receives is kept. */
const stubServer = (answer: () => Response) => {
  const sent: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path === "/api/app/auth/password/change" && init.method === "POST") {
        sent.push(JSON.parse(init.body as string));
        return answer();
      }
      throw new Error(`unexpected ${path}`);
    })
  );
  return sent;
};

const renderForm = (options: Pick<AuthOptions, "passwordResetEnabled" | "smtpConfigured">) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(authKeys.options, options as AuthOptions);
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: ChangePassword }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const field = async (label: string) => (await screen.findByLabelText(label)) as HTMLInputElement;
const current = () => field(t("workspace.settings.profile.password_change.current_password"));
const next = () => field(t("auth.forgot-password.reset.new_password"));
const confirm = () => field(t("auth.forgot-password.reset.confirm_password"));
const submit = () =>
  fireEvent.click(
    screen.getByRole("button", { name: t("workspace.settings.profile.password_change.submit") })
  );

const fill = async (values: { current: string; next: string; confirm: string }) => {
  fireEvent.change(await current(), { target: { value: values.current } });
  fireEvent.change(await next(), { target: { value: values.next } });
  fireEvent.change(await confirm(), { target: { value: values.confirm } });
};

/** The message the field's aria-describedby points at. */
const errorOf = (input: HTMLInputElement) =>
  document.getElementById(input.getAttribute("aria-describedby") ?? "")?.textContent;

describe("ChangePassword", () => {
  test("says that other sessions will be signed out", async () => {
    stubServer(() => new Response(null, { status: 204 }));
    renderForm({ passwordResetEnabled: true, smtpConfigured: false });

    expect(
      await screen.findByText(t("workspace.settings.profile.password_change.other_sessions_signed_out_hint"))
    ).toBeTruthy();
  });

  test("checks the fields before sending anything", async () => {
    const sent = stubServer(() => new Response(null, { status: 204 }));
    renderForm({ passwordResetEnabled: true, smtpConfigured: false });

    await current();
    submit();
    await waitFor(async () =>
      expect(errorOf(await current())).toBe(
        t("workspace.settings.profile.password_change.current_password_required")
      )
    );
    expect(errorOf(await next())).toBe(t("auth.validation.password_too_short", { min: 8 }));

    await fill({ current: "old", next: "NewPassword1", confirm: "NewPassword2" });
    submit();
    await waitFor(async () =>
      expect(errorOf(await confirm())).toBe(t("auth.forgot-password.reset.passwords_do_not_match"))
    );
    expect(errorOf(await current())).toBeUndefined();
    expect(sent).toEqual([]);
  });

  test("changes the password and starts the form over", async () => {
    const sent = stubServer(() => new Response(null, { status: 204 }));
    renderForm({ passwordResetEnabled: true, smtpConfigured: false });

    await fill({ current: "old password", next: "NewPassword1", confirm: "NewPassword1" });
    submit();

    await waitFor(() =>
      expect(sent).toEqual([{ currentPassword: "old password", newPassword: "NewPassword1" }])
    );
    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
        t("workspace.settings.profile.password_change.password_changed")
      )
    );
    await waitFor(async () => expect((await current()).value).toBe(""));
    expect((await next()).value).toBe("");
    expect((await confirm()).value).toBe("");
  });

  test("puts a wrong current password under that field", async () => {
    stubServer(() => json(400, { code: "invalid_password", message: "Invalid password" }));
    renderForm({ passwordResetEnabled: true, smtpConfigured: false });

    await fill({ current: "wrong", next: "NewPassword1", confirm: "NewPassword1" });
    submit();

    await waitFor(async () =>
      expect(errorOf(await current())).toBe(
        t("workspace.settings.profile.password_change.incorrect_current_password")
      )
    );
    expect((await next()).value).toBe("NewPassword1");
  });

  test("puts a breached new password under that field", async () => {
    stubServer(() => json(400, { code: "password_compromised", message: "breached" }));
    renderForm({ passwordResetEnabled: true, smtpConfigured: false });

    await fill({ current: "old password", next: "Password123", confirm: "Password123" });
    submit();

    await waitFor(async () => expect(errorOf(await next())).toBe(t("auth.password_compromised")));
  });

  test("offers the emailed reset link only where the instance sends email", async () => {
    stubServer(() => new Response(null, { status: 204 }));
    renderForm({ passwordResetEnabled: true, smtpConfigured: false });
    await current();
    const resetLink = t("workspace.settings.profile.password_change.email_reset_link");
    expect(screen.queryByRole("button", { name: resetLink })).toBeNull();
    cleanup();

    renderForm({ passwordResetEnabled: true, smtpConfigured: true });
    expect(await screen.findByRole("button", { name: resetLink })).toBeTruthy();
    cleanup();

    renderForm({ passwordResetEnabled: false, smtpConfigured: true });
    await current();
    expect(screen.queryByRole("button", { name: resetLink })).toBeNull();
  });
});
