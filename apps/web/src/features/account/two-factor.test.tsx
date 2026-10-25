import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { Me } from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { AccountSecurity } from "./two-factor";

// The enrolment dialog: its description follows the step, the code boxes take focus, a wrong code is reported beside them, and the backup codes cannot be dismissed by accident.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const USER: Me = {
  id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d10",
  name: "Ada",
  email: "ada@example.com",
  emailVerified: true,
  locale: "en-US",
  identityProvider: "email",
  twoFactorEnabled: false,
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: null,
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The setup and enable endpoints; the first code sent is refused. */
const stubTwoFactor = () => {
  let attempts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === "/api/app/auth/two-factor/setup") {
        return json(200, {
          secret: "SECRET",
          keyUri: "otpauth://totp/x",
          dataUri: "data:image/png;base64,",
          backupCodes: ["aaaaabbbbb", "cccccddddd"],
        });
      }
      if (path === "/api/app/auth/two-factor/enable") {
        attempts++;
        return attempts === 1
          ? json(400, { code: "invalid_code", message: "Invalid code" })
          : json(200, { message: "Two factor authentication enabled" });
      }
      if (path === "/api/app/auth/me") return json(200, USER);
      throw new Error(`unexpected ${path}`);
    })
  );
};

const digitBoxes = () =>
  screen.getAllByRole("textbox", { name: /digit/i }).filter((box) => box instanceof HTMLInputElement);

/** Presses Escape on the focused element and tells whether something held back its default action, the browser's close request. */
const pressEscape = (): boolean => {
  const event = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
  (document.activeElement ?? document.body).dispatchEvent(event);
  return event.defaultPrevented;
};

test("enrolment walks the steps with focus, inline errors and backup codes that only an explicit choice closes", async () => {
  stubTwoFactor();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AccountSecurity user={USER} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  fireEvent.click(screen.getByRole("switch"));
  const dialog = await screen.findByRole("dialog");
  const description = () => document.getElementById(dialog.getAttribute("aria-describedby") ?? "");
  expect(description()?.textContent).toBe(
    i18n.t("workspace.settings.profile.confirm_your_current_password_to_get_started")
  );

  fireEvent.change(within(dialog).getByLabelText(i18n.t("common.password")), { target: { value: "secret" } });
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("common.confirm") }));
  await waitFor(() =>
    expect(description()?.textContent).toBe(
      i18n.t("workspace.settings.profile.scan_the_qr_code_below_with_your_authenticator_app")
    )
  );
  // One heading only: the dialog's title, no second one inside the step.
  expect(within(dialog).queryAllByRole("heading")).toHaveLength(1);

  // Before the codes exist, Escape is left to close the dialog.
  expect(pressEscape()).toBe(false);

  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("common.next") }));
  await waitFor(() => expect(document.activeElement).toBe(digitBoxes()[0]));
  expect(description()?.textContent).toBe(
    i18n.t("workspace.settings.profile.enter_the_code_from_your_authenticator_app_below")
  );
  // Turning 2FA on signs out every other session, and the step that does it says so beforehand.
  expect(
    within(dialog).getByText(i18n.t("workspace.settings.profile.two_factor_signs_out_other_sessions"))
  ).toBeTruthy();

  fireEvent.change(digitBoxes()[0] as HTMLInputElement, { target: { value: "123456" } });
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("common.confirm") }));
  expect((await within(dialog).findByRole("alert")).textContent).toBe("Invalid code");
  await waitFor(() => expect(document.activeElement).toBe(digitBoxes()[0]));
  expect(digitBoxes().every((box) => (box as HTMLInputElement).value === "")).toBe(true);

  fireEvent.change(digitBoxes()[0] as HTMLInputElement, { target: { value: "654321" } });
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("common.confirm") }));
  await waitFor(() =>
    expect(description()?.textContent).toBe(
      i18n.t("workspace.settings.profile.save_the_following_backup_codes_in_a_safe_place")
    )
  );
  // The confirmation is the app's own, in the user's language, and repeats that the other sessions were signed out.
  expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
    i18n.t("workspace.settings.profile.two_factor_enabled")
  );
  // The success toast carries a close button of its own; with it gone, no close button may be left.
  act(() => {
    for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  });
  await waitFor(() =>
    expect(within(dialog).queryByRole("button", { name: i18n.t("common.close") })).toBeNull()
  );
  // Escape is the dialog's cancel event; it must leave the codes on screen.
  fireEvent(dialog, new Event("cancel", { cancelable: true }));
  expect(screen.getByRole("dialog")).toBe(dialog);
  // Chromium closes a dialog regardless after repeated Escapes, so the key itself is held back, however often it is pressed.
  expect([pressEscape(), pressEscape(), pressEscape()]).toEqual([true, true, true]);
  expect(screen.getByRole("dialog")).toBe(dialog);
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("auth.two_factor.i_saved_my_codes") }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  // Gone with the dialog: Escape works as usual again.
  expect(pressEscape()).toBe(false);
});
