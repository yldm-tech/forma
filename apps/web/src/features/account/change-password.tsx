import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { FormError } from "../../components/ui/form-error";
import { Label } from "../../components/ui/label";
import { PasswordInput } from "../../components/ui/password-input";
import { errorMessage } from "../../lib/errors";
import { type Validators, useForm } from "../../lib/form";
import {
  authOptionsQuery,
  signOut,
  useChangePassword,
  useRequestPasswordResetLink,
} from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { PasswordChecks } from "../auth/components/password-checks";
import { changePasswordFailure } from "../auth/errors";
import { passwordError, validationMessage } from "../auth/validation";

/**
 * The password part of the Security card: change it with the current one, or, where the instance mails reset links, have one sent instead. Only password users see the card.
 */
export const ChangePassword = () => {
  // A changed password starts the form over, empty.
  const [generation, setGeneration] = useState(0);
  return (
    <div className="space-y-4 border-b border-slate-200 pb-6">
      <ChangePasswordForm key={generation} onChanged={() => setGeneration((n) => n + 1)} />
      <ResetLink />
    </div>
  );
};

interface ChangePasswordValues {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

const EMPTY: ChangePasswordValues = { currentPassword: "", newPassword: "", confirmPassword: "" };

/**
 * The current password, as the server checks it, and a new one held to the sign-up policy and confirmed. The server signs every other session out, which the form says before the user submits.
 */
export const ChangePasswordForm = ({ onChanged }: { onChanged: () => void }) => {
  const { t } = useTranslation();
  const change = useChangePassword();
  // Any non-empty current password is sent: one that predates the composition rule must still be accepted.
  const rules = useMemo<Validators<ChangePasswordValues>>(
    () => ({
      currentPassword: (value) =>
        value === "" ? t("workspace.settings.profile.password_change.current_password_required") : undefined,
      newPassword: (value) => {
        const key = passwordError(value);
        return key && validationMessage(t, key);
      },
      confirmPassword: (value, values) =>
        value === values.newPassword ? undefined : t("auth.forgot-password.reset.passwords_do_not_match"),
    }),
    [t]
  );
  const form = useForm<ChangePasswordValues>({
    defaultValues: EMPTY,
    validators: rules,
    onSubmit: async ({ currentPassword, newPassword }) => {
      try {
        await change.mutateAsync({ currentPassword, newPassword });
      } catch (error) {
        const failure = changePasswordFailure(t, error);
        if (failure.fields.currentPassword)
          form.setFieldError("currentPassword", failure.fields.currentPassword);
        if (failure.fields.newPassword) form.setFieldError("newPassword", failure.fields.newPassword);
        form.setFormError(failure.message);
        return;
      }
      toast.success(t("workspace.settings.profile.password_change.password_changed"));
      onChanged();
    },
  });

  const field = (name: keyof ChangePasswordValues, label: string, autoComplete: string) => {
    const id = `change-password-${name}`;
    const error = form.errors[name];
    return (
      <div className="space-y-2">
        <Label htmlFor={id}>{label}</Label>
        <PasswordInput
          id={id}
          name={name}
          autoComplete={autoComplete}
          required
          value={form.values[name]}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `${id}-error` : undefined}
          onChange={(event) => form.setValue(name, event.target.value)}
        />
        <FormError id={`${id}-error`}>{error}</FormError>
      </div>
    );
  };

  return (
    <form
      className="w-full max-w-sm space-y-4"
      onSubmit={(event) => void form.handleSubmit(event)}
      noValidate
      aria-labelledby="change-password-title">
      <div>
        <p id="change-password-title" className="text-sm font-semibold text-slate-800">
          {t("workspace.settings.profile.password_change.title")}
        </p>
        <p className="text-xs text-slate-600">
          {t("workspace.settings.profile.password_change.other_sessions_signed_out_hint")}
        </p>
      </div>
      {field(
        "currentPassword",
        t("workspace.settings.profile.password_change.current_password"),
        "current-password"
      )}
      {field("newPassword", t("auth.forgot-password.reset.new_password"), "new-password")}
      {field("confirmPassword", t("auth.forgot-password.reset.confirm_password"), "new-password")}
      <PasswordChecks password={form.values.newPassword} />
      <FormError>{form.formError}</FormError>
      <Button type="submit" size="sm" loading={form.isSubmitting}>
        {t("workspace.settings.profile.password_change.submit")}
      </Button>
    </form>
  );
};

/** The emailed reset link, for a user who no longer knows the current password. It is mailed, so without SMTP it is not offered. Requesting one signs this browser out, as forma2's did. */
const ResetLink = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const options = useQuery(authOptionsQuery);
  const resetLink = useRequestPasswordResetLink();
  if (!options.data?.passwordResetEnabled || !options.data.smtpConfigured) return null;

  const resetPassword = async () => {
    try {
      await resetLink.mutateAsync();
    } catch (error) {
      toast.error(errorMessage(t, error));
      return;
    }
    toast.success(t("auth.forgot-password.email-sent.heading"));
    await signOut(queryClient, { reason: "password_reset" });
    await navigate({ href: "/auth/login" });
  };

  return (
    <div className="flex max-w-sm flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <div>
        <p className="text-sm text-slate-700">
          {t("workspace.settings.profile.password_change.forgot_current_password")}
        </p>
        <p className="text-xs text-slate-500">{t("auth.forgot-password.reset_password_description")}</p>
      </div>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        onClick={() => void resetPassword()}
        loading={resetLink.isPending}>
        {t("workspace.settings.profile.password_change.email_reset_link")}
      </Button>
    </div>
  );
};
