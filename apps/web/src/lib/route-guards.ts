import { notFound, redirect } from "@tanstack/react-router";
import { ApiError } from "./api";
import { BILLING_ROLE_CODE } from "./queries/workspaces";

/** The server's refusal of a member whose organization requires two-factor authentication they have not turned on; details.organizationId names the organization. */
export const TWO_FACTOR_REQUIRED_CODE = "organization_requires_two_factor";

/** The page that guides a member to the two-factor authentication their organization requires. */
export const twoFactorRequiredPath = (organizationId: string): string =>
  `/organizations/${organizationId}/two-factor`;

/**
 * Turns the refusals a page's first request can meet into navigation: a billing member asking for a workspace goes where the server says (forma2's billing redirect), a member refused for want of two-factor authentication goes to turn it on, a resource the caller may not see is a not-found, and anything else is the route's error.
 */
export const guardPageError = (error: unknown, missing: "notFound" | "home" = "notFound"): never => {
  if (error instanceof ApiError) {
    const target = error.details?.redirect;
    if (error.code === BILLING_ROLE_CODE && typeof target === "string")
      throw redirect({ href: target, replace: true });
    const organizationId = error.details?.organizationId;
    if (error.code === TWO_FACTOR_REQUIRED_CODE && typeof organizationId === "string")
      throw redirect({ href: twoFactorRequiredPath(organizationId), replace: true });
    if (error.status === 403 || error.status === 404) {
      if (missing === "home") throw redirect({ href: "/", replace: true });
      throw notFound();
    }
  }
  throw error;
};
