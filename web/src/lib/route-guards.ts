import { notFound, redirect } from "@tanstack/react-router";
import { ApiError } from "./api";
import { BILLING_ROLE_CODE } from "./queries/workspaces";

/**
 * Turns the refusals a page's first request can meet into navigation: a billing member asking for a workspace goes where the server says (forma2's billing redirect), a resource the caller may not see is a not-found, and anything else is the route's error.
 */
export const guardPageError = (error: unknown, missing: "notFound" | "home" = "notFound"): never => {
  if (error instanceof ApiError) {
    const target = error.details?.redirect;
    if (error.code === BILLING_ROLE_CODE && typeof target === "string") throw redirect({ href: target, replace: true });
    if (error.status === 403 || error.status === 404) {
      if (missing === "home") throw redirect({ href: "/", replace: true });
      throw notFound();
    }
  }
  throw error;
};
