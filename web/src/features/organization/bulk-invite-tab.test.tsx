import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import {
  BULK_INVITE_TEMPLATE_PATH,
  type BulkCsvRow,
  BulkInviteTab,
  checkBulkInvitees,
  parseBulkCsv,
  parseTeamCell,
  readCell,
} from "./bulk-invite-tab";

// forma2's bulk-invite-tab.tsx and its tests (ENG-1596), with the bulk half of organization-actions.tsx's handleAddMembers.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("readCell", () => {
  test("returns the first non-empty value across header aliases", () => {
    expect(readCell({ "Email Address": "user@example.com", email: "ignored@example.com" }, "Email Address", "email")).toBe(
      "user@example.com"
    );
    expect(readCell({ email: "user@example.com" }, "Email Address", "email")).toBe("user@example.com");
  });

  test("returns an empty string for a missing, undefined or empty column instead of throwing", () => {
    const row: BulkCsvRow = { "Full Name": "Ada", "Email Address": undefined, email: "" };
    expect(readCell(row, "Email Address", "email")).toBe("");
    expect(() => readCell({}, "Email Address", "email").trim()).not.toThrow();
  });

  test("does not match a differently-cased header", () => {
    expect(readCell({ "full name": "Ada" }, "Full Name", "name")).toBe("");
  });
});

describe("parseTeamCell", () => {
  test("splits on commas and pipes, trimming and dropping empty segments", () => {
    expect(parseTeamCell("Design, Engineering | Sales")).toEqual(["Design", "Engineering", "Sales"]);
    expect(parseTeamCell(" , Design,, ")).toEqual(["Design"]);
    expect(parseTeamCell(undefined)).toEqual([]);
  });
});

describe("parseBulkCsv", () => {
  test("reads rows by the header row, skipping empty lines and a byte order mark", () => {
    expect(parseBulkCsv("﻿Full Name,Email Address,Organization Role,Teams\nAda,ada@example.com,member,\"Design, Sales\"\n\n")).toEqual([
      { "Full Name": "Ada", "Email Address": "ada@example.com", "Organization Role": "member", Teams: "Design, Sales" },
    ]);
  });

  test("detects a semicolon file and keeps a short row", () => {
    expect(parseBulkCsv("name;email;role\nAda;ada@example.com")).toEqual([{ name: "Ada", email: "ada@example.com" }]);
  });

  test("throws on a file it cannot read", () => {
    expect(() => parseBulkCsv('name,email\n"Ada,ada@example.com')).toThrow();
  });
});

const TEAMS = [
  { id: "t1", name: "Design" },
  { id: "t2", name: "Sales " },
];

describe("checkBulkInvitees", () => {
  test("maps names, emails, roles and team names to an invite per row", () => {
    const rows: BulkCsvRow[] = [
      { "Full Name": " Ada ", "Email Address": " ada@example.com ", "Organization Role": "Manager", Teams: "design|SALES, Design" },
      { name: "Grace", email: "grace@example.com", role: "member" },
    ];
    expect(checkBulkInvitees(rows, TEAMS, false)).toEqual({
      invitees: [
        { name: "Ada", email: "ada@example.com", role: "manager", teamIds: ["t1", "t2"] },
        { name: "Grace", email: "grace@example.com", role: "member", teamIds: [] },
      ],
    });
  });

  test("names the teams that do not exist", () => {
    const rows: BulkCsvRow[] = [{ name: "Ada", email: "ada@example.com", role: "member", teams: "Design, Ops, Legal" }];
    expect(checkBulkInvitees(rows, TEAMS, false)).toEqual({ unknownTeams: ["Ops", "Legal"] });
  });

  test("refuses the billing role off Forma Cloud", () => {
    const rows: BulkCsvRow[] = [{ name: "Ada", email: "ada@example.com", role: "billing" }];
    expect(checkBulkInvitees(rows, TEAMS, false)).toEqual({ billingEmails: ["ada@example.com"] });
    expect(checkBulkInvitees(rows, TEAMS, true)).toMatchObject({ invitees: [{ role: "billing" }] });
  });

  test.each([
    ["a missing email", { name: "Ada", role: "member" }],
    ["an unknown role", { name: "Ada", email: "ada@example.com", role: "admin" }],
    ["a missing name", { email: "ada@example.com", role: "member" }],
  ])("rejects %s", (_name, row) => {
    expect(checkBulkInvitees([row], TEAMS, false)).toEqual({ invalid: true });
  });
});

/** The invite endpoints answering as the Go server does; the request bodies are recorded by path. */
const stubInvites = (results: { email: string; success: boolean; failureReason?: string }[]) => {
  const requests: { path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (init.method === "POST") {
        requests.push({ path, body: JSON.parse(init.body as string) });
        const body = path.endsWith("/invites/bulk") ? { results } : { id: "i1" };
        return new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
      }
      return new Response(JSON.stringify([]), { headers: { "content-type": "application/json" } });
    })
  );
  return requests;
};

const renderTab = (onDone = vi.fn()) => {
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient()}>
        <BulkInviteTab organizationId="o1" teams={TEAMS} billingRoleAllowed={false} onCancel={() => {}} onDone={onDone} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return onDone;
};

const upload = async (csv: string, name = "members.csv") => {
  const input = document.getElementById("bulk-invite-file") as HTMLInputElement;
  fireEvent.change(input, { target: { files: [new File([csv], name, { type: "text/csv" })] } });
  await screen.findByText(name);
};

const importButton = () => screen.getByRole("button", { name: i18n.t("common.import") });

test("links forma2's CSV template", () => {
  renderTab();
  const template = screen.getByRole("link", { name: i18n.t("workspace.settings.general.bulk_invite_download_template") });
  expect(template.getAttribute("href")).toBe(BULK_INVITE_TEMPLATE_PATH);
});

test("previews the rows and shows the teams the organization does not have", async () => {
  const requests = stubInvites([]);
  renderTab();
  await upload("Full Name,Email Address,Organization Role,Teams\nAda,ada@example.com,member,Ops\n");
  expect(screen.getByText(i18n.t("workspace.settings.general.bulk_invite_rows_detected", { count: 1 }))).toBeTruthy();
  expect(screen.getByRole("cell", { name: "ada@example.com" })).toBeTruthy();
  await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(importButton());
  expect(
    await screen.findByText(i18n.t("workspace.settings.general.bulk_invite_unknown_teams", { teams: "Ops" }))
  ).toBeTruthy();
  expect(requests).toEqual([]);
});

test("invites every row in one bulk request", async () => {
  const requests = stubInvites([
    { email: "ada@example.com", success: true },
    { email: "grace@example.com", success: true },
  ]);
  const onDone = renderTab();
  await upload(
    "Full Name,Email Address,Organization Role,Teams\nAda,Ada@Example.com,manager,Design|Sales\nGrace,grace@example.com,member,\n"
  );
  await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(importButton());
  await waitFor(() => expect(onDone).toHaveBeenCalled());
  expect(requests).toEqual([
    {
      path: "/api/app/organizations/o1/invites/bulk",
      body: {
        invitees: [
          { name: "Ada", email: "ada@example.com", role: "manager", teamIds: ["t1", "t2"] },
          { name: "Grace", email: "grace@example.com", role: "member", teamIds: [] },
        ],
      },
    },
  ]);
});

test("a file of one row is a single invite", async () => {
  const requests = stubInvites([]);
  const onDone = renderTab();
  await upload("name,email,role\nAda,ada@example.com,member\n");
  await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(importButton());
  await waitFor(() => expect(onDone).toHaveBeenCalled());
  expect(requests).toEqual([
    { path: "/api/app/organizations/o1/invites", body: { name: "Ada", email: "ada@example.com", role: "member", teamIds: [] } },
  ]);
});

test("stays open when no one could be invited", async () => {
  stubInvites([
    { email: "ada@example.com", success: false, failureReason: "user_already_member" },
    { email: "grace@example.com", success: false, failureReason: "invite_already_exists" },
  ]);
  const onDone = renderTab();
  await upload("name,email,role\nAda,ada@example.com,member\nGrace,grace@example.com,member\n");
  await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(importButton());
  await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
  expect(onDone).not.toHaveBeenCalled();
});

test("refuses a file that is not a CSV", async () => {
  renderTab();
  const input = document.getElementById("bulk-invite-file") as HTMLInputElement;
  fireEvent.change(input, { target: { files: [new File(["x"], "members.xlsx")] } });
  expect(await screen.findByText(i18n.t("common.invalid_file_type"))).toBeTruthy();
});
