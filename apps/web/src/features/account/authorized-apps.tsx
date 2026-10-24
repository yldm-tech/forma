import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { LoadingBlock, SettingsCard } from "../../components/ui/page";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { errorMessage } from "../../lib/errors";
import { type OAuthConsent, oauthConsentsQuery, useRevokeOAuthConsent } from "../../lib/queries/oauth";
import { formatDateTimeForDisplay } from "../../lib/time";
import { toast } from "../../lib/toast";
import { hostOf, scopeLabel } from "./oauth";

/**
 * forma2's AuthorizedAppsCard: the MCP clients that may act as this user, and the control to stop one. It sits on the profile page beside two-factor authentication, as a question about the account's security. Revoking ends the client's refresh tokens; an access token it already holds runs out within 15 minutes.
 */
export const AuthorizedAppsCard = ({ locale }: { locale: string }) => {
  const { t } = useTranslation();
  const consents = useQuery(oauthConsentsQuery);

  return (
    <SettingsCard
      width="full"
      title={t("auth.oauth.authorized_apps_title")}
      description={t("auth.oauth.authorized_apps_description")}
      cta={
        <Button asChild variant="secondary" size="sm">
          <a
            href="https://forma.yldm.ai/docs/platform/mcp/overview"
            target="_blank"
            rel="noopener noreferrer">
            {t("auth.oauth.mcp_docs_link")}
          </a>
        </Button>
      }>
      {consents.isPending ? (
        <LoadingBlock />
      ) : consents.isError ? (
        <p className="text-sm text-red-600">{errorMessage(t, consents.error)}</p>
      ) : consents.data.length === 0 ? (
        <p className="text-sm text-slate-600">{t("auth.oauth.no_authorized_apps")}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("auth.oauth.client")}</TableHead>
              <TableHead>{t("auth.oauth.permissions")}</TableHead>
              <TableHead>{t("common.created_at")}</TableHead>
              <TableHead>{t("common.updated_at")}</TableHead>
              <TableHead className="text-right">{t("common.action")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {consents.data.map((consent) => (
              <AuthorizedAppRow key={consent.id} consent={consent} locale={locale} />
            ))}
          </TableBody>
        </Table>
      )}
    </SettingsCard>
  );
};

const AuthorizedAppRow = ({ consent, locale }: { consent: OAuthConsent; locale: string }) => {
  const { t } = useTranslation();
  const revoke = useRevokeOAuthConsent();
  const { client } = consent;

  const onRevoke = async () => {
    if (!window.confirm(t("auth.oauth.revoke_confirmation"))) return;
    try {
      await revoke.mutateAsync(consent.id);
      toast.success(t("auth.oauth.consent_revoked"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <TableRow className="hover:bg-slate-100">
      <TableCell>
        <div className="space-y-1">
          <p className="font-medium text-slate-900">{client.name || client.clientId}</p>
          <p className="text-xs break-all text-slate-500">
            {hostOf(client.uri) ?? client.uri ?? client.clientId}
          </p>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1.5">
          {consent.scopes.map((scope) => (
            <span
              key={scope}
              className="rounded-md border border-slate-200 bg-slate-50 px-2 py-1 text-xs font-medium text-slate-700">
              {scopeLabel(scope, t)}
            </span>
          ))}
        </div>
      </TableCell>
      <TableCell className="text-slate-600">{formatDateTimeForDisplay(consent.createdAt, locale)}</TableCell>
      <TableCell className="text-slate-600">{formatDateTimeForDisplay(consent.updatedAt, locale)}</TableCell>
      <TableCell className="text-right">
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={() => void onRevoke()}
          loading={revoke.isPending}>
          {t("auth.oauth.revoke")}
        </Button>
      </TableCell>
    </TableRow>
  );
};
