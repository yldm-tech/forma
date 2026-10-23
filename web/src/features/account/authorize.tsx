import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { LoadingBlock } from "../../components/ui/page";
import { Wordmark } from "../../components/ui/logo";
import { authorizationRequestQuery, useAnswerAuthorizationRequest } from "../../lib/queries/oauth";
import { toast } from "../../lib/toast";
import { consentRedirectUrl, hostOf, isLocalhostHost, scopeLabel } from "./oauth";

const Frame = ({ children }: { children: ReactNode }) => {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4 py-10">
      <div className="w-full max-w-xl rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        <div className="mb-6 flex justify-center">
          <a
            target="_blank"
            href="https://forma.ylam.ai?utm_source=forma-app&utm_medium=webapp&utm_campaign=oauth_consent_logo"
            rel="noopener noreferrer"
            aria-label={t("auth.oauth.forma_website")}>
            <Wordmark className="text-xl" />
          </a>
        </div>
        {children}
      </div>
    </div>
  );
};

/**
 * forma2's consent page (/account/authorize): an MCP client is asking to act as the signed-in user. It shows the client, where it sends the browser back and the permissions it asks for, and the answer sends the browser on to the client. A user who already approved all of it for this client, and only signs in first, is not asked again.
 */
export const AuthorizePage = ({ requestId }: { requestId?: string }) => {
  const { t } = useTranslation();
  const request = useQuery({
    ...authorizationRequestQuery(requestId ?? ""),
    enabled: requestId !== undefined,
  });
  const answer = useAnswerAuthorizationRequest();
  const continued = useRef(false);

  const respond = async (accept: boolean) => {
    if (!requestId) return;
    try {
      const url = consentRedirectUrl(await answer.mutateAsync({ requestId, accept }));
      if (!url) {
        toast.error(t("auth.oauth.consent_failed"));
        return;
      }
      window.location.href = url;
    } catch {
      toast.error(t("auth.oauth.consent_failed"));
    }
  };

  const approvedBefore = request.data?.consentRequired === false;
  useEffect(() => {
    if (!approvedBefore || continued.current) return;
    continued.current = true;
    void respond(true);
  });

  if (!requestId || request.isError) {
    return (
      <Frame>
        <Alert variant="error" role="status">
          <AlertTitle>{t("auth.oauth.invalid_oauth_request")}</AlertTitle>
          <AlertDescription>{t("auth.oauth.invalid_oauth_request_description")}</AlertDescription>
        </Alert>
      </Frame>
    );
  }
  if (!request.data || approvedBefore) {
    return (
      <Frame>
        <LoadingBlock />
      </Frame>
    );
  }

  const { client, redirectUri, scopes } = request.data;
  const clientName = client.name || t("auth.oauth.unknown_client");
  const redirectHost = hostOf(redirectUri) ?? hostOf(client.uri);
  const pending = answer.isPending ? answer.variables?.accept : undefined;

  return (
    <Frame>
      <div className="space-y-1">
        <p className="text-sm font-medium text-slate-500">{t("auth.oauth.authorization_request")}</p>
        <h1 className="text-2xl font-semibold text-slate-900">
          {t("auth.oauth.authorize_title", { clientName })}
        </h1>
        <p className="text-sm text-slate-600">{t("auth.oauth.authorize_description")}</p>
      </div>

      <dl className="mt-6 space-y-4 text-sm">
        <div>
          <dt className="font-medium text-slate-900">{t("auth.oauth.client")}</dt>
          <dd className="mt-1 text-slate-600">{clientName}</dd>
        </div>
        {redirectHost && (
          <div>
            <dt className="font-medium text-slate-900">{t("auth.oauth.redirect_host")}</dt>
            <dd className="mt-1 text-slate-600">{redirectHost}</dd>
          </div>
        )}
        {client.uri && (
          <div>
            <dt className="font-medium text-slate-900">{t("auth.oauth.client_uri")}</dt>
            <dd className="mt-1 break-all text-slate-600">{client.uri}</dd>
          </div>
        )}
        {scopes.length > 0 && (
          <div>
            <dt className="font-medium text-slate-900">{t("auth.oauth.requested_permissions")}</dt>
            <dd className="mt-2 flex flex-wrap gap-2">
              {scopes.map((scope) => (
                <span
                  key={scope}
                  className="rounded-md border border-slate-200 bg-slate-50 px-2 py-1 text-xs font-medium text-slate-700">
                  {scopeLabel(scope, t)}
                </span>
              ))}
            </dd>
          </div>
        )}
      </dl>

      {isLocalhostHost(hostOf(redirectUri)) && (
        <Alert variant="warning" className="mt-6" role="status">
          <AlertTitle>{t("auth.oauth.localhost_redirect_warning")}</AlertTitle>
          <AlertDescription>{t("auth.oauth.localhost_redirect_warning_description")}</AlertDescription>
        </Alert>
      )}

      <div className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button
          type="button"
          variant="secondary"
          onClick={() => void respond(false)}
          loading={pending === false}
          disabled={answer.isPending}>
          {t("auth.oauth.deny_access")}
        </Button>
        <Button
          type="button"
          onClick={() => void respond(true)}
          loading={pending === true}
          disabled={answer.isPending}>
          {t("auth.oauth.allow_access")}
        </Button>
      </div>
    </Frame>
  );
};
