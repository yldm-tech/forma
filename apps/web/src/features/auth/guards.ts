import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { authApi, sessionQuery } from "./api";
import { callbackPath } from "./callback-url";
import { stringParam } from "./search";

/**
 * Where a signed-in visitor of a signed-out screen goes instead: the validated callback URL's path when the screen has one, else the root page, which decides from there. forma2's auth layout sent a session to "/", and its proxy sent a signed-in request carrying a valid callbackUrl straight to it. Returns null for a visitor without a session.
 */
export const signedInRedirectTarget = async (
  queryClient: QueryClient,
  callbackUrl: string | undefined,
  origin: string
): Promise<string | null> => {
  const user = await queryClient.fetchQuery(sessionQuery);
  return user ? callbackPath(callbackUrl, origin) : null;
};

/** beforeLoad for the screens only a signed-out visitor needs: sign-in, sign-up, verification notices and password reset. */
export const redirectIfSignedIn = async ({
  context,
  search,
}: {
  context: { queryClient: QueryClient };
  search: object;
}): Promise<void> => {
  const callbackUrl = "callbackUrl" in search ? stringParam(search.callbackUrl) : undefined;
  const target = await signedInRedirectTarget(context.queryClient, callbackUrl, window.location.origin);
  if (target) throw redirect({ href: target, replace: true });
};

/**
 * forma2's sign-up page gate: when this instance does not take uninvited sign-ups, the page exists only for a live invite. Inspecting the link changes nothing, so it is safe to run on preload; a link the server cannot classify counts as no invite.
 */
export const signupAvailable = async (
  signupEnabled: boolean,
  inviteToken: string | undefined
): Promise<boolean> => {
  if (signupEnabled) return true;
  if (!inviteToken) return false;
  const inspection = await authApi.inspectInvite(inviteToken).catch(() => null);
  return inspection?.status === "valid";
};
