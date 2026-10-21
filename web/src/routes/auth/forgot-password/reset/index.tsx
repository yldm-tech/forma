import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { authApi } from "../../../../features/auth/api";
import { PasswordChecks } from "../../../../features/auth/components/password-checks";
import { Alert, Button, Field, PasswordInput } from "../../../../features/auth/components/ui";
import { resetPasswordFailure } from "../../../../features/auth/errors";
import { redirectIfSignedIn } from "../../../../features/auth/guards";
import { tokenSearch } from "../../../../features/auth/search";
import { passwordError } from "../../../../features/auth/validation";
import { type Validators, useForm } from "../../../../lib/form";

// The link in the password reset email.
export const Route = createFileRoute("/auth/forgot-password/reset/")({
  validateSearch: tokenSearch,
  beforeLoad: redirectIfSignedIn,
  component: ResetPasswordPage,
});

interface ResetValues {
  password: string;
  confirmPassword: string;
}

// Both fields follow the password policy and the submit button stays disabled until they do; whether they match is checked on submit, as forma2 did.
const RULES: Validators<ResetValues> = {
  password: passwordError,
  confirmPassword: passwordError,
};

/** A successful reset signs every session out, so the next page offers to sign in. */
function ResetPasswordPage() {
  const { t } = useTranslation();
  const { token } = Route.useSearch();
  const navigate = useNavigate();
  const form = useForm<ResetValues>({
    defaultValues: { password: "", confirmPassword: "" },
    validators: RULES,
    onSubmit: async ({ password, confirmPassword }) => {
      if (password !== confirmPassword) {
        form.setFormError(t("auth.forgot-password.reset.passwords_do_not_match"));
        return;
      }
      if (!token) {
        form.setFormError(t("auth.forgot-password.reset.no_token_provided"));
        return;
      }
      try {
        await authApi.resetPassword(token, password);
      } catch (error) {
        const failure = resetPasswordFailure(t, error);
        if (failure.fields.password) form.setFieldError("password", failure.fields.password);
        form.setFormError(failure.message);
        return;
      }
      await navigate({ to: "/auth/forgot-password/reset/success" });
    },
  });

  return (
    <form onSubmit={(event) => void form.handleSubmit(event)} className="space-y-6" noValidate>
      {form.formError && <Alert variant="error">{form.formError}</Alert>}
      <div className="space-y-4">
        <Field
          id="password"
          label={t("auth.forgot-password.reset.new_password")}
          error={form.errors.password}>
          <PasswordInput
            id="password"
            name="password"
            autoComplete="new-password"
            placeholder="*******"
            required
            value={form.values.password}
            error={form.errors.password}
            onChange={(event) => form.setValue("password", event.target.value)}
          />
        </Field>
        <Field
          id="confirmPassword"
          label={t("auth.forgot-password.reset.confirm_password")}
          error={form.errors.confirmPassword}>
          <PasswordInput
            id="confirmPassword"
            name="confirmPassword"
            autoComplete="new-password"
            placeholder="*******"
            required
            value={form.values.confirmPassword}
            error={form.errors.confirmPassword}
            onChange={(event) => form.setValue("confirmPassword", event.target.value)}
          />
        </Field>
        <PasswordChecks password={form.values.password} />
      </div>
      <Button type="submit" className="w-full" disabled={!form.isValid} loading={form.isSubmitting}>
        {t("auth.forgot-password.reset_password")}
      </Button>
    </form>
  );
}
