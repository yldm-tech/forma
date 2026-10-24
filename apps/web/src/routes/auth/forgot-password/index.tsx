import { useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { authApi } from "../../../features/auth/api";
import { Alert, Button, ButtonLink, Field, TextInput } from "../../../features/auth/components/ui";
import { emailFormFailure } from "../../../features/auth/errors";
import { redirectIfSignedIn } from "../../../features/auth/guards";
import { stringParam } from "../../../features/auth/search";
import { emailError, translatedRules } from "../../../features/auth/validation";
import { type Validators, useForm } from "../../../lib/form";

export const Route = createFileRoute("/auth/forgot-password/")({
  // The sign-in page passes on what was typed in its email field.
  validateSearch: (search: Record<string, unknown>): { email?: string } => ({ email: stringParam(search.email) }),
  beforeLoad: redirectIfSignedIn,
  component: ForgotPasswordPage,
});

const RULES: Validators<{ email: string }> = { email: emailError };

/** The server answers the same for every address, so the next page says only that instructions follow if an account exists. */
function ForgotPasswordPage() {
  const { t } = useTranslation();
  const { email: typedEmail } = Route.useSearch();
  const navigate = useNavigate();
  const rules = useMemo(() => translatedRules(t, RULES), [t]);
  const form = useForm({
    defaultValues: { email: typedEmail ?? "" },
    validators: rules,
    onSubmit: async ({ email }) => {
      try {
        await authApi.forgotPassword(email);
      } catch (error) {
        const failure = emailFormFailure(t, error);
        if (failure.fields.email) form.setFieldError("email", failure.fields.email);
        form.setFormError(failure.message);
        return;
      }
      await navigate({ to: "/auth/forgot-password/email-sent" });
    },
  });

  return (
    <form onSubmit={(event) => void form.handleSubmit(event)} className="space-y-6" noValidate>
      <div className="text-center">
        <h1 className="mb-2 text-xl font-semibold text-balance text-slate-800">
          {t("auth.forgot-password.title")}
        </h1>
        <p className="text-sm text-pretty text-slate-600">{t("auth.forgot-password.description")}</p>
      </div>
      {form.formError && <Alert variant="error">{form.formError}</Alert>}
      <Field id="forgot-email" label={t("common.email")} error={form.errors.email}>
        <TextInput
          id="forgot-email"
          type="email"
          name="email"
          autoComplete="email"
          inputMode="email"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          required
          value={form.values.email}
          error={form.errors.email}
          onChange={(event) => form.setValue("email", event.target.value)}
        />
      </Field>
      <div>
        <Button type="submit" className="w-full" loading={form.isSubmitting}>
          {t("auth.forgot-password.reset_password")}
        </Button>
        <div className="mt-3 text-center">
          <ButtonLink to="/auth/login" variant="ghost" className="w-full">
            {t("auth.forgot-password.back_to_login")}
          </ButtonLink>
        </div>
      </div>
    </form>
  );
}
