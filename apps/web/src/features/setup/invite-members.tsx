import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { PlusIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { FormError } from "../../components/ui/form-error";
import { Input } from "../../components/ui/input";
import { errorMessage } from "../../lib/errors";
import { authOptionsQuery } from "../../lib/queries/auth";
import { useSetupInvite } from "../../lib/queries/invites";
import {
  MAX_SETUP_INVITES,
  type SetupInviteErrors,
  type SetupInviteRow,
  validateSetupInvites,
} from "../../lib/setup-invites";
import { toast } from "../../lib/toast";

/** forma2's onboarding invite step: up to ten people, each invited as an owner, one request at a time; skipping goes on to the product. */
export const InviteMembers = ({ organizationId }: { organizationId: string }) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const options = useQuery(authOptionsQuery);
  const [rows, setRows] = useState<SetupInviteRow[]>([{ email: "", name: "" }]);
  const [errors, setErrors] = useState<SetupInviteErrors>([]);
  const [submitting, setSubmitting] = useState(false);
  const invite = useSetupInvite(organizationId);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const result = validateSetupInvites(rows);
    setErrors(result.errors);
    if (result.errors.some((error) => error.email || error.name)) return;
    setSubmitting(true);
    for (const invitee of result.invitees) {
      try {
        await invite.mutateAsync(invitee);
        toast.success(t("setup.invite.invitation_sent_to_email", { email: invitee.email }));
      } catch (error) {
        toast.error(errorMessage(t, error));
      }
    }
    setSubmitting(false);
    await navigate({ to: "/" });
  };

  const setRow = (index: number, row: Partial<SetupInviteRow>) =>
    setRows(rows.map((current, i) => (i === index ? { ...current, ...row } : current)));

  return (
    <>
      {options.data && !options.data.smtpConfigured && (
        <Alert variant="warning" role="status" className="text-left">
          <AlertTitle>{t("setup.invite.smtp_not_configured")}</AlertTitle>
          <AlertDescription>{t("setup.invite.smtp_not_configured_description")}</AlertDescription>
        </Alert>
      )}
      <form onSubmit={submit} className="space-y-4" noValidate>
        <div className="flex flex-col items-center gap-y-4">
          <h1 className="text-2xl font-medium">{t("setup.invite.invite_your_organization_members")}</h1>
          <p>{t("setup.invite.life_s_no_fun_alone")}</p>
          {rows.map((row, index) => (
            // oxlint-disable-next-line react/no-array-index-key -- invite rows are positional and only ever appended
            <div key={index} className="space-y-2">
              <div>
                <Input
                  value={row.email}
                  onChange={(e) => setRow(index, { email: e.target.value })}
                  placeholder="user@example.com"
                  aria-label={t("common.email")}
                  type="email"
                  className="w-80"
                  isInvalid={!!errors[index]?.email}
                />
                <FormError className="text-left">{errors[index]?.email}</FormError>
              </div>
              <div>
                <Input
                  value={row.name}
                  onChange={(e) => setRow(index, { name: e.target.value })}
                  placeholder={t("common.full_name")}
                  aria-label={t("common.full_name")}
                  className="w-80"
                  isInvalid={!!errors[index]?.name}
                />
                <FormError className="text-left">{errors[index]?.name}</FormError>
              </div>
            </div>
          ))}
          <Button
            variant="ghost"
            type="button"
            disabled={rows.length >= MAX_SETUP_INVITES}
            onClick={() => setRows([...rows, { email: "", name: "" }].slice(0, MAX_SETUP_INVITES))}>
            <PlusIcon />
            {t("setup.invite.add_another_member")}
          </Button>
          <hr className="my-6 w-full border-slate-200" />
          <div className="space-y-2">
            <Button className="flex w-80 justify-center" type="submit" loading={submitting}>
              {t("setup.invite.continue")}
            </Button>
            <Button
              type="button"
              variant="ghost"
              className="flex w-80 justify-center"
              onClick={() => void navigate({ to: "/" })}>
              {t("setup.invite.skip")}
            </Button>
          </div>
        </div>
      </form>
    </>
  );
};
