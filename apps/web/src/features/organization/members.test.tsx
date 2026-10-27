import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { Invite, UserManagementAccess } from "../../lib/queries/invites";
import type { Member, OrganizationDetail } from "../../lib/queries/organizations";
import type { OrganizationRole } from "../../lib/roles";
import { toast } from "../../lib/toast";
import { InviteMemberModal } from "./invite-member-modal";
import { MembersCard } from "./members";

// The members table and the invite modal on an instance with and without SMTP, for an owner and for a team admin, whom the server lets invite into their own teams but not share or resend invites.

const ORG = "0190a000-0000-7000-8000-000000000001";
const API = `/api/app/organizations/${ORG}`;
const TEAM = "0190a000-0000-7000-8000-0000000000aa";

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

const organization = (role: OrganizationRole): OrganizationDetail => ({
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  isAIResponseReadingEnabled: true,
  displayTimeZone: null,
  surveySchedulingTimeZone: null,
  surveyScheduling: {
    timeZone: "Europe/Berlin",
    localHour: 0,
    localMinute: 0,
    instanceTimeZone: "Europe/Berlin",
  },
  whitelabel: {},
  membership: { organizationId: ORG, userId: "u1", accepted: true, role, suspendedAt: null },
  userManagementAllowed: role === "owner" || role === "manager",
  twoFactorEnforcedAt: null,
  twoFactorStatus: "enabled",
  twoFactorBlocked: false,
});

const ada: Member = {
  userId: "u1",
  name: "Ada Owner",
  email: "ada@example.com",
  accepted: true,
  role: "owner",
  isActive: true,
  suspendedAt: null,
};

const bob: Member = {
  userId: "u2",
  name: "Bob Member",
  email: "bob@example.com",
  accepted: true,
  role: "member",
  isActive: true,
  suspendedAt: null,
};

const members: Member[] = [ada, bob];

const invites: Invite[] = [
  {
    id: "i1",
    email: "cy@example.com",
    name: "Cy Invitee",
    role: "member",
    expiresAt: "2099-01-01T00:00:00Z",
    createdAt: "2026-01-01T00:00:00Z",
  },
];

/** The server: answers by method and path, recording every request. */
const stubServer = (answers: Record<string, unknown>) => {
  const fetch = vi.fn(async (path: string, init: RequestInit = {}) => {
    const key = `${init.method ?? "GET"} ${path}`;
    if (!(key in answers)) throw new Error(`unexpected ${key}`);
    return new Response(JSON.stringify(answers[key]), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
};

const renderInRouter = (node: ReactNode) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => node }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const renderMembers = ({
  role,
  access,
  smtpConfigured,
  memberList = members,
}: {
  role: OrganizationRole;
  access: UserManagementAccess;
  smtpConfigured: boolean;
  memberList?: Member[];
}) => {
  stubServer({
    [`GET ${API}/members`]: { members: memberList },
    [`GET ${API}/invites`]: invites,
    [`GET ${API}/user-management`]: access,
    "GET /api/app/auth/me": { id: "u1", name: "Ada Owner" },
    "GET /api/app/auth/options": { smtpConfigured },
  });
  renderInRouter(<MembersCard organization={organization(role)} />);
};

const ownerAccess: UserManagementAccess = {
  canManageUsers: true,
  adminTeamIds: [],
  inviteDisabled: false,
  billingRoleAllowed: false,
};

const rowOf = async (name: string) => (await screen.findByText(name)).closest("tr") as HTMLElement;
const share = () => t("workspace.settings.general.share_invite_link");
const resend = () => t("workspace.settings.general.resend_invitation_email");

describe("MembersCard", () => {
  // The server sends each member's two-factor status to owners and managers, who decide on the organization's requirement and follow it up.
  test("shows owners each member's two-factor status, and members no such column", async () => {
    renderMembers({
      role: "owner",
      access: ownerAccess,
      smtpConfigured: true,
      memberList: [
        { ...members[0], twoFactor: "enabled" },
        { ...members[1], twoFactor: "disabled" },
        { ...members[1], userId: "u3", name: "Sam Sso", email: "sam@example.com", twoFactor: "sso" },
      ] as Member[],
    });
    expect(
      await screen.findByRole("columnheader", { name: t("organizations.two_factor.column") })
    ).toBeTruthy();
    expect(
      within(await rowOf("Ada Owner")).getByText(t("organizations.two_factor.status_enabled"))
    ).toBeTruthy();
    expect(
      within(await rowOf("Bob Member")).getByText(t("organizations.two_factor.status_disabled"))
    ).toBeTruthy();
    const sso = await rowOf("Sam Sso");
    expect(within(sso).getByText(t("organizations.two_factor.status_sso"))).toBeTruthy();
    expect(within(sso).getByText(t("organizations.two_factor.status_sso_hint"))).toBeTruthy();
    // A pending invite has no account yet, so no status.
    expect(within(await rowOf("Cy Invitee")).getByText("—")).toBeTruthy();
    cleanup();

    renderMembers({
      role: "member",
      access: { ...ownerAccess, canManageUsers: false },
      smtpConfigured: true,
    });
    await rowOf("Bob Member");
    expect(screen.queryByRole("columnheader", { name: t("organizations.two_factor.column") })).toBeNull();
  });

  test("offers share and resend on an invite row only", async () => {
    renderMembers({ role: "owner", access: ownerAccess, smtpConfigured: true });

    const member = await rowOf("Bob Member");
    expect(within(member).queryByRole("button", { name: share() })).toBeNull();
    expect(within(member).queryByRole("button", { name: resend() })).toBeNull();
    expect(within(member).getByRole("button", { name: t("common.delete") })).toBeTruthy();

    const invite = await rowOf("Cy Invitee");
    expect(within(invite).getByRole("button", { name: share() })).toBeTruthy();
    expect(within(invite).getByRole("button", { name: resend() })).toBeTruthy();
  });

  test("keeps share but drops resend where the instance sends no email", async () => {
    renderMembers({ role: "owner", access: ownerAccess, smtpConfigured: false });

    const invite = await rowOf("Cy Invitee");
    await waitFor(() => expect(within(invite).queryByRole("button", { name: resend() })).toBeNull());
    expect(within(invite).getByRole("button", { name: share() })).toBeTruthy();
  });

  // An account switched off on the instance is not something an organization can undo (it suspends a member instead); the badge says who can.
  test("explains an inactive account's badge: only the instance's administrator turns it back on", async () => {
    renderMembers({
      role: "owner",
      access: ownerAccess,
      smtpConfigured: true,
      memberList: members.map((member) => (member.userId === "u2" ? { ...member, isActive: false } : member)),
    });

    const row = await rowOf("Bob Member");
    // The button is named by its badge alone, so the explanation is not read twice once the tooltip describes it.
    const badge = within(row).getByRole("button", { name: t("common.inactive") });
    const explanation = t("organizations.members.account_inactive_tooltip");
    expect(explanation).toContain("administrator");
    // A screen reader reading the table finds it beside the badge; the eye gets it as a tooltip on a trigger the keyboard can reach, which describes the focused button.
    expect(within(row).getByText(explanation).closest("button")).toBeNull();
    expect(within(await rowOf("Ada Owner")).queryByText(explanation)).toBeNull();
    act(() => badge.focus());
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(explanation);
    expect(badge.getAttribute("aria-describedby")).toBe(tooltip.id);
  });

  test("lets an owner suspend a member after confirming, and restore a suspended one", async () => {
    const suspendedAt = "2026-10-01T00:00:00Z";
    const memberList: Member[] = [
      ...members,
      { ...bob, userId: "u3", name: "Sue Suspended", email: "sue@example.com", suspendedAt },
    ];
    const fetch = stubServer({
      [`GET ${API}/members`]: { members: memberList },
      [`GET ${API}/invites`]: invites,
      [`GET ${API}/user-management`]: ownerAccess,
      "GET /api/app/auth/me": { id: "u1", name: "Ada Owner" },
      "GET /api/app/auth/options": { smtpConfigured: true },
      [`POST ${API}/members/u2/suspension`]: {
        organizationId: ORG,
        userId: "u2",
        accepted: true,
        role: "member",
        suspendedAt,
      },
      [`DELETE ${API}/members/u3/suspension`]: {
        organizationId: ORG,
        userId: "u3",
        accepted: true,
        role: "member",
        suspendedAt: null,
      },
    });
    renderInRouter(<MembersCard organization={organization("owner")} />);
    const suspend = t("organizations.members.suspend_access");
    const restore = t("organizations.members.restore_access");

    const sue = await rowOf("Sue Suspended");
    expect(within(sue).getByText(t("organizations.members.suspended"))).toBeTruthy();
    expect(within(sue).queryByText(t("common.active"))).toBeNull();
    expect(within(sue).queryByRole("button", { name: suspend })).toBeNull();
    // Nobody suspends themselves, and an invite has no access to suspend.
    expect(within(await rowOf("Ada Owner")).queryByRole("button", { name: suspend })).toBeNull();
    expect(within(await rowOf("Cy Invitee")).queryByRole("button", { name: suspend })).toBeNull();

    fireEvent.click(within(await rowOf("Bob Member")).getByRole("button", { name: suspend }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(t("organizations.members.suspend_title", { name: "Bob Member" }))
    ).toBeTruthy();
    expect(fetch.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
    fireEvent.click(within(dialog).getByRole("button", { name: suspend }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([path, init]) => init?.method === "POST" && path === `${API}/members/u2/suspension`
        )
      ).toBe(true)
    );

    fireEvent.click(within(sue).getByRole("button", { name: restore }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([path, init]) => init?.method === "DELETE" && path === `${API}/members/u3/suspension`
        )
      ).toBe(true)
    );
  });

  test("does not offer a manager to suspend an owner", async () => {
    renderMembers({
      role: "manager",
      access: ownerAccess,
      smtpConfigured: true,
      memberList: [
        { ...ada, role: "manager" },
        { ...ada, userId: "u4", name: "Olga Owner", email: "olga@example.com" },
        bob,
      ],
    });
    const suspend = t("organizations.members.suspend_access");
    expect(within(await rowOf("Olga Owner")).queryByRole("button", { name: suspend })).toBeNull();
    expect(within(await rowOf("Bob Member")).getByRole("button", { name: suspend })).toBeTruthy();
  });

  test("offers a team admin neither share nor resend, which the server refuses them", async () => {
    renderMembers({
      role: "member",
      access: { ...ownerAccess, canManageUsers: false, adminTeamIds: [TEAM] },
      smtpConfigured: true,
    });

    const invite = await rowOf("Cy Invitee");
    expect(within(invite).queryByRole("button", { name: share() })).toBeNull();
    expect(within(invite).queryByRole("button", { name: resend() })).toBeNull();
  });
});

const renderModal = ({
  role,
  adminTeamIds,
  emailConfigured,
  onCreatedWithoutEmail,
}: {
  role: OrganizationRole;
  adminTeamIds: string[];
  emailConfigured: boolean;
  onCreatedWithoutEmail: (inviteId: string) => void;
}) => {
  const fetch = stubServer({
    [`GET ${API}/teams`]: {
      userTeams: [{ id: TEAM, name: "Research", userRole: "admin", memberCount: 1 }],
      otherTeams: [],
    },
    [`POST ${API}/invites`]: { id: "new-invite" },
  });
  renderInRouter(
    <InviteMemberModal
      open
      setOpen={() => {}}
      organizationId={ORG}
      membershipRole={role}
      adminTeamIds={adminTeamIds}
      billingRoleAllowed={false}
      emailConfigured={emailConfigured}
      onCreatedWithoutEmail={onCreatedWithoutEmail}
    />
  );
  return fetch;
};

const fillAndSubmit = async () => {
  fireEvent.change(await screen.findByLabelText(t("common.full_name")), { target: { value: "Dee" } });
  fireEvent.change(screen.getByLabelText(t("common.email")), { target: { value: "dee@example.com" } });
  fireEvent.click(screen.getByRole("button", { name: t("common.invite") }));
};

const successToast = () => toast.getSnapshot().find((shown) => shown.kind === "success")?.message;

describe("InviteMemberModal without email", () => {
  test("tells an owner to share the link and hands the new invite over for it", async () => {
    const onCreatedWithoutEmail = vi.fn();
    renderModal({ role: "owner", adminTeamIds: [], emailConfigured: false, onCreatedWithoutEmail });

    expect(
      await screen.findByText(t("workspace.settings.teams.invite_email_not_configured_hint"))
    ).toBeTruthy();
    await fillAndSubmit();

    await waitFor(() => expect(onCreatedWithoutEmail).toHaveBeenCalledWith("new-invite"));
    expect(successToast()).toBe(t("workspace.settings.teams.invite_created_email_not_configured"));
  });

  test("tells a team admin to ask an owner or manager, and asks the server for no link", async () => {
    const onCreatedWithoutEmail = vi.fn();
    const fetch = renderModal({
      role: "member",
      adminTeamIds: [TEAM],
      emailConfigured: false,
      onCreatedWithoutEmail,
    });

    expect(
      await screen.findByText(t("workspace.settings.teams.invite_email_not_configured_team_admin_hint"))
    ).toBeTruthy();
    expect(screen.queryByText(t("workspace.settings.teams.invite_email_not_configured_hint"))).toBeNull();

    // A team admin invites into one of their own teams.
    const teams = screen.getByLabelText(t("common.add_to_team")) as HTMLButtonElement;
    await waitFor(() => expect(teams.disabled).toBe(false));
    fireEvent.keyDown(teams, { key: "Enter" });
    const research = await screen.findByRole("menuitemcheckbox", { name: "Research" });
    fireEvent.click(research);
    // The menu stays open for more teams; closing it gives the rest of the form back.
    fireEvent.keyDown(research, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menuitemcheckbox")).toBeNull());
    expect(screen.getByText("Research")).toBeTruthy();
    await fillAndSubmit();

    await waitFor(() =>
      expect(successToast()).toBe(t("workspace.settings.teams.invite_created_email_not_configured_ask_admin"))
    );
    expect(onCreatedWithoutEmail).not.toHaveBeenCalled();
    expect(fetch.mock.calls.some(([path]) => path.endsWith("/link"))).toBe(false);
  });
});
