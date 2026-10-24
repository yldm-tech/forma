import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { type InviteStatus, authApi, inviteInspectionQuery, sessionQuery } from "../features/auth/api";
import { inviteUrl } from "../features/auth/callback-url";
import { Button, ButtonLink, ContentLayout } from "../features/auth/components/ui";
import { type Acceptance, acceptanceFailure, invitePageState } from "../features/auth/invite";
import { tokenSearch } from "../features/auth/search";
import { untilAuthorized } from "../lib/consistency";
import { errorMessage } from "../lib/errors";
import { signOut } from "../lib/queries/auth";
import { organizationNavigationQuery } from "../lib/queries/organizations";
import { toast } from "../lib/toast";

export const Route = createFileRoute("/invite")({
  validateSearch: tokenSearch,
  component: InvitePage,
});

/**
 * Accepting is a CSRF-checked POST the page sends once it has shown the visitor is the invitee, never a side effect of fetching the page: forma2 accepted during the GET render, so a link prefetch could join an organization. Every state names what the link is for when the server says so, and leaves the visitor a way on.
 */
function InvitePage() {
  const { t } = useTranslation();
  const { token = "" } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useQuery(sessionQuery);
  const inspection = useQuery({
    ...inviteInspectionQuery(token, session.data?.id ?? null),
    enabled: token !== "" && session.isSuccess,
  });
  const [acceptance, setAcceptance] = useState<Acceptance>("idle");
  // What "Go to app" opens once the invite is accepted: the organization just joined, or the root page (no id) when its permissions never arrived.
  const [joined, setJoined] = useState<{ organizationId?: string; organizationName?: string }>();
  const [switching, setSwitching] = useState(false);
  const accepting = useRef(false);

  const signedIn = session.data != null;
  const inspected: InviteStatus | undefined =
    token === "" || inspection.isError ? "not_found" : inspection.data?.status;
  const state = inspected ? invitePageState(inspected, signedIn, acceptance) : undefined;
  const { email, organizationName, inviterName } = inspection.data ?? {};

  useEffect(() => {
    if (state !== "accepting" || accepting.current) return;
    accepting.current = true;
    // The answer to the inspection is gone once the invite is used, so the name is kept from it now.
    const name = organizationName;
    authApi.acceptInvite(token).then(
      async ({ organizationId }) => {
        // The membership reaches the server's authorization a few seconds after the accept; until it does the organization answers 404, so the link waits for it rather than leading to a not-found.
        const reachable = await untilAuthorized(() =>
          queryClient.fetchQuery(organizationNavigationQuery(organizationId))
        ).then(
          () => true,
          () => false
        );
        setJoined({ organizationId: reachable ? organizationId : undefined, organizationName: name });
        setAcceptance("accepted");
        // The new membership changes what the app shows this user.
        void queryClient.invalidateQueries();
      },
      (error: unknown) => setAcceptance(acceptanceFailure(error))
    );
  }, [organizationName, queryClient, state, token]);

  /** Signs the current account out and opens sign-in for the invited address, coming back to this invite. */
  const switchAccount = async () => {
    setSwitching(true);
    try {
      await signOut(queryClient, { reason: "user_initiated" });
    } catch (error) {
      setSwitching(false);
      toast.error(errorMessage(t, error));
      return;
    }
    await navigate({
      to: "/auth/login",
      search: { callbackUrl: inviteUrl(token, window.location.origin), email },
    });
  };

  const goOn = signedIn ? (
    <ButtonLink to="/">{t("auth.invite.go_to_app")}</ButtonLink>
  ) : (
    <ButtonLink to="/auth/login">{t("auth.invite.login")}</ButtonLink>
  );

  if (session.isError) {
    return (
      <ContentLayout
        headline={t("auth.invite.invite_not_found")}
        description={<p>{t("common.something_went_wrong_please_try_again")}</p>}
      />
    );
  }

  switch (state) {
    case undefined:
    case "accepting":
      return <ContentLayout headline={t("common.loading")} />;
    case "not_found":
      return (
        <ContentLayout
          headline={t("auth.invite.invite_not_found")}
          description={
            <>
              <p>{t("auth.invite.invite_not_found_description")}</p>
              {/* Signing up through an invite already joins its organization, so the link the sign-in returns to is used up by then. */}
              {signedIn && <p>{t("auth.invite.already_used_signed_in")}</p>}
            </>
          }>
          {goOn}
        </ContentLayout>
      );
    case "expired":
      return (
        <ContentLayout
          headline={t("auth.invite.invite_expired")}
          description={<p>{t("auth.invite.invite_expired_description")}</p>}>
          {goOn}
        </ContentLayout>
      );
    case "signed_out": {
      const callbackUrl = inviteUrl(token, window.location.origin);
      return (
        <ContentLayout
          headline={t("auth.invite.happy_to_have_you")}
          description={
            <>
              {organizationName && (
                <p className="font-medium text-slate-800">
                  {inviterName
                    ? t("auth.invite.invited_you_to_join", { inviterName, organizationName })
                    : t("auth.invite.you_are_invited_to_join", { organizationName })}
                </p>
              )}
              <p>{t("auth.invite.happy_to_have_you_description")}</p>
            </>
          }>
          <ButtonLink to="/auth/signup" search={{ inviteToken: token, email }}>
            {t("auth.invite.create_account")}
          </ButtonLink>
          <ButtonLink to="/auth/login" search={{ callbackUrl, email }} variant="secondary">
            {t("auth.invite.login")}
          </ButtonLink>
        </ContentLayout>
      );
    }
    case "email_mismatch":
      return (
        <ContentLayout
          headline={t("auth.invite.email_does_not_match")}
          description={
            <p>
              {email
                ? t("auth.invite.email_does_not_match_for", { email, currentEmail: session.data?.email ?? "" })
                : t("auth.invite.email_does_not_match_description")}
            </p>
          }>
          <Button onClick={() => void switchAccount()} loading={switching}>
            {t("auth.invite.switch_account")}
          </Button>
          <ButtonLink to="/" variant="secondary">
            {t("auth.invite.go_to_app")}
          </ButtonLink>
        </ContentLayout>
      );
    case "accepted":
      return (
        <ContentLayout
          headline={
            joined?.organizationName
              ? t("auth.invite.you_joined", { organizationName: joined.organizationName })
              : t("auth.invite.welcome_to_organization")
          }
          description={<p>{t("auth.invite.welcome_to_organization_description")}</p>}>
          {joined?.organizationId ? (
            <ButtonLink to="/organizations/$organizationId" params={{ organizationId: joined.organizationId }}>
              {t("auth.invite.go_to_app")}
            </ButtonLink>
          ) : (
            <ButtonLink to="/">{t("auth.invite.go_to_app")}</ButtonLink>
          )}
        </ContentLayout>
      );
  }
}
