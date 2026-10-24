import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { type AuthOptions, type Me, authKeys } from "../../lib/queries/auth";
import { EditProfileDetailsForm } from "./profile";

// The address on the profile form: a change is confirmed from the new mailbox where the instance verifies addresses, so without SMTP it cannot be made here.

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const user = (identityProvider: Me["identityProvider"]): Me => ({
  id: "u1",
  name: "Ada",
  email: "ada@example.com",
  emailVerified: true,
  locale: "en-US",
  identityProvider,
  twoFactorEnabled: false,
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: null,
});

/** The form with the instance's options already known, as the settings shell would have loaded them; the form asks the server nothing else on render. */
const renderForm = (me: Me, options: Pick<AuthOptions, "emailVerificationRequired" | "smtpConfigured">) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      throw new Error(`unexpected ${path}`);
    })
  );
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(authKeys.options, { passwordResetEnabled: true, ...options } as AuthOptions);
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => <EditProfileDetailsForm user={me} />,
      }),
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

const emailField = async () => (await screen.findByLabelText(t("common.email"))) as HTMLInputElement;
const hint = () => t("workspace.settings.profile.email_change_needs_email");

describe("EditProfileDetailsForm email address", () => {
  test("is read-only, and says why, where the verification mail cannot be sent", async () => {
    renderForm(user("email"), { emailVerificationRequired: true, smtpConfigured: false });

    const field = await emailField();
    expect(field.disabled).toBe(true);
    const described = document.getElementById(field.getAttribute("aria-describedby") ?? "");
    expect(described?.textContent).toBe(hint());
  });

  test("can be changed where the instance sends email", async () => {
    renderForm(user("email"), { emailVerificationRequired: true, smtpConfigured: true });

    expect((await emailField()).disabled).toBe(false);
    expect(screen.queryByText(hint())).toBeNull();
  });

  test("can be changed without SMTP where the instance does not verify addresses", async () => {
    renderForm(user("email"), { emailVerificationRequired: false, smtpConfigured: false });

    expect((await emailField()).disabled).toBe(false);
    expect(screen.queryByText(hint())).toBeNull();
  });
});
