import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { Suspense } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { BillingPage, CatalogItem } from "../../lib/queries/billing";
import { toast } from "../../lib/toast";
import { stringParam } from "../auth/search";
import { BillingSettingsPage } from "./billing-page";
import type { BillingSearch } from "./components/pricing-table";
import { confirmUpgradePayment } from "./lib/stripe";

vi.mock("./lib/stripe", () => ({ confirmUpgradePayment: vi.fn() }));

const ORG = "0190a000-0000-7000-8000-000000000001";
const BILLING_PATH = `/organizations/${ORG}/settings/billing`;
const API = `/api/app/organizations/${ORG}/billing`;

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no modal dialogs; showing one is setting its open attribute.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  window.scrollTo = () => {};
});

let assign: ReturnType<typeof vi.fn>;

beforeEach(() => {
  sessionStorage.clear();
  assign = vi.fn();
  vi.stubGlobal("location", { ...globalThis.location, assign });
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
  vi.mocked(confirmUpgradePayment).mockReset();
});

const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

const item = (
  plan: CatalogItem["plan"],
  interval: CatalogItem["interval"],
  unitAmount: number
): CatalogItem => ({
  plan,
  interval,
  currency: "usd",
  unitAmount,
  responseOverage: null,
  workflowRunsIncluded: null,
  workflowRunsOverage: null,
});

const page = (
  overrides: Partial<BillingPage> = {},
  stripe: Partial<NonNullable<BillingPage["billing"]["stripe"]>> = {}
): BillingPage => ({
  billing: {
    stripeCustomerId: "cus_1",
    limits: { workspaces: 1, monthly: { responses: 250, workflowRuns: null } },
    usageCycleAnchor: null,
    stripe: {
      plan: "hobby",
      interval: null,
      subscriptionStatus: "active",
      subscriptionId: "sub_hobby",
      hasPaymentMethod: false,
      trialEnd: null,
      pendingChange: null,
      paymentAttemptError: null,
      ...stripe,
    },
  },
  currentCloudPlan: "hobby",
  currentBillingInterval: null,
  currentSubscriptionStatus: "active",
  pendingChange: null,
  trialDaysRemaining: null,
  usageCycleStart: "2026-10-01T00:00:00Z",
  usageCycleEnd: "2026-11-01T00:00:00Z",
  billingCatalog: {
    hobby: { monthly: item("hobby", "monthly", 0) },
    pro: { monthly: item("pro", "monthly", 4900), yearly: item("pro", "yearly", 49000) },
    scale: {
      monthly: { ...item("scale", "monthly", 14900), workflowRunsIncluded: 1000 },
      yearly: item("scale", "yearly", 149000),
    },
  },
  responseCount: 120,
  workspaceCount: 1,
  workflowRunCount: 0,
  isPlanComparison: false,
  hasBillingRights: true,
  isStripeSetupIncomplete: false,
  stripePublishableKey: "pk_test",
  ...overrides,
});

interface Call {
  method: string;
  path: string;
  body: unknown;
}

type Answer = { status?: number; body?: unknown };

/** The Go server: the billing page, and the given answers to the billing actions (keyed "METHOD path-after-/billing"). Every request is recorded. */
const stubServer = (
  data: BillingPage,
  answers: Record<string, Answer | ((body: unknown) => Answer)> = {}
) => {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const method = init.method ?? "GET";
      const body = typeof init.body === "string" ? (JSON.parse(init.body) as unknown) : undefined;
      calls.push({ method, path, body });
      const respond = ({ status = 200, body: payload }: Answer) =>
        new Response(JSON.stringify(payload ?? {}), {
          status,
          headers: { "content-type": "application/json" },
        });
      if (method === "GET" && path === API) return respond({ body: data });
      const key = `${method} ${path.slice(API.length).split("?")[0]}`;
      const answer = answers[key];
      if (!answer) return respond({ status: 404, body: { code: "not_found", message: key } });
      return respond(typeof answer === "function" ? answer(body) : answer);
    })
  );
  return calls;
};

const actions = (calls: Call[]) => calls.filter((call) => !(call.method === "GET" && call.path === API));

const renderPage = (search: BillingSearch = {}) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const billingRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/organizations/$organizationId/settings/billing",
    component: () => {
      const search = billingRoute.useSearch();
      return (
        <Suspense fallback={null}>
          <BillingSettingsPage organizationId={ORG} search={search} />
        </Suspense>
      );
    },
    validateSearch: (raw: Record<string, unknown>): BillingSearch => ({
      checkout_success: stringParam(raw.checkout_success),
      upgrade_pending: stringParam(raw.upgrade_pending),
      session_id: stringParam(raw.session_id),
    }),
  });
  const query = new URLSearchParams(search as Record<string, string>).toString();
  const router = createRouter({
    routeTree: rootRoute.addChildren([billingRoute]),
    history: createMemoryHistory({ initialEntries: [query ? `${BILLING_PATH}?${query}` : BILLING_PATH] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return router;
};

const planCard = (name: string) => screen.getByRole("region", { name });
const toastMessages = () => toast.getSnapshot().map((shown) => `${shown.kind}: ${shown.message}`);

describe("subscription and usage", () => {
  test("shows the plan, the month's usage against the limits and the usage cycle", async () => {
    stubServer(page());
    renderPage();
    const subscription = await screen.findByRole("region", {
      name: t("workspace.settings.billing.subscription"),
    });
    expect(within(subscription).getByText(t("workspace.settings.billing.plan_hobby"))).toBeTruthy();
    expect(subscription.textContent).toContain("120 / 250");
    expect(subscription.textContent).toContain("Oct 1, 2026 - Nov 1, 2026");
    // Hobby has no workflow price, so no workflow-runs card.
    expect(subscription.textContent).not.toContain(t("common.workflow_runs"));
    expect(
      within(subscription).getByText(t("workspace.settings.billing.manage_billing_details"))
    ).toBeTruthy();
  });

  test("the customer portal opens from the subscription card", async () => {
    const calls = stubServer(page(), {
      "POST /portal": { body: { url: "https://billing.stripe.com/p/session" } },
    });
    renderPage();
    fireEvent.click(await screen.findByText(t("workspace.settings.billing.manage_billing_details")));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://billing.stripe.com/p/session"));
    expect(actions(calls).map((call) => call.method + " " + call.path)).toEqual([`POST ${API}/portal`]);
  });

  test("a member without billing rights sees the plans but can change nothing", async () => {
    stubServer(page({ hasBillingRights: false }));
    renderPage();
    const pro = await screen.findByRole("region", { name: t("workspace.settings.billing.plan_pro") });
    expect(
      within(pro)
        .getByRole("button", { name: t("workspace.settings.billing.upgrade_now") })
        .hasAttribute("disabled")
    ).toBe(true);
    expect(screen.queryByText(t("workspace.settings.billing.manage_billing_details"))).toBeNull();
  });

  test("an incomplete Stripe setup offers a retry instead of the plans", async () => {
    const calls = stubServer(page({ isStripeSetupIncomplete: true }, {}), {
      "POST /retry-setup": { body: { success: true } },
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: t("workspace.settings.billing.retry_setup") }));
    await waitFor(() => expect(actions(calls)).toHaveLength(1));
    expect(actions(calls)[0]?.path).toBe(`${API}/retry-setup`);
    expect(screen.queryByText(t("workspace.settings.billing.plan_selection_title"))).toBeNull();
  });

  test("a failed payment is explained with the way to support", async () => {
    stubServer(
      page(
        {},
        {
          paymentAttemptError: {
            type: "failed_invoice",
            paymentIntentId: "pi_1",
            message: "Payment was canceled.",
            createdAt: "2026-10-20T00:00:00Z",
          },
        }
      )
    );
    renderPage();
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain(t("workspace.settings.billing.payment_error_failed_invoice"));
    expect(within(alert).getByRole("link", { name: "hola@forma.ylam.ai" }).getAttribute("href")).toBe(
      "mailto:hola@forma.ylam.ai"
    );
  });
});

describe("choosing a plan without a card on file", () => {
  test("Pro without any subscription goes to a subscription Checkout", async () => {
    const calls = stubServer(page({}, { subscriptionId: null }), {
      "POST /checkout": { body: { url: "https://checkout.stripe.com/c/pay" } },
    });
    renderPage();
    fireEvent.click(
      await within(await screen.findByRole("region", { name: "Pro" })).findByRole("button", {
        name: t("workspace.settings.billing.upgrade_now"),
      })
    );
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://checkout.stripe.com/c/pay"));
    expect(actions(calls)[0]?.body).toEqual({ targetPlan: "pro", targetInterval: "monthly" });
    expect(sessionStorage.getItem("billingConfirmationOrganizationId")).toBe(ORG);
  });

  test("yearly Checkout without a subscription is refused before asking the server", async () => {
    const calls = stubServer(page({}, { subscriptionId: null }));
    renderPage();
    fireEvent.click(await screen.findByRole("tab", { name: t("workspace.settings.billing.yearly") }));
    fireEvent.click(
      within(planCard("Scale")).getByRole("button", { name: t("workspace.settings.billing.upgrade_now") })
    );
    await waitFor(() =>
      expect(toastMessages()).toContain(
        `error: ${t("workspace.settings.billing.yearly_checkout_unavailable")}`
      )
    );
    expect(actions(calls)).toEqual([]);
  });

  test("with a subscription, a setup Checkout carries the upgrade to apply on return", async () => {
    const calls = stubServer(page(), {
      "POST /setup-checkout": { body: { url: "https://checkout.stripe.com/c/setup" } },
    });
    renderPage();
    fireEvent.click(await screen.findByRole("tab", { name: t("workspace.settings.billing.yearly") }));
    fireEvent.click(
      within(planCard("Scale")).getByRole("button", { name: t("workspace.settings.billing.upgrade_now") })
    );
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://checkout.stripe.com/c/setup"));
    expect(actions(calls)[0]?.body).toEqual({ targetPlan: "scale", targetInterval: "yearly" });
    expect(sessionStorage.getItem("billingPendingUpgradePlan")).toBe("scale");
  });

  test("a refused setup Checkout forgets the upgrade and says why", async () => {
    stubServer(page(), {
      "POST /setup-checkout": {
        status: 403,
        body: { code: "payment_method_required", message: "payment_method_required" },
      },
    });
    renderPage();
    fireEvent.click(
      within(await screen.findByRole("region", { name: "Pro" })).getByRole("button", {
        name: t("workspace.settings.billing.upgrade_now"),
      })
    );
    await waitFor(() =>
      expect(toastMessages()).toContain(`error: ${t("workspace.settings.billing.payment_method_required")}`)
    );
    expect(sessionStorage.getItem("billingPendingUpgradePlan")).toBeNull();
  });
});

describe("changing plans with a card on file", () => {
  const pro = () =>
    page(
      { currentCloudPlan: "pro", currentBillingInterval: "monthly" },
      { plan: "pro", interval: "monthly", subscriptionId: "sub_pro", hasPaymentMethod: true }
    );

  test("an upgrade confirms the previewed charge, applies it and waits for the plan", async () => {
    const calls = stubServer(pro(), {
      "GET /upgrade-preview": { body: { preview: { amountDue: 10000, currency: "usd" } } },
      "POST /plan": {
        body: { mode: "immediate", pendingChange: null, clientSecret: null, requiresAction: false },
      },
      "POST /wait/plan": { body: { plan: "scale" } },
    });
    renderPage();
    fireEvent.click(
      within(await screen.findByRole("region", { name: "Scale" })).getByRole("button", {
        name: t("workspace.settings.billing.upgrade_now"),
      })
    );
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText(/You'll be charged \$100 now/);
    fireEvent.click(
      within(dialog).getByRole("button", { name: t("workspace.settings.billing.confirm_upgrade_button") })
    );
    await waitFor(() =>
      expect(toastMessages()).toContain(
        `success: ${t("workspace.settings.billing.upgrade_checkout_success", { plan: "Scale" })}`
      )
    );
    expect(actions(calls).map((call) => [call.method, call.path.slice(API.length), call.body])).toEqual([
      ["GET", "/upgrade-preview?targetPlan=scale&targetInterval=monthly", undefined],
      ["POST", "/plan", { targetPlan: "scale", targetInterval: "monthly" }],
      ["POST", "/wait/plan", { targetPlan: "scale" }],
    ]);
  });

  test("an upgrade whose 3D Secure is abandoned is reported and not applied", async () => {
    vi.mocked(confirmUpgradePayment).mockResolvedValue({ status: "failed", paymentIntentId: "pi_3ds" });
    const calls = stubServer(pro(), {
      "GET /upgrade-preview": { body: { preview: null } },
      "POST /plan": {
        body: { mode: "immediate", pendingChange: null, clientSecret: "pi_3ds_secret", requiresAction: true },
      },
      "POST /payment-issue": { body: { success: true } },
    });
    renderPage();
    fireEvent.click(
      within(await screen.findByRole("region", { name: "Scale" })).getByRole("button", {
        name: t("workspace.settings.billing.upgrade_now"),
      })
    );
    const dialog = await screen.findByRole("dialog");
    // Without a preview the modal names the list price.
    await within(dialog).findByText(/\$149 per month/);
    fireEvent.click(
      within(dialog).getByRole("button", { name: t("workspace.settings.billing.confirm_upgrade_button") })
    );
    await waitFor(() =>
      expect(toastMessages()).toContain(
        `error: ${t("workspace.settings.billing.payment_authentication_failed")}`
      )
    );
    expect(confirmUpgradePayment).toHaveBeenCalledWith("pk_test", "pi_3ds_secret");
    expect(actions(calls).find((call) => call.path.endsWith("/payment-issue"))?.body).toEqual({
      paymentIntentId: "pi_3ds",
    });
    expect(actions(calls).some((call) => call.path.endsWith("/wait/plan"))).toBe(false);
  });

  test("the current paid plan's button cancels at period end", async () => {
    const calls = stubServer(pro(), {
      "POST /plan": {
        body: { mode: "scheduled", pendingChange: null, clientSecret: null, requiresAction: false },
      },
    });
    renderPage();
    fireEvent.click(
      within(await screen.findByRole("region", { name: "Pro" })).getByRole("button", {
        name: t("workspace.settings.billing.cancel_at_period_end"),
      })
    );
    await waitFor(() =>
      expect(toastMessages()).toContain(`success: ${t("workspace.settings.billing.plan_change_scheduled")}`)
    );
    expect(actions(calls)[0]?.body).toEqual({ targetPlan: "hobby", targetInterval: "monthly" });
  });

  test("a scheduled change is announced and can be undone", async () => {
    const calls = stubServer(
      page({
        ...pro(),
        pendingChange: {
          type: "plan_change",
          targetPlan: "hobby",
          targetInterval: null,
          effectiveAt: "2026-11-01T00:00:00Z",
        },
      }),
      { "DELETE /pending-change": { body: { success: true } } }
    );
    renderPage();
    expect(
      await screen.findByText(
        t("workspace.settings.billing.pending_plan_change_description", {
          plan: "Hobby",
          date: "Nov 1, 2026",
        })
      )
    ).toBeTruthy();
    expect(
      within(planCard("Hobby"))
        .getByRole("button", { name: t("workspace.settings.billing.pending_plan_cta") })
        .hasAttribute("disabled")
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: t("workspace.settings.billing.keep_current_plan") }));
    await waitFor(() =>
      expect(toastMessages()).toContain(`success: ${t("workspace.settings.billing.pending_change_removed")}`)
    );
    expect(actions(calls)[0]).toMatchObject({ method: "DELETE", path: `${API}/pending-change` });
  });
});

describe("a trial", () => {
  const trial = (hasPaymentMethod: boolean) =>
    page(
      {
        currentCloudPlan: "pro",
        currentBillingInterval: "monthly",
        currentSubscriptionStatus: "trialing",
        trialDaysRemaining: 5,
      },
      {
        plan: "pro",
        interval: "monthly",
        subscriptionStatus: "trialing",
        subscriptionId: "sub_trial",
        hasPaymentMethod,
        trialEnd: "2026-10-28T00:00:00Z",
      }
    );

  test("without a card, unlocking the plan offers paying now and adds the card through a setup Checkout", async () => {
    const calls = stubServer(trial(false), {
      "GET /upgrade-preview": { body: { preview: { amountDue: 4900, currency: "usd" } } },
      "POST /setup-checkout": { body: { url: "https://checkout.stripe.com/c/setup" } },
    });
    renderPage();
    const banner = await screen.findByText(t("common.trial_days_remaining", { count: 5 }));
    expect(
      screen.getByText(t("workspace.settings.billing.trial_cancels_automatically", { date: "Oct 28, 2026" }))
    ).toBeTruthy();
    fireEvent.click(
      within(banner.closest("[role=status]") as HTMLElement).getByRole("button", {
        name: "Unlock all Pro features",
      })
    );
    const dialog = await screen.findByRole("dialog");
    const payNow = await within(dialog).findByRole("button", { name: "Pay $49 now" });
    fireEvent.click(payNow);
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://checkout.stripe.com/c/setup"));
    expect(actions(calls).at(-1)?.body).toEqual({ targetPlan: "pro", targetInterval: "monthly" });
  });

  test("returning to Hobby ends the trial now, after a confirmation", async () => {
    const calls = stubServer(trial(false), {
      "POST /plan": {
        body: { mode: "immediate", pendingChange: null, clientSecret: null, requiresAction: false },
      },
      "POST /wait/plan": { body: { plan: "hobby" } },
    });
    renderPage();
    fireEvent.click(
      within(await screen.findByRole("region", { name: "Hobby" })).getByRole("button", {
        name: t("workspace.settings.billing.downgrade_to_hobby"),
      })
    );
    const dialog = await screen.findByRole("dialog");
    expect(actions(calls)).toEqual([]);
    fireEvent.click(
      within(dialog).getByRole("button", { name: t("workspace.settings.billing.downgrade_to_hobby") })
    );
    await waitFor(() =>
      expect(actions(calls).map((call) => call.path.slice(API.length))).toEqual(["/plan", "/wait/plan"])
    );
    expect(actions(calls)[0]?.body).toEqual({ targetPlan: "hobby", targetInterval: "monthly" });
  });
});

describe("returning from Stripe Checkout", () => {
  test("a setup Checkout for an upgrade applies it and waits until the plan shows", async () => {
    sessionStorage.setItem("billingPendingUpgradePlan", "scale");
    const calls = stubServer(page(), {
      "POST /setup-checkout/finalize": {
        body: { mode: "immediate", clientSecret: null, requiresAction: false, targetPlan: "scale" },
      },
      "POST /wait/plan": { body: { plan: "scale" } },
    });
    const router = renderPage({ checkout_success: "1", upgrade_pending: "1", session_id: "cs_test_1" });
    await waitFor(() =>
      expect(toastMessages()).toContain(
        `success: ${t("workspace.settings.billing.upgrade_checkout_success", { plan: "Scale" })}`
      )
    );
    expect(actions(calls).map((call) => [call.path.slice(API.length), call.body])).toEqual([
      ["/setup-checkout/finalize", { checkoutSessionId: "cs_test_1" }],
      ["/wait/plan", { targetPlan: "scale" }],
    ]);
    await waitFor(() => expect(router.state.location.searchStr).toBe(""));
    expect(sessionStorage.getItem("billingPendingUpgradePlan")).toBeNull();
  });

  test("a refused finalization explains itself and leaves the return URL", async () => {
    stubServer(page(), {
      "POST /setup-checkout/finalize": {
        status: 403,
        body: { code: "checkout_session_mismatch", message: "checkout_session_mismatch" },
      },
    });
    const router = renderPage({ checkout_success: "1", upgrade_pending: "1", session_id: "cs_other" });
    await waitFor(() =>
      expect(toastMessages()).toContain(`error: ${t("common.something_went_wrong_please_try_again")}`)
    );
    await waitFor(() => expect(router.state.location.pathname).toBe(BILLING_PATH));
    expect(router.state.location.searchStr).toBe("");
  });

  test("a card-only setup Checkout waits for the card before showing the page again", async () => {
    const calls = stubServer(page(), { "POST /wait/payment-method": { body: { hasPaymentMethod: true } } });
    const router = renderPage({ checkout_success: "1" });
    await waitFor(() => expect(router.state.location.searchStr).toBe(""));
    expect(actions(calls).map((call) => call.path.slice(API.length))).toEqual(["/wait/payment-method"]);
    expect(toastMessages()).toEqual([]);
  });
});
