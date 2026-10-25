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
import { Suspense } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { type AuthOptions, type InviteStatus, authKeys } from "../api";
import { LAST_USED_KEY } from "../last-used";
import { SignupForm, type SignupFormSearch } from "./signup-form";

// forma2's signup-form.tsx: Forma Cloud offers the product update emails where other instances offer the security ones, the choice is sent with the sign-up, and TERMS_URL and PRIVACY_URL are linked under the form when set.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
  globalThis.history.replaceState(null, "", "/");
});

const OPTIONS: AuthOptions = {
  emailAuthEnabled: true,
  signupEnabled: true,
  passwordResetEnabled: true,
  emailVerificationRequired: true,
  smtpConfigured: true,
  ssoProviders: [],
  isFormaCloud: false,
};

/** POST /api/app/auth/signup answering success; the bodies are recorded. */
const stubSignup = () => {
  const bodies: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path !== "/api/app/auth/signup") throw new Error(`unexpected ${path}`);
      bodies.push(JSON.parse(init.body as string) as Record<string, unknown>);
      return new Response(JSON.stringify({ success: true }), {
        headers: { "content-type": "application/json" },
      });
    })
  );
  return bodies;
};

/** POST /api/app/auth/signup answering success, POST /api/app/auth/sign-in answering `signIn` (a status, or an error status) and POST /api/app/invites/inspect answering `invite`; the requests are recorded in order. */
const stubSignupAndSignIn = (signIn: "signed_in" | "two_factor_required" | 401, invite?: InviteStatus) => {
  const requests: { path: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      requests.push({ path, body: JSON.parse(init.body as string) as Record<string, unknown> });
      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
      if (path === "/api/app/auth/signup") return json({ success: true });
      if (path === "/api/app/invites/inspect" && invite) return json({ status: invite });
      if (path !== "/api/app/auth/sign-in") throw new Error(`unexpected ${path}`);
      return signIn === 401
        ? json({ code: "invalid_credentials", message: "Invalid email or password" }, 401)
        : json({ status: signIn });
    })
  );
  return requests;
};

/** The form on /auth/signup in a router served under the app's base path, as in the browser, beside the root page a sign-in lands on, the invite page and the two notices a sign-up can end on. */
const renderForm = (options: Partial<AuthOptions> = {}, search: SignupFormSearch = {}) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(authKeys.options, { ...OPTIONS, ...options });
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/auth/signup",
      component: () => (
        <Suspense fallback={null}>
          <SignupForm search={search} />
        </Suspense>
      ),
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <h1>Home</h1>,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/invite",
      component: () => <h1>Invite</h1>,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/auth/verification-requested",
      component: () => <h1>Verification requested</h1>,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/auth/signup-without-verification-success",
      component: () => <h1>Signup notice</h1>,
    }),
  ]);
  const router = createRouter({
    routeTree,
    basepath: "/app",
    history: createMemoryHistory({ initialEntries: ["/app/auth/signup"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const fillAndSubmit = async (optIn?: string) => {
  // With email the only way in, the fields show at once.
  await screen.findByLabelText(i18n.t("common.full_name"));
  fireEvent.change(screen.getByLabelText(i18n.t("common.full_name")), { target: { value: "Ada Lovelace" } });
  fireEvent.change(screen.getByLabelText(i18n.t("common.email")), { target: { value: "Ada@Example.com" } });
  fireEvent.change(screen.getByLabelText(i18n.t("common.password")), {
    target: { value: "Correct-Horse-1" },
  });
  if (optIn) fireEvent.click(screen.getByRole("checkbox", { name: new RegExp(optIn) }));
  fireEvent.click(screen.getByRole("button", { name: i18n.t("auth.continue_with_email") }));
};

describe("the mailing list opt-in", () => {
  test("is the security updates off Forma Cloud", async () => {
    const bodies = stubSignup();
    renderForm();
    await fillAndSubmit(i18n.t("auth.signup.security_updates_title"));
    expect(
      screen.queryByRole("checkbox", { name: new RegExp(i18n.t("auth.signup.product_updates_title")) })
    ).toBeNull();
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      email: "ada@example.com",
      subscribeToSecurityUpdates: true,
      subscribeToProductUpdates: false,
    });
  });

  test("is the monthly product updates on Forma Cloud", async () => {
    const bodies = stubSignup();
    renderForm({ isFormaCloud: true });
    await fillAndSubmit(i18n.t("auth.signup.product_updates_title"));
    expect(
      screen.queryByRole("checkbox", { name: new RegExp(i18n.t("auth.signup.security_updates_title")) })
    ).toBeNull();
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ subscribeToSecurityUpdates: false, subscribeToProductUpdates: true });
  });
});

describe("the legal links", () => {
  test("link the instance's terms and privacy policy in a new tab", async () => {
    renderForm({ termsUrl: "https://forma.example/terms", privacyUrl: "https://forma.example/privacy" });
    const terms = await screen.findByRole("link", { name: i18n.t("auth.signup.terms_of_service") });
    const privacy = screen.getByRole("link", { name: i18n.t("auth.signup.privacy_policy") });
    expect(terms.getAttribute("href")).toBe("https://forma.example/terms");
    expect(terms.getAttribute("target")).toBe("_blank");
    expect(privacy.getAttribute("href")).toBe("https://forma.example/privacy");
  });

  test("show only what is configured", async () => {
    renderForm({ privacyUrl: "https://forma.example/privacy" });
    await screen.findByRole("link", { name: i18n.t("auth.signup.privacy_policy") });
    expect(screen.queryByRole("link", { name: i18n.t("auth.signup.terms_of_service") })).toBeNull();
  });

  test("are absent when neither is set", async () => {
    renderForm();
    await screen.findByRole("button", { name: i18n.t("auth.continue_with_email") });
    expect(screen.queryByRole("link", { name: i18n.t("auth.signup.privacy_policy") })).toBeNull();
  });
});

test("the sign-in link carries the page's marketing attribution", async () => {
  globalThis.history.replaceState(null, "", "/auth/signup?utm_source=newsletter&email=ada%40example.com");
  renderForm();
  const login = await screen.findByRole("link", { name: i18n.t("auth.signup.log_in") });
  // The router serves the app under its base path, as in the browser.
  expect(login.getAttribute("href")).toBe("/app/auth/login?utm_source=newsletter");
});

describe("the email form", () => {
  test("is revealed beside SSO by a click that submits nothing", async () => {
    renderForm({ ssoProviders: ["google"] });
    const reveal = await screen.findByRole("button", { name: i18n.t("auth.continue_with_email") });
    expect(screen.queryByLabelText(i18n.t("common.full_name"))).toBeNull();
    fireEvent.click(reveal);
    await screen.findByLabelText(i18n.t("common.full_name"));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  test("says what is wrong on a refused submit and focuses the first field it names", async () => {
    const bodies = stubSignup();
    renderForm();
    fireEvent.change(await screen.findByLabelText(i18n.t("common.full_name")), {
      target: { value: "Ada Lovelace" },
    });
    fireEvent.change(screen.getByLabelText(i18n.t("common.email")), { target: { value: "bad-email" } });
    const submit = screen.getByRole("button", { name: i18n.t("auth.continue_with_email") });
    expect((submit as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(submit);
    const email = screen.getByLabelText(i18n.t("common.email"));
    await waitFor(() => expect(document.activeElement).toBe(email));
    expect(email.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByText(i18n.t("auth.validation.invalid_email"))).toBeTruthy();
    expect(screen.getByText(i18n.t("auth.validation.password_too_short", { min: 8 }))).toBeTruthy();
    expect(bodies).toHaveLength(0);
  });

  test("trims the email when the field is left", async () => {
    renderForm();
    const email = await screen.findByLabelText(i18n.t("common.email"));
    fireEvent.change(email, { target: { value: " ada@example.com " } });
    fireEvent.blur(email);
    await waitFor(() => expect((email as HTMLInputElement).value).toBe("ada@example.com"));
  });
});

describe("with email verification off", () => {
  test("a sign-up signs in with the same credentials and goes where a sign-in goes", async () => {
    const requests = stubSignupAndSignIn("signed_in");
    renderForm({ emailVerificationRequired: false });
    await fillAndSubmit();

    expect(await screen.findByRole("heading", { name: "Home" })).toBeTruthy();
    expect(requests.map((request) => request.path)).toEqual([
      "/api/app/auth/signup",
      "/api/app/auth/sign-in",
    ]);
    expect(requests[1]?.body).toEqual({ email: "ada@example.com", password: "Correct-Horse-1" });
    // As after a sign-in on the login page, the next visit opens on the email form.
    expect(localStorage.getItem(LAST_USED_KEY)).toBe("Email");
  });

  test.each([
    // The sign-up created the account and redeemed the invite, so its link would only say it is used up.
    ["not_found", "Home"],
    // The address already had an account, so the invite still waits to be accepted on its page.
    ["valid", "Invite"],
  ] as const)("an invited sign-up whose invite inspects as %s goes to %s", async (invite, page) => {
    const requests = stubSignupAndSignIn("signed_in", invite);
    renderForm({ emailVerificationRequired: false }, { inviteToken: "tok" });
    await fillAndSubmit();

    expect(await screen.findByRole("heading", { name: page })).toBeTruthy();
    expect(requests.map((request) => request.path)).toEqual([
      "/api/app/auth/signup",
      "/api/app/auth/sign-in",
      "/api/app/invites/inspect",
    ]);
    expect(requests[2]?.body).toEqual({ token: "tok" });
  });

  test.each(["two_factor_required", 401] as const)(
    "a sign-in that does not open a session (%s) leaves the visitor on the notice that asks them to log in",
    async (signIn) => {
      stubSignupAndSignIn(signIn);
      renderForm({ emailVerificationRequired: false });
      await fillAndSubmit();

      expect(await screen.findByRole("heading", { name: "Signup notice" })).toBeTruthy();
    }
  );
});

test("with email verification on, a sign-up does not try to sign in", async () => {
  const requests = stubSignupAndSignIn("signed_in");
  renderForm({ emailVerificationRequired: true });
  await fillAndSubmit();

  expect(await screen.findByRole("heading", { name: "Verification requested" })).toBeTruthy();
  expect(requests.map((request) => request.path)).toEqual(["/api/app/auth/signup"]);
});

test("a sign-up sends the browser's time zone for the organization it creates", async () => {
  const requests = stubSignupAndSignIn("signed_in");
  renderForm();
  await fillAndSubmit();

  await waitFor(() => expect(requests.length).toBeGreaterThan(0));
  expect(requests[0]?.body.timeZone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone);
});
