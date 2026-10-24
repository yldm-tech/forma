import { useQuery, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { FormError } from "../../components/ui/form-error";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { PasswordInput } from "../../components/ui/password-input";
import { Select } from "../../components/ui/select";
import { errorMessage } from "../../lib/errors";
import { changeLocale, isLocale } from "../../lib/i18n";
import { APP_LANGUAGES, languageLabel, sortedAppLanguages } from "../../lib/languages";
import { profileChanges } from "../../lib/profile-form";
import {
  type Me,
  authOptionsQuery,
  meQuery,
  signOut,
  useRequestEmailChange,
  useRequestPasswordResetLink,
  useUpdateProfile,
} from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { emailError, userNameError } from "../../lib/validation";
import { AuthorizedAppsCard } from "./authorized-apps";
import { DeleteAccount } from "./delete-account";
import { AccountSecurity } from "./two-factor";

/** forma2's profile page: personal information, two-factor authentication for password users, the connected MCP clients, and deleting the account. */
export const ProfilePage = () => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.profile")} />
      <SettingsCard
        width="full"
        title={t("workspace.settings.profile.personal_information")}
        description={t("workspace.settings.profile.update_personal_info")}>
        <EditProfileDetailsForm key={`${me.name}|${me.email}|${me.locale}`} user={me} />
      </SettingsCard>
      {me.identityProvider === "email" && (
        <SettingsCard
          width="full"
          title={t("common.security")}
          description={t("workspace.settings.profile.security_description")}>
          <AccountSecurity user={me} />
        </SettingsCard>
      )}
      <AuthorizedAppsCard locale={me.locale} />
      <SettingsCard
        width="full"
        title={t("workspace.settings.profile.delete_account")}
        description={t("workspace.settings.profile.confirm_delete_account")}>
        <DeleteAccount user={me} />
      </SettingsCard>
      <IdBadge id={me.id} label={t("common.profile_id")} />
    </PageContentWrapper>
  );
};

/**
 * forma2's EditProfileDetailsForm. An identity provider owns an SSO user's name and address, so those fields are read-only for them. A new address needs the current password and, where the instance verifies addresses, confirmation from the new mailbox; without verification it applies at once and the user signs in again.
 */
const EditProfileDetailsForm = ({ user }: { user: Me }) => {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const options = useQuery(authOptionsQuery);
  const [name, setName] = useState(user.name);
  const [email, setEmail] = useState(user.email);
  const [locale, setLocale] = useState(user.locale);
  const [errors, setErrors] = useState<{ name?: string; email?: string }>({});
  const [confirmOpen, setConfirmOpen] = useState(false);
  const update = useUpdateProfile();
  const changeEmail = useRequestEmailChange();
  const resetLink = useRequestPasswordResetLink();
  const isPasswordUser = user.identityProvider === "email";
  const changes = profileChanges(user, { name, email, locale });
  const languages = APP_LANGUAGES.some((language) => language.code === user.locale)
    ? sortedAppLanguages
    : [...sortedAppLanguages, { code: user.locale, english: user.locale, native: user.locale }];

  const saveNameAndLocale = async () => {
    if (!changes.profile) return;
    await update.mutateAsync(changes.profile);
    if (changes.profile.locale && isLocale(changes.profile.locale))
      await changeLocale(i18n, changes.profile.locale);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const found = {
      ...(isPasswordUser && userNameError(name) ? { name: userNameError(name) ?? undefined } : {}),
      ...(isPasswordUser && emailError(email) ? { email: emailError(email) ?? undefined } : {}),
    };
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    if (changes.email) {
      setConfirmOpen(true);
      return;
    }
    try {
      await saveNameAndLocale();
      toast.success(t("workspace.settings.profile.profile_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const confirmEmailChange = async (password: string) => {
    if (!changes.email) return;
    // The address goes first: a wrong password then changes nothing, as forma2's single action did.
    await changeEmail.mutateAsync({ email: changes.email, password });
    setConfirmOpen(false);
    await saveNameAndLocale();
    if (options.data?.emailVerificationRequired ?? true) {
      toast.success(t("auth.verification-requested.new_email_verification_success"));
      setEmail(user.email);
      return;
    }
    toast.success(t("workspace.settings.profile.email_change_initiated"));
    await signOut(queryClient, { reason: "email_change" });
    await navigate({ href: "/email-change-without-verification-success" });
  };

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
    <>
      <form className="w-full max-w-sm pb-4" onSubmit={submit} noValidate>
        <div className="space-y-2">
          <Label htmlFor="profile-name">{t("common.full_name")}</Label>
          <Input
            id="profile-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            placeholder={t("common.full_name")}
            isInvalid={!!errors.name}
            aria-describedby={errors.name ? "profile-name-error" : undefined}
            disabled={!isPasswordUser}
          />
          <FormError id="profile-name-error">{errors.name}</FormError>
        </div>
        <div className="mt-4 space-y-2">
          <Label htmlFor="profile-email">{t("common.email")}</Label>
          <Input
            id="profile-email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            isInvalid={!!errors.email}
            aria-describedby={errors.email ? "profile-email-error" : undefined}
            disabled={!isPasswordUser}
          />
          <FormError id="profile-email-error">{errors.email}</FormError>
        </div>
        <div className="mt-4 space-y-2">
          <Label htmlFor="profile-language">{t("common.language")}</Label>
          <Select id="profile-language" value={locale} onChange={(e) => setLocale(e.target.value)}>
            {languages.map((language) => (
              <option key={language.code} value={language.code}>
                {languageLabel(language)}
              </option>
            ))}
          </Select>
        </div>
        {/* The reset link is mailed, so without SMTP the button could only fail. */}
        {options.data?.passwordResetEnabled && options.data.smtpConfigured && isPasswordUser && (
          <div className="mt-4 space-y-2">
            <Label htmlFor="reset-password">{t("auth.forgot-password.reset_password")}</Label>
            <p className="mt-1 text-sm text-slate-500">
              {t("auth.forgot-password.reset_password_description")}
            </p>
            <div className="flex items-center justify-between gap-2">
              <Input type="email" id="reset-password" value={user.email} disabled readOnly />
              <Button
                type="button"
                onClick={() => void resetPassword()}
                loading={resetLink.isPending}
                variant="secondary">
                {t("auth.forgot-password.reset_password")}
              </Button>
            </div>
          </div>
        )}
        <Button
          type="submit"
          className="mt-4"
          size="sm"
          loading={update.isPending && !confirmOpen}
          disabled={!changes.email && !changes.profile}>
          {t("common.update")}
        </Button>
      </form>
      <PasswordConfirmationModal
        open={confirmOpen}
        setOpen={setConfirmOpen}
        oldEmail={user.email}
        newEmail={changes.email ?? user.email}
        onConfirm={confirmEmailChange}
      />
    </>
  );
};

const PasswordConfirmationModal = ({
  open,
  setOpen,
  oldEmail,
  newEmail,
  onConfirm,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  oldEmail: string;
  newEmail: string;
  onConfirm: (password: string) => Promise<void>;
}) => {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const close = () => {
    setPassword("");
    setError(null);
    setOpen(false);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setPending(true);
    try {
      await onConfirm(password);
      setPassword("");
      setError(null);
    } catch (err) {
      setError(errorMessage(t, err));
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
      <DialogHeader>
        <DialogTitle>{t("auth.forgot-password.reset.confirm_password")}</DialogTitle>
        <DialogDescription>{t("auth.email-change.confirm_password_description")}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="space-y-4">
        <DialogBody className="space-y-4">
          <div className="flex flex-col gap-2 text-sm sm:flex-row sm:justify-between sm:gap-4">
            <p>
              <strong>{t("auth.email-change.old_email")}:</strong>
              <br /> {oldEmail.toLowerCase()}
            </p>
            <p>
              <strong>{t("auth.email-change.new_email")}:</strong>
              <br /> {newEmail.toLowerCase()}
            </p>
          </div>
          <PasswordInput
            id="email-change-password"
            autoComplete="current-password"
            placeholder="*******"
            aria-label={t("common.password")}
            aria-describedby={error ? "email-change-password-error" : undefined}
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <FormError id="email-change-password-error">{error}</FormError>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={pending} disabled={pending || password === ""}>
            {t("common.confirm")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
