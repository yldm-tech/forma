import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { Retention } from "../../lib/queries/organizations";
import { toast } from "../../lib/toast";
import { RetentionCard, deletesMore } from "./retention";

// The owner's Data retention card: the windows, whether the instance's sweep only counts, and what its last run counted or deleted.

const ORG = "0190a000-0000-7000-8000-000000000001";
const API = `/api/app/organizations/${ORG}/retention`;

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

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
  vi.unstubAllGlobals();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const retention = (overrides: Partial<Retention> = {}): Retention => ({
  workflowRunRetentionDays: null,
  displayRetentionDays: null,
  dryRun: true,
  nextSweepAt: "2026-10-27T00:00:00Z",
  lastSweep: null,
  ...overrides,
});

/** The server: the card as given, a PUT that answers with the windows it was sent, and a preview that counts 7 runs and 2 impressions for whichever windows it is asked about. */
const stubServer = (current: Retention) => {
  const fetch = vi.fn(async (path: string, init: RequestInit = {}) => {
    let body: unknown;
    if (path.startsWith(`${API}/preview?`)) {
      const query = new URLSearchParams(path.slice(path.indexOf("?") + 1));
      body = {
        workflowRuns: query.has("workflowRunRetentionDays") ? 7 : null,
        displays: query.has("displayRetentionDays") ? 2 : null,
      };
    } else if (path === API) {
      body =
        init.method === "PUT" ? { ...current, ...JSON.parse(init.body as string), lastSweep: null } : current;
    } else throw new Error(`unexpected ${path}`);
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
};

const putBody = (fetch: ReturnType<typeof stubServer>): unknown => {
  const call = fetch.mock.calls.find(([, init]) => init?.method === "PUT");
  return call ? JSON.parse(call[1]?.body as string) : undefined;
};

const renderCard = (isFormaCloud = false) =>
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RetentionCard organizationId={ORG} isFormaCloud={isFormaCloud} />
      </QueryClientProvider>
    </I18nextProvider>
  );

const runsSelect = () =>
  screen.getByLabelText(t("organizations.retention.workflow_runs_label")) as HTMLSelectElement;
const save = () => fireEvent.click(screen.getByRole("button", { name: t("common.save_changes") }));

describe("RetentionCard", () => {
  test("on a dry-run instance an owner sets a window without confirming: nothing is deleted", async () => {
    const fetch = stubServer(retention());
    renderCard();
    expect(await screen.findByText(t("organizations.retention.dry_run_title"))).toBeTruthy();
    expect(screen.getByText(t("organizations.retention.nothing_deleted"))).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: t("common.save_changes") }) as HTMLButtonElement).disabled
    ).toBe(true);
    fireEvent.change(runsSelect(), { target: { value: "90" } });
    save();
    await waitFor(() =>
      expect(putBody(fetch)).toEqual({ workflowRunRetentionDays: 90, displayRetentionDays: null })
    );
    expect(screen.queryByText(t("organizations.retention.confirm_title"))).toBeNull();
    expect(await screen.findByText(/No sweep has run with these settings yet/)).toBeTruthy();
  });

  // The sweep keeps an app survey's impressions of identified users whatever the window (they decide show once, display limits and waiting periods), and the impressions window says so.
  test("the impressions window says identified users' app survey impressions are kept", async () => {
    stubServer(retention());
    renderCard();
    expect(
      await screen.findByText(/App survey impressions of identified users are also kept, however old/)
    ).toBeTruthy();
  });

  // Saving during the dry run is not confirmed, so the banner says the settings delete without asking once deletion is on; only a self-hosting operator can turn it on, so Cloud owners are not sent to an environment variable.
  test.each([
    [false, "dry_run_description", true],
    [true, "dry_run_description_cloud", false],
  ] as const)("the dry-run banner on Forma Cloud = %s", async (isFormaCloud, key, mentionsVariable) => {
    stubServer(retention());
    renderCard(isFormaCloud);
    const banner = await screen.findByText(t(`organizations.retention.${key}`));
    expect(banner.textContent).toContain("without asking again");
    expect(banner.textContent?.includes("RETENTION_SWEEP_DRY_RUN")).toBe(mentionsVariable);
  });

  test("on an instance that deletes, setting or shortening a window asks first", async () => {
    const fetch = stubServer(retention({ dryRun: false, workflowRunRetentionDays: 365 }));
    renderCard();
    expect(await screen.findByText(t("organizations.retention.deleting_title"))).toBeTruthy();
    fireEvent.change(runsSelect(), { target: { value: "30" } });
    save();
    expect(await screen.findByText(t("organizations.retention.confirm_title"))).toBeTruthy();
    // The dialog counts what the new windows would delete, and only for the windows that delete anything.
    expect(await screen.findByText("Would delete 7 workflow runs")).toBeTruthy();
    expect(screen.queryByText(/Would delete \d+ impression/)).toBeNull();
    expect(fetch).toHaveBeenCalledWith(`${API}/preview?workflowRunRetentionDays=30`, expect.anything());
    expect(putBody(fetch)).toBeUndefined();
    fireEvent.click(screen.getByRole("button", { name: t("organizations.retention.confirm_button") }));
    await waitFor(() =>
      expect(putBody(fetch)).toEqual({ workflowRunRetentionDays: 30, displayRetentionDays: null })
    );
  });

  test("the last sweep's counts under the current windows, and a window set elsewhere is offered as it is", async () => {
    stubServer(
      retention({
        workflowRunRetentionDays: 45,
        displayRetentionDays: 30,
        lastSweep: {
          sweptAt: "2026-10-26T00:00:00Z",
          dryRun: true,
          stoppedEarly: false,
          workflowRunRetentionDays: 45,
          displayRetentionDays: 30,
          workflowRuns: { matched: 3, deleted: 0, cappedUnits: 0 },
          displays: { matched: 1, deleted: 0, cappedUnits: 1 },
        },
      })
    );
    renderCard();
    expect(await screen.findByText("Would delete 3 workflow runs")).toBeTruthy();
    expect(screen.getByText("Would delete 1 impression without a response")).toBeTruthy();
    expect(screen.getByText(t("organizations.retention.capped"))).toBeTruthy();
    expect(screen.queryByText(t("organizations.retention.stopped_early"))).toBeNull();
    expect(runsSelect().value).toBe("45");
    expect(runsSelect().selectedOptions[0]?.textContent).toBe("45 days");
  });

  test("a real sweep reports what it deleted, even when it stopped early", async () => {
    stubServer(
      retention({
        dryRun: false,
        displayRetentionDays: 30,
        lastSweep: {
          sweptAt: "2026-10-26T00:00:00Z",
          dryRun: false,
          stoppedEarly: true,
          workflowRunRetentionDays: null,
          displayRetentionDays: 30,
          workflowRuns: { matched: 0, deleted: 0, cappedUnits: 0 },
          displays: { matched: 0, deleted: 12, cappedUnits: 0 },
        },
      })
    );
    renderCard();
    expect(await screen.findByText("Deleted 12 impressions without a response")).toBeTruthy();
    expect(screen.queryByText(/Deleted \d+ workflow run/)).toBeNull();
    expect(screen.getByText(t("organizations.retention.stopped_early"))).toBeTruthy();
  });
});

describe("deletesMore", () => {
  test("a window set or shortened deletes more; one lengthened or removed does not", () => {
    const none = { workflowRunRetentionDays: null, displayRetentionDays: null };
    expect(deletesMore(none, { ...none, displayRetentionDays: 365 })).toBe(true);
    expect(
      deletesMore({ ...none, workflowRunRetentionDays: 90 }, { ...none, workflowRunRetentionDays: 30 })
    ).toBe(true);
    expect(
      deletesMore({ ...none, workflowRunRetentionDays: 30 }, { ...none, workflowRunRetentionDays: 90 })
    ).toBe(false);
    expect(deletesMore({ ...none, workflowRunRetentionDays: 30 }, none)).toBe(false);
  });
});
