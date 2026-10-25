import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { ConfirmationModal } from "../../components/ui/confirmation-modal";
import { LoadingBlock, SettingsCard } from "../../components/ui/page";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { errorMessage } from "../../lib/errors";
import {
  type UserSession,
  sessionsQuery,
  useRevokeOtherSessions,
  useRevokeSession,
} from "../../lib/queries/auth";
import { formatDateTimeForDisplay, timeSince } from "../../lib/time";
import { toast } from "../../lib/toast";
import { describeUserAgent } from "./user-agent";

/**
 * Where the user is signed in: every session that has not expired, with the browser and address it was opened from, when it was opened and when it was last used. Any session but this browser's can be signed out, one at a time or all at once; this browser signs out from the account menu. A user who suspects a stolen session can end it here without resetting the password.
 */
export const SessionsCard = ({ locale }: { locale: string }) => {
  const { t } = useTranslation();
  const sessions = useQuery(sessionsQuery);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const revokeOthers = useRevokeOtherSessions();
  const others = sessions.data?.filter((session) => !session.current).length ?? 0;

  const onRevokeOthers = async () => {
    try {
      const { revoked } = await revokeOthers.mutateAsync();
      toast.success(t("workspace.settings.profile.sessions.other_sessions_signed_out", { count: revoked }));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setConfirmOpen(false);
    }
  };

  return (
    <SettingsCard
      width="full"
      title={t("workspace.settings.profile.sessions.title")}
      description={t("workspace.settings.profile.sessions.description")}
      cta={
        others > 0 && (
          <Button type="button" variant="secondary" size="sm" onClick={() => setConfirmOpen(true)}>
            {t("workspace.settings.profile.sessions.sign_out_other_sessions")}
          </Button>
        )
      }>
      {sessions.isPending ? (
        <LoadingBlock />
      ) : sessions.isError ? (
        <p className="pb-4 text-sm text-red-600">{errorMessage(t, sessions.error)}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("workspace.settings.profile.sessions.device")}</TableHead>
              <TableHead>{t("workspace.settings.profile.sessions.ip_address")}</TableHead>
              <TableHead>{t("workspace.settings.profile.sessions.signed_in")}</TableHead>
              <TableHead>{t("workspace.settings.profile.sessions.last_active")}</TableHead>
              <TableHead className="text-right">{t("common.action")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sessions.data.map((session) => (
              <SessionRow key={session.id} session={session} locale={locale} />
            ))}
          </TableBody>
        </Table>
      )}
      <ConfirmationModal
        open={confirmOpen}
        setOpen={setConfirmOpen}
        title={t("workspace.settings.profile.sessions.sign_out_other_sessions")}
        body={t("workspace.settings.profile.sessions.sign_out_other_sessions_confirmation", {
          count: others,
        })}
        buttonText={t("workspace.settings.profile.sessions.sign_out_other_sessions")}
        buttonLoading={revokeOthers.isPending}
        onConfirm={() => void onRevokeOthers()}
      />
    </SettingsCard>
  );
};

const SessionRow = ({ session, locale }: { session: UserSession; locale: string }) => {
  const { t } = useTranslation();
  const revoke = useRevokeSession();
  const device = useDeviceLabel(session.userAgent);

  const onRevoke = async () => {
    try {
      await revoke.mutateAsync(session.id);
      toast.success(t("workspace.settings.profile.sessions.session_signed_out"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <TableRow className="hover:bg-slate-100">
      <TableCell>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-slate-900" title={session.userAgent ?? undefined}>
            {device}
          </span>
          {session.current && (
            <Badge
              type="success"
              size="tiny"
              className="whitespace-nowrap"
              text={t("workspace.settings.profile.sessions.this_device")}
            />
          )}
        </div>
      </TableCell>
      <TableCell className="text-slate-600">
        {session.ipAddress ?? t("workspace.settings.profile.sessions.unknown")}
      </TableCell>
      <TableCell className="text-slate-600">{formatDateTimeForDisplay(session.createdAt, locale)}</TableCell>
      <TableCell className="text-slate-600" title={formatDateTimeForDisplay(session.lastActiveAt, locale)}>
        {session.current
          ? t("workspace.settings.profile.sessions.active_now")
          : timeSince(session.lastActiveAt, locale)}
      </TableCell>
      <TableCell className="text-right">
        {!session.current && (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            aria-label={t("workspace.settings.profile.sessions.sign_out_session", { device })}
            onClick={() => void onRevoke()}
            loading={revoke.isPending}>
            {t("workspace.settings.profile.sessions.sign_out")}
          </Button>
        )}
      </TableCell>
    </TableRow>
  );
};

/** "Chrome on macOS", or as much of it as the header names. */
const useDeviceLabel = (userAgent: string | null): string => {
  const { t } = useTranslation();
  const { browser, os } = describeUserAgent(userAgent);
  if (browser && os) return t("workspace.settings.profile.sessions.browser_on_os", { browser, os });
  return browser ?? os ?? t("workspace.settings.profile.sessions.unknown_device");
};
