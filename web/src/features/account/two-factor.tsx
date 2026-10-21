import { CopyIcon } from "lucide-react";
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
import { Input } from "../../components/ui/input";
import { OTPInput } from "../../components/ui/otp-input";
import { PasswordInput } from "../../components/ui/password-input";
import { Switch } from "../../components/ui/switch";
import { copyText } from "../../lib/clipboard";
import { errorMessage } from "../../lib/errors";
import {
  type Me,
  type TwoFactorSetup,
  useDisableTwoFactor,
  useEnableTwoFactor,
  useSetupTwoFactor,
} from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { disableTwoFactorFactor, formatBackupCode } from "../../lib/two-factor";

/** forma2's AccountSecurity card: a switch that opens the enrolment or the removal of two-factor authentication. */
export const AccountSecurity = ({ user }: { user: Me }) => {
  const { t } = useTranslation();
  const [enableOpen, setEnableOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  return (
    <div className="pb-4">
      <div className="flex items-center gap-x-4">
        <Switch
          aria-labelledby="two-factor-title"
          checked={user.twoFactorEnabled}
          onCheckedChange={(checked) => (checked ? setEnableOpen(true) : setDisableOpen(true))}
        />
        <div className="flex flex-col">
          <p id="two-factor-title" className="text-sm font-semibold text-slate-800">
            {t("workspace.settings.profile.two_factor_authentication")}
          </p>
          <p className="text-xs text-slate-600">
            {t("workspace.settings.profile.two_factor_authentication_description")}
          </p>
        </div>
      </div>
      {enableOpen && <EnableTwoFactorModal onClose={() => setEnableOpen(false)} />}
      {disableOpen && <DisableTwoFactorModal onClose={() => setDisableOpen(false)} />}
    </div>
  );
};

type EnableStep = "confirmPassword" | "scanQRCode" | "enterCode" | "backupCodes";

/** forma2's EnableTwoFactorModal: confirm the password, scan the secret, prove it with a code, then keep the backup codes. */
const EnableTwoFactorModal = ({ onClose }: { onClose: () => void }) => {
  const { t } = useTranslation();
  const [step, setStep] = useState<EnableStep>("confirmPassword");
  const [setup, setSetup] = useState<TwoFactorSetup | null>(null);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <DialogTitle>{t("workspace.settings.profile.two_factor_authentication")}</DialogTitle>
        <DialogDescription>
          {t("workspace.settings.profile.confirm_your_current_password_to_get_started")}
        </DialogDescription>
      </DialogHeader>
      <DialogBody>
        {step === "confirmPassword" && (
          <ConfirmPasswordStep
            onCancel={onClose}
            onDone={(result) => {
              setSetup(result);
              setStep("scanQRCode");
            }}
          />
        )}
        {step === "scanQRCode" && setup && (
          <ScanQRCodeStep setup={setup} onCancel={onClose} onNext={() => setStep("enterCode")} />
        )}
        {step === "enterCode" && <EnterCodeStep onCancel={onClose} onDone={() => setStep("backupCodes")} />}
        {step === "backupCodes" && setup && <BackupCodesStep codes={setup.backupCodes} onClose={onClose} />}
      </DialogBody>
    </Dialog>
  );
};

/**
 * Starts the enrolment with the current password. Unlike forma2's form, which held the password to today's rules and so locked out users with an older, shorter password, any non-empty password is sent; the server checks it.
 */
const ConfirmPasswordStep = ({
  onCancel,
  onDone,
}: {
  onCancel: () => void;
  onDone: (setup: TwoFactorSetup) => void;
}) => {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const setup = useSetupTwoFactor();
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      onDone(await setup.mutateAsync(password));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };
  return (
    <form className="flex flex-col gap-y-4" onSubmit={submit}>
      <div className="flex flex-col gap-2">
        <label htmlFor="two-factor-password" className="text-sm font-medium text-slate-700">
          {t("common.password")}
        </label>
        <PasswordInput
          id="two-factor-password"
          autoComplete="current-password"
          placeholder="*******"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
      <div className="flex w-full items-center justify-end gap-x-2">
        <Button variant="secondary" size="sm" type="button" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" type="submit" loading={setup.isPending} disabled={password === ""}>
          {t("common.confirm")}
        </Button>
      </div>
    </form>
  );
};

const ScanQRCodeStep = ({
  setup,
  onCancel,
  onNext,
}: {
  setup: TwoFactorSetup;
  onCancel: () => void;
  onNext: () => void;
}) => {
  const { t } = useTranslation();
  return (
    <div>
      <div className="p-6">
        <h3 className="text-lg font-semibold">
          {t("workspace.settings.profile.enable_two_factor_authentication")}
        </h3>
        <p className="text-sm text-slate-700">
          {t("workspace.settings.profile.scan_the_qr_code_below_with_your_authenticator_app")}
        </p>
      </div>
      <div className="mb-4 flex flex-col items-center justify-center gap-y-4">
        <img src={setup.dataUri} alt="QR code" width={200} height={200} />
        <p className="text-sm text-slate-700">
          {t("workspace.settings.profile.or_enter_the_following_code_manually")}
        </p>
        <div className="flex items-center gap-x-2">
          <p className="font-mono text-sm font-medium text-slate-700">{setup.secret}</p>
          <Button
            variant="secondary"
            size="sm"
            type="button"
            aria-label={t("common.copy")}
            onClick={() => void copyText(setup.secret, t)}>
            <CopyIcon className="size-4" />
          </Button>
        </div>
      </div>
      <div className="flex w-full items-center justify-end gap-x-4">
        <Button variant="secondary" size="sm" type="button" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" type="button" onClick={onNext}>
          {t("common.next")}
        </Button>
      </div>
    </div>
  );
};

const EnterCodeStep = ({ onCancel, onDone }: { onCancel: () => void; onDone: () => void }) => {
  const { t } = useTranslation();
  const [code, setCode] = useState("");
  const enable = useEnableTwoFactor();
  const complete = /^\d{6}$/.test(code);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!complete) return;
    try {
      const result = await enable.mutateAsync(code);
      toast.success(result.message);
      onDone();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };
  return (
    <div>
      <div className="p-6">
        <h3 className="text-lg font-semibold">
          {t("workspace.settings.profile.enable_two_factor_authentication")}
        </h3>
        <p className="text-sm text-slate-700">
          {t("workspace.settings.profile.enter_the_code_from_your_authenticator_app_below")}
        </p>
      </div>
      <form className="flex flex-col gap-y-10" onSubmit={submit}>
        <fieldset className="flex flex-col gap-2 px-6">
          <legend className="text-sm font-medium text-slate-700">{t("common.code")}</legend>
          <OTPInput value={code} onChange={setCode} valueLength={6} className="justify-start" />
        </fieldset>
        <div className="flex w-full items-center justify-end gap-x-4">
          <Button variant="secondary" size="sm" type="button" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button size="sm" type="submit" loading={enable.isPending} disabled={!complete}>
            {t("common.confirm")}
          </Button>
        </div>
      </form>
    </div>
  );
};

const BackupCodesStep = ({ codes, onClose }: { codes: string[]; onClose: () => void }) => {
  const { t } = useTranslation();
  const formatted = codes.map(formatBackupCode).join("\n");
  const download = () => {
    const url = URL.createObjectURL(new Blob([formatted], { type: "text/plain" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "forma-backup-codes.txt";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
  };
  return (
    <div>
      <div className="p-6">
        <h3 className="text-lg font-semibold">
          {t("workspace.settings.profile.enable_two_factor_authentication")}
        </h3>
        <p className="text-sm text-slate-700">
          {t("workspace.settings.profile.save_the_following_backup_codes_in_a_safe_place")}
        </p>
      </div>
      <ul className="mx-auto mb-6 grid max-w-[60%] grid-cols-2 gap-1 text-center font-mono">
        {codes.map((code) => (
          <li key={code} className="text-sm font-medium text-slate-700">
            {formatBackupCode(code)}
          </li>
        ))}
      </ul>
      <div className="flex w-full items-center justify-end gap-x-4 border-t border-slate-300 p-4">
        <Button variant="secondary" type="button" size="sm" onClick={onClose}>
          {t("common.close")}
        </Button>
        <Button size="sm" type="button" onClick={() => void copyText(formatted, t)}>
          {t("common.copy")}
        </Button>
        <Button size="sm" type="button" onClick={download}>
          {t("common.download")}
        </Button>
      </div>
    </div>
  );
};

/** forma2's DisableTwoFactorModal: the password and exactly one of an authenticator code or a backup code. */
const DisableTwoFactorModal = ({ onClose }: { onClose: () => void }) => {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [backupCode, setBackupCode] = useState("");
  const [useBackupCode, setUseBackupCode] = useState(false);
  const [factorError, setFactorError] = useState<string | null>(null);
  const disable = useDisableTwoFactor();

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const factor = disableTwoFactorFactor(useBackupCode ? "" : code, useBackupCode ? backupCode : "");
    if (!factor) {
      setFactorError("Please provide either the code OR the backup code");
      return;
    }
    setFactorError(null);
    try {
      const result = await disable.mutateAsync({ password, ...factor });
      toast.success(result.message);
      onClose();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <DialogTitle>{t("workspace.settings.profile.disable_two_factor_authentication")}</DialogTitle>
        <DialogDescription>
          {t("workspace.settings.profile.disable_two_factor_authentication_description")}
        </DialogDescription>
      </DialogHeader>
      <form className="flex flex-col gap-y-6" onSubmit={submit} noValidate>
        <DialogBody className="space-y-6">
          <div className="flex flex-col gap-2">
            <label htmlFor="disable-two-factor-password" className="text-sm font-medium text-slate-700">
              {t("common.password")}
            </label>
            <PasswordInput
              id="disable-two-factor-password"
              autoComplete="current-password"
              placeholder="*******"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <label htmlFor="disable-two-factor-backup" className="text-sm font-medium text-slate-700">
              {useBackupCode
                ? t("workspace.settings.profile.backup_code")
                : t("workspace.settings.profile.two_factor_code")}
            </label>
            <p className="text-sm text-slate-700">
              {useBackupCode
                ? t(
                    "workspace.settings.profile.each_backup_code_can_be_used_exactly_once_to_grant_access_without_your_authenticator"
                  )
                : t(
                    "workspace.settings.profile.two_factor_authentication_enabled_please_enter_the_six_digit_code_from_your_authenticator_app"
                  )}
            </p>
            {useBackupCode ? (
              <Input
                id="disable-two-factor-backup"
                placeholder="XXXXX-XXXXX"
                value={backupCode}
                autoComplete="off"
                onChange={(e) => setBackupCode(e.target.value)}
              />
            ) : (
              <OTPInput value={code} valueLength={6} onChange={setCode} className="mt-2 justify-start" />
            )}
            <FormError>{factorError}</FormError>
          </div>
        </DialogBody>
        <DialogFooter className="flex w-full items-center sm:justify-between">
          <Button
            variant="ghost"
            type="button"
            onClick={() => {
              setUseBackupCode(!useBackupCode);
              setFactorError(null);
            }}>
            {useBackupCode ? t("common.go_back") : t("workspace.settings.profile.lost_access")}
          </Button>
          <div className="flex items-center gap-x-4">
            <Button variant="secondary" type="button" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" loading={disable.isPending} disabled={password === ""}>
              {t("common.disable")}
            </Button>
          </div>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
