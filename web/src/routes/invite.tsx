import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { type InviteStatus, authApi, sessionQuery } from "../features/auth/api";
import { type Acceptance, acceptanceFailure, invitePageState } from "../features/auth/invite";
import { inviteUrl } from "../features/auth/callback-url";
import { ButtonLink, ContentLayout } from "../features/auth/components/ui";
import { tokenSearch } from "../features/auth/search";

export const Route = createFileRoute("/invite")({
  validateSearch: tokenSearch,
  component: InvitePage,
});

/**
 * Accepting is a CSRF-checked POST the page sends once it has shown the visitor is the invitee, never a side effect of fetching the page: forma2 accepted during the GET render, so a link prefetch could join an organization.
 */
function InvitePage() {
  const { t } = useTranslation();
  const { token = "" } = Route.useSearch();
  const queryClient = useQueryClient();
  const session = useQuery(sessionQuery);
  const inspection = useQuery({
    queryKey: ["invites", "inspect", token, session.data?.id ?? null],
    queryFn: () => authApi.inspectInvite(token),
    enabled: token !== "" && session.isSuccess,
    retry: false,
    staleTime: Infinity,
  });
  const [acceptance, setAcceptance] = useState<Acceptance>("idle");
  const accepting = useRef(false);

  const inspected: InviteStatus | undefined =
    token === "" || inspection.isError ? "not_found" : inspection.data?.status;
  const state = inspected ? invitePageState(inspected, session.data != null, acceptance) : undefined;

  useEffect(() => {
    if (state !== "accepting" || accepting.current) return;
    accepting.current = true;
    authApi.acceptInvite(token).then(
      () => {
        setAcceptance("accepted");
        // The new membership changes what the app shows this user.
        void queryClient.invalidateQueries();
      },
      (error: unknown) => setAcceptance(acceptanceFailure(error))
    );
  }, [queryClient, state, token]);

  if (session.isError) {
    return (
      <ContentLayout
        headline={t("auth.invite.invite_not_found")}
        description={t("common.something_went_wrong_please_try_again")}
      />
    );
  }

  switch (state) {
    case undefined:
    case "accepting":
      return <ContentLayout headline={t("common.loading")} description="" />;
    case "not_found":
      return (
        <ContentLayout
          headline={t("auth.invite.invite_not_found")}
          description={t("auth.invite.invite_not_found_description")}
        />
      );
    case "expired":
      return (
        <ContentLayout
          headline={t("auth.invite.invite_expired")}
          description={t("auth.invite.invite_expired_description")}
        />
      );
    case "signed_out": {
      const email = inspection.data?.email;
      const callbackUrl = inviteUrl(token, window.location.origin);
      return (
        <ContentLayout
          headline={t("auth.invite.happy_to_have_you")}
          description={t("auth.invite.happy_to_have_you_description")}>
          <ButtonLink to="/auth/signup" search={{ inviteToken: token, email }} variant="secondary">
            {t("auth.invite.create_account")}
          </ButtonLink>
          <ButtonLink to="/auth/login" search={{ callbackUrl, email }}>
            {t("auth.invite.login")}
          </ButtonLink>
        </ContentLayout>
      );
    }
    case "email_mismatch":
      return (
        <ContentLayout
          headline={t("auth.invite.email_does_not_match")}
          description={t("auth.invite.email_does_not_match_description")}>
          <ButtonLink to="/">{t("auth.invite.go_to_app")}</ButtonLink>
        </ContentLayout>
      );
    case "accepted":
      return (
        <ContentLayout
          headline={t("auth.invite.welcome_to_organization")}
          description={t("auth.invite.welcome_to_organization_description")}>
          <ButtonLink to="/">{t("auth.invite.go_to_app")}</ButtonLink>
        </ContentLayout>
      );
  }
}
