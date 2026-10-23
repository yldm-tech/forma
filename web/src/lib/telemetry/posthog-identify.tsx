import { useEffect } from "react";
import { initPostHogClient, postHogOptions, whenPostHogReady } from "./posthog";
import { postHogClientConfig } from "./runtime-config";

// The user posthog-js last identified, kept across remounts of the layout (signing out and in again does not reload the page), so a different user starts a fresh PostHog person.
let identifiedUserId: string | null = null;

/** The lowercased domain of an email address, empty without one; the server's analytics.EmailDomain. */
export const emailDomain = (email: string): string => {
  const at = email.indexOf("@");
  return at < 0 ? "" : email.slice(at + 1).toLowerCase();
};

/**
 * forma2's PostHogIdentify: starts posthog-js for a signed-in user, where the server configured POSTHOG_KEY, and identifies them by their user id. Like the server's events it sends the email domain, never the address or the name. Rendered by the authenticated layout; without a key it does nothing and the SDK is never downloaded.
 */
export const PostHogIdentify = ({ userId, email }: { userId: string; email: string }) => {
  useEffect(() => {
    const config = postHogClientConfig();
    if (!config) return;
    let cancelled = false;
    void initPostHogClient(config.key, postHogOptions(config)).then((posthog) => {
      if (cancelled || !posthog) return;
      if (identifiedUserId && identifiedUserId !== userId) posthog.reset();
      posthog.identify(userId, { email_domain: emailDomain(email) });
      identifiedUserId = userId;
    });
    return () => {
      cancelled = true;
    };
  }, [userId, email]);
  return null;
};

/**
 * forma2's PostHogGroupIdentify: attaches the session to the organization and workspace the user is working in, with their names. PostHogIdentify starts the SDK from the parent layout's effect, which runs after this one, so it waits for it rather than assuming it is loaded, and gives up after a few seconds on an install without a key.
 */
export const PostHogGroupIdentify = ({
  organizationId,
  organizationName,
  workspaceId,
  workspaceName,
}: {
  organizationId: string;
  organizationName: string;
  workspaceId: string;
  workspaceName: string;
}) => {
  useEffect(
    () =>
      whenPostHogReady((posthog) => {
        posthog.group("organization", organizationId, { name: organizationName });
        posthog.group("workspace", workspaceId, { name: workspaceName });
      }),
    [organizationId, organizationName, workspaceId, workspaceName]
  );
  return null;
};
