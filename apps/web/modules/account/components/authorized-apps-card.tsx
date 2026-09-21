import { headers } from "next/headers";
import type { TUserLocale } from "@forma/types/user";
import { formatDateTimeForDisplay } from "@/lib/utils/datetime";
import { getTranslate } from "@/lingodotdev/server";
import { RevokeOAuthConsentButton } from "@/modules/account/components/revoke-oauth-consent-button";
import { auth } from "@/modules/auth/lib/auth";
import {
  type TOAuthPublicClient,
  getHostFromUrl,
  getOAuthScopeLabel,
} from "@/modules/auth/lib/oauth-client-metadata";
import { Button } from "@/modules/ui/components/button";
import { SettingsCard, type TSettingsCardWidth } from "@/modules/ui/components/settings-card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/modules/ui/components/table";

type TOAuthConsent = {
  id: string;
  clientId: string;
  scopes: string[];
  createdAt: Date | string;
  updatedAt: Date | string;
};

type TAuthorizedApp = {
  consent: TOAuthConsent;
  client: TOAuthPublicClient | null;
};

const getAuthorizedApps = async (): Promise<TAuthorizedApp[]> => {
  const requestHeaders = await headers();
  const consents = (await auth.api.getOAuthConsents({ headers: requestHeaders })) as TOAuthConsent[];

  return await Promise.all(
    consents.map(async (consent) => {
      try {
        const client = (await auth.api.getOAuthClientPublic({
          query: { client_id: consent.clientId },
          headers: requestHeaders,
        })) as TOAuthPublicClient;

        return { consent, client };
      } catch {
        return { consent, client: null };
      }
    })
  );
};

/**
 * Which clients may act as this user, and the control to stop one.
 *
 * It sits on the profile page beside two-factor authentication rather than on a page of its own: it
 * answers a question about the account's security, and a dedicated destination spent a slot in the
 * personal menu on a list that is empty until someone connects an MCP client.
 */
export const AuthorizedAppsCard = async ({
  locale,
  width,
  className,
}: Readonly<{ locale: TUserLocale; width?: TSettingsCardWidth; className?: string }>) => {
  const t = await getTranslate();
  const apps = await getAuthorizedApps();

  return (
    <SettingsCard
      width={width}
      className={className}
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
      {apps.length === 0 ? (
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
            {apps.map(({ consent, client }) => {
              const clientName = client?.client_name ?? consent.clientId;
              const clientHost = getHostFromUrl(client?.client_uri);

              return (
                <TableRow key={consent.id} className="hover:bg-slate-100">
                  <TableCell>
                    <div className="space-y-1">
                      <p className="font-medium text-slate-900">{clientName}</p>
                      <p className="text-xs break-all text-slate-500">
                        {clientHost ?? client?.client_uri ?? consent.clientId}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1.5">
                      {consent.scopes.map((scope) => (
                        <span
                          key={scope}
                          className="rounded-md border border-slate-200 bg-slate-50 px-2 py-1 text-xs font-medium text-slate-700">
                          {getOAuthScopeLabel(scope, t)}
                        </span>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="text-slate-600">
                    {formatDateTimeForDisplay(new Date(consent.createdAt), locale)}
                  </TableCell>
                  <TableCell className="text-slate-600">
                    {formatDateTimeForDisplay(new Date(consent.updatedAt), locale)}
                  </TableCell>
                  <TableCell className="text-right">
                    <RevokeOAuthConsentButton consentId={consent.id} />
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </SettingsCard>
  );
};
