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
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { MainNavigation } from "../../../components/shell/main-navigation";
import { SettingsNavigation } from "../../../components/shell/settings-navigation";
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import { BillingConfirmation } from "./billing-confirmation";
import { LimitsReachedBanner } from "./limits-reached-banner";
import { SelectPlanCard } from "./select-plan-card";
import { trialAlertVariant } from "./trial-alert";

// jsdom has no canvas to draw the confetti on.
vi.mock("../../../components/ui/confetti", () => ({ Confetti: () => null }));

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";
const API = `/api/app/organizations/${ORG}/billing`;

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  window.scrollTo = () => {};
});

beforeEach(() => sessionStorage.clear());

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

/** The Go server answering each "METHOD path" with the given status and body; requests are recorded, and a deferred answer waits until released. */
const stubServer = (
  answers: Record<string, { status?: number; body: unknown } | Promise<{ status?: number; body: unknown }>>
) => {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      calls.push(key);
      const answer = await answers[key];
      if (!answer) throw new Error(`unexpected ${key}`);
      return new Response(JSON.stringify(answer.body), {
        status: answer.status ?? 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return calls;
};

/** Renders the element at path, with a page at every other path that names it, so a navigation shows where it went. */
const renderAt = (path: string, element: ReactNode) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path, component: () => element }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "$",
        component: () => {
          const { pathname } = router.state.location;
          return <h1>{`page ${pathname}`}</h1>;
        },
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return router;
};

describe("LimitsReachedBanner", () => {
  const limits = (responses: number | null) => ({ workspaces: 1, monthly: { responses } });

  test("appears once the month's responses reach the limit, and can be dismissed", async () => {
    renderAt("/", <LimitsReachedBanner organizationId={ORG} limits={limits(250)} responseCount={250} />);
    expect(
      await screen.findByText(
        t("common.you_have_reached_your_monthly_response_limit_of_count", { count: 250 })
      )
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: t("common.learn_more") }).getAttribute("href")).toBe(
      `/organizations/${ORG}/settings/billing`
    );
    fireEvent.click(screen.getByRole("button", { name: t("common.close") }));
    expect(screen.queryByText(t("common.limits_reached"))).toBeNull();
  });

  test.each([
    ["below the limit", 250, 249],
    ["unlimited", null, 10_000],
  ])("stays away %s", async (_name, responses, count) => {
    const router = renderAt(
      "/",
      <LimitsReachedBanner organizationId={ORG} limits={limits(responses)} responseCount={count} />
    );
    await waitFor(() => expect(router.state.status).toBe("idle"));
    expect(screen.queryByText(t("common.limits_reached"))).toBeNull();
  });
});

test("the trial notice turns amber in the last week and red in the last three days", () => {
  expect(trialAlertVariant(10)).toBe("info");
  expect(trialAlertVariant(7)).toBe("warning");
  expect(trialAlertVariant(3)).toBe("error");
  expect(trialAlertVariant(0)).toBe("error");
});

describe("navigation", () => {
  test("the workspace sidebar shows an owner the trial countdown, linking to billing", async () => {
    renderAt(
      `/workspaces/${WS}/`,
      <MainNavigation
        workspaceId={WS}
        organizationId={ORG}
        membershipRole="owner"
        pathname={`/workspaces/${WS}/`}
        trialDaysRemaining={1}
      />
    );
    const notice = await screen.findByText(t("common.trial_one_day_remaining"));
    expect(notice.closest("a")?.getAttribute("href")).toBe(`/organizations/${ORG}/settings/billing`);
  });

  test("a member sees no trial countdown", async () => {
    const router = renderAt(
      `/workspaces/${WS}/`,
      <MainNavigation
        workspaceId={WS}
        organizationId={ORG}
        membershipRole="member"
        pathname={`/workspaces/${WS}/`}
        trialDaysRemaining={5}
      />
    );
    await waitFor(() => expect(router.state.status).toBe("idle"));
    expect(screen.queryByText(t("common.trial_days_remaining", { count: 5 }))).toBeNull();
  });

  test.each([
    [true, true],
    [false, false],
  ])("the settings sidebar offers billing on Forma Cloud (%s)", async (isFormaCloud, shown) => {
    const path = `/organizations/${ORG}/settings/general`;
    const router = renderAt(
      path,
      <SettingsNavigation
        organizationId={ORG}
        organizationName="Acme"
        membershipRole="billing"
        backUrl="/"
        pathname={path}
        isFormaCloud={isFormaCloud}
      />
    );
    await waitFor(() => expect(router.state.status).toBe("idle"));
    const billing = screen.queryByRole("link", { name: t("common.billing") });
    expect(billing !== null).toBe(shown);
    // The billing role, which may use nothing else here, may use billing.
    if (billing) expect(billing.getAttribute("href")).toBe(`/organizations/${ORG}/settings/billing`);
  });
});

describe("SelectPlanCard", () => {
  const card = () =>
    renderAt(
      `/organizations/${ORG}/workspaces/new/plan`,
      <SelectPlanCard organizationId={ORG} nextUrl={`/organizations/${ORG}/workspaces/new/survey`} />
    );

  /** The billing summary, which names the length of a trial started now. */
  const summary = (proTrialDays: number) => ({
    [`GET ${API}/summary`]: {
      body: {
        isFormaCloud: true,
        plan: "hobby",
        subscriptionStatus: null,
        trialEnd: null,
        trialDaysRemaining: null,
        proTrialDays,
        limits: { workspaces: 1, monthly: { responses: 250 } },
        usage: null,
        usageCycle: null,
      },
    },
  });

  test("starting the trial moves on to the first survey", async () => {
    const calls = stubServer({ ...summary(14), [`POST ${API}/trial`]: { body: { success: true } } });
    card();
    expect(await screen.findByText("You're on Pro for the next 14 days")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: t("workspace.settings.billing.select_plan_cta") }));
    expect(await screen.findByText(`page /organizations/${ORG}/workspaces/new/survey`)).toBeTruthy();
    expect(calls).toEqual([`GET ${API}/summary`, `POST ${API}/trial`]);
  });

  // The server shortens the trial for the organizations in forma2's a-b_billing_shorten-trial-days test variant; the card promises the trial it will start.
  test("promises the trial length the server will start", async () => {
    stubServer(summary(7));
    card();
    expect(await screen.findByText("You're on Pro for the next 7 days")).toBeTruthy();
    expect(screen.queryByText("You're on Pro for the next 14 days")).toBeNull();
  });

  test("a trial already used is said so, and the card stays", async () => {
    stubServer({
      ...summary(14),
      [`POST ${API}/trial`]: {
        status: 403,
        body: { code: "trial_already_used", message: "trial_already_used" },
      },
    });
    card();
    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.settings.billing.select_plan_cta") })
    );
    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toEqual([
        t("workspace.settings.billing.trial_already_used"),
      ])
    );
    expect(
      screen
        .getByRole("button", { name: t("workspace.settings.billing.select_plan_cta") })
        .hasAttribute("disabled")
    ).toBe(false);
  });

  test("staying on Hobby is confirmed first, then moves on", async () => {
    const calls = stubServer({ ...summary(14), [`POST ${API}/hobby`]: { body: { success: true } } });
    card();
    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.settings.billing.select_plan_skip") })
    );
    expect(calls).toEqual([`GET ${API}/summary`]);
    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.settings.billing.hobby_confirm_downgrade") })
    );
    expect(await screen.findByText(`page /organizations/${ORG}/workspaces/new/survey`)).toBeTruthy();
    expect(calls).toEqual([`GET ${API}/summary`, `POST ${API}/hobby`]);
  });
});

describe("BillingConfirmation", () => {
  test("the way back to billing waits until the purchased plan is synced", async () => {
    let release: (answer: { body: unknown }) => void = () => {};
    const synced = new Promise<{ body: unknown }>((resolve) => {
      release = resolve;
    });
    const calls = stubServer({ [`POST ${API}/wait/plan`]: synced });
    renderAt("/billing-confirmation", <BillingConfirmation organizationId={ORG} plan="pro" />);
    const waiting = await screen.findByRole("button", {
      name: t("billing_confirmation.back_to_billing_overview"),
    });
    expect(waiting.hasAttribute("disabled")).toBe(true);
    release({ body: { plan: "pro" } });
    const back = await screen.findByRole("link", {
      name: t("billing_confirmation.back_to_billing_overview"),
    });
    expect(back.getAttribute("href")).toBe(`/organizations/${ORG}/settings/billing`);
    expect(calls).toEqual([`POST ${API}/wait/plan`]);
  });

  test("without the organization in the URL it uses the one this tab left for", async () => {
    sessionStorage.setItem("billingConfirmationOrganizationId", ORG);
    const calls = stubServer({});
    renderAt("/billing-confirmation", <BillingConfirmation />);
    const back = await screen.findByRole("link", {
      name: t("billing_confirmation.back_to_billing_overview"),
    });
    expect(back.getAttribute("href")).toBe(`/organizations/${ORG}/settings/billing`);
    // No plan to wait for.
    expect(calls).toEqual([]);
  });
});
