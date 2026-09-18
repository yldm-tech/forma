import { describe, expect, test } from "vitest";
import { getNavigationDestinations } from "./destinations";

// The palette shows labels, but what is worth proving is which destinations a role is offered.
// Identity keeps the assertions readable: a label is its own key.
const t = ((key: string) => key) as never;

const build = (overrides: Partial<Parameters<typeof getNavigationDestinations>[0]> = {}) =>
  getNavigationDestinations({
    t,
    workspaceId: "ws_1",
    organizationId: "org_1",
    isBilling: false,
    isOwnerOrManager: true,
    isFormaCloud: false,
    ...overrides,
  });

const ids = (destinations: ReturnType<typeof build>) => destinations.map((d) => d.id);

describe("getNavigationDestinations", () => {
  test("offers every product area and every settings page a manager can open", () => {
    const result = ids(build());

    expect(result).toEqual(
      expect.arrayContaining([
        "surveys",
        "contacts",
        "workflows",
        "user-actions",
        "integrations",
        "app-connection",
        "tags",
        "ws-general",
        "ws-look",
        "ws-languages",
        "ws-teams",
        "org-general",
        "org-teams",
        "org-api-keys",
        "account-profile",
        "account-notifications",
      ])
    );
  });

  // These are sidebar entries, which they cannot be while their paths contain `/settings` — the
  // main navigation swaps itself out for the settings sidebar on those.
  test("keeps the product areas out of the settings paths", () => {
    const result = build();

    for (const id of ["user-actions", "integrations", "tags"]) {
      const destination = result.find((d) => d.id === id);
      expect(destination?.href).toBe(`/workspaces/ws_1/${id}`);
    }
  });

  // The SDK connection had two entries: a card in Integrations and its own settings page. It is
  // now one page, inside Integrations, and the palette has to agree with the card.
  test("offers the SDK connection inside integrations", () => {
    const destination = build().find((d) => d.id === "app-connection");

    expect(destination?.href).toBe("/workspaces/ws_1/integrations/app-connection");
  });

  test("offers the billing role only what it can open", () => {
    const result = ids(build({ isBilling: true }));

    expect(result).toEqual(["org-general", "org-teams", "org-api-keys", "org-domain", "account-profile"]);
  });

  test("withholds API keys from a member, matching the settings sidebar", () => {
    const result = ids(build({ isOwnerOrManager: false }));

    expect(result).not.toContain("org-api-keys");
    expect(result).toContain("org-general");
  });

  test("offers billing on cloud only, where the page exists", () => {
    const cloud = build({ isFormaCloud: true }).find((d) => d.id === "org-billing");
    const selfHosted = build({ isFormaCloud: false }).find((d) => d.id === "org-billing");

    expect(cloud?.href).toBe("/organizations/org_1/settings/billing");
    expect(selfHosted).toBeUndefined();
  });

  test("offers the domain page only off cloud, where that settings page exists", () => {
    expect(ids(build({ isFormaCloud: true }))).not.toContain("org-domain");
    expect(ids(build({ isFormaCloud: false }))).toContain("org-domain");
  });

  test("scopes every workspace destination to the workspace it was given", () => {
    const workspaceScoped = build().filter((d) => d.href.startsWith("/workspaces/"));

    expect(workspaceScoped.length).toBeGreaterThan(0);
    for (const destination of workspaceScoped) {
      expect(destination.href.startsWith("/workspaces/ws_1/")).toBe(true);
    }
  });
});
