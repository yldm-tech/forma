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
import { EditProfileDetailsForm, SecurityCard } from "./profile";

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
  hasPassword: identityProvider === "email",
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

describe("SecurityCard", () => {
  /** The card after a marker, so a test knows the route has rendered even when the card renders nothing. */
  const renderCard = async (me: Me) => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    queryClient.setQueryData(authKeys.options, { passwordResetEnabled: true } as AuthOptions);
    const rootRoute = createRootRoute({ component: Outlet });
    const router = createRouter({
      routeTree: rootRoute.addChildren([
        createRoute({
          getParentRoute: () => rootRoute,
          path: "/",
          component: () => (
            <>
              <p>rendered</p>
              <SecurityCard user={me} />
            </>
          ),
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
    await screen.findByText("rendered");
  };
  const twoFactorSwitch = () =>
    screen.queryByRole("switch", { name: t("workspace.settings.profile.two_factor_authentication") });
  const passwordChange = () => screen.queryAllByText(t("workspace.settings.profile.password_change.title"));

  test("offers a password account its password and two-factor authentication", async () => {
    await renderCard(user("email"));

    expect(twoFactorSwitch()).not.toBeNull();
    expect(passwordChange().length).toBeGreaterThan(0);
  });

  test("offers two-factor authentication to an SSO account that still has a password, which signs it in alone", async () => {
    await renderCard({ ...user("sso"), hasPassword: true });

    expect(twoFactorSwitch()).not.toBeNull();
    expect(passwordChange()).toHaveLength(0);
  });

  test("is left out for an SSO account without a password", async () => {
    await renderCard(user("sso"));

    expect(twoFactorSwitch()).toBeNull();
    expect(screen.queryByText(t("common.security"))).toBeNull();
  });
});
