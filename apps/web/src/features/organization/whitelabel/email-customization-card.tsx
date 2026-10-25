import { useQuery } from "@tanstack/react-query";
import { RepeatIcon, Trash2Icon } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { Uploader, useIsStorageConfigured } from "../../../components/ui/file-input/components/uploader";
import { Wordmark } from "../../../components/ui/logo";
import { SettingsCard } from "../../../components/ui/page";
import { showStorageNotConfiguredToast } from "../../../components/ui/storage-not-configured-toast";
import { Muted, Small } from "../../../components/ui/typography";
import { errorMessage } from "../../../lib/errors";
import { authOptionsQuery, meQuery } from "../../../lib/queries/auth";
import type { OrganizationDetail } from "../../../lib/queries/organizations";
import { accessFlags } from "../../../lib/roles";
import { toast } from "../../../lib/toast";
import { handleFileUpload, showFileUploadErrorToast } from "../../../lib/upload";
import { useEmailLogoUrl, useSendTestEmail, useUploadWorkspaceId } from "./api";

const allowedFileExtensions = ["jpeg", "png", "jpg", "webp"];

/**
 * forma2's EmailCustomizationSettings: the logo the organization's emails carry in place of Forma's, uploaded, replaced or removed, previewed beside a sample email, and sent to the caller as a test email where the instance has SMTP. Owners and managers only; the page says once, above every card, that others only look.
 */
export const EmailCustomizationCard = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const isReadOnly = !accessFlags(organization.membership.role).isOwnerOrManager;
  const savedLogoUrl = organization.whitelabel?.logoUrl || null;
  const { data: me } = useQuery(meQuery);
  const { data: options } = useQuery(authOptionsQuery);
  const emailUnavailable = options?.smtpConfigured === false;
  const workspaceId = useUploadWorkspaceId(organization.id);
  const isStorageConfigured = useIsStorageConfigured();
  const setLogo = useEmailLogoUrl(organization.id);
  const sendTest = useSendTestEmail(organization.id);
  const inputRef = useRef<HTMLInputElement>(null);
  const [logoFile, setLogoFile] = useState<File | null>(null);
  // null is Forma's own logo, "" no logo at all (the default removed until the page is reopened).
  const [logoUrl, setLogoUrl] = useState<string | null>(savedLogoUrl);
  const [isSaving, setIsSaving] = useState(false);
  const isDefaultLogo = logoUrl === null;

  const replacePreview = (next: string | null) => {
    if (logoUrl?.startsWith("blob:")) URL.revokeObjectURL(logoUrl);
    setLogoUrl(next);
  };

  const pickFile = (files: File[]) => {
    const [file] = files;
    if (!file) return;
    if (!allowedFileExtensions.includes(file.name.split(".").pop()?.toLowerCase() ?? "")) {
      toast.error(t("common.invalid_file_type"));
      return;
    }
    setLogoFile(file);
    replacePreview(URL.createObjectURL(file));
  };

  const removeLogo = async () => {
    setLogoFile(null);
    replacePreview("");
    if (isDefaultLogo || !savedLogoUrl) return;
    try {
      await setLogo.mutateAsync(null);
      toast.success(t("workspace.settings.general.logo_removed_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const save = async () => {
    if (!logoFile) return;
    setIsSaving(true);
    try {
      const { url, error } = await handleFileUpload(logoFile, workspaceId, allowedFileExtensions);
      if (error) {
        showFileUploadErrorToast(error, t);
        return;
      }
      await setLogo.mutateAsync(url);
      toast.success(t("workspace.settings.general.logo_saved_successfully"));
      setLogoFile(null);
      replacePreview(url);
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsSaving(false);
    }
  };

  const sendTestEmail = async () => {
    if (logoUrl === "") {
      toast.error(t("workspace.settings.general.please_add_a_logo"));
      return;
    }
    if (logoUrl !== savedLogoUrl) {
      toast.error(t("workspace.settings.general.please_save_logo_before_sending_test_email"));
      return;
    }
    try {
      await sendTest.mutateAsync();
      toast.success(t("workspace.settings.general.test_email_sent_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  // Forma's own logo is the lockup the emails carry (logo-transparent.png, the symbol beside the drawn wordmark), sized by its font size: its <svg> has no size of its own, so it needs one where its box only caps the size; an uploaded <img> keeps its intrinsic size.
  const logo = (className: string, wordmarkClassName: string) =>
    logoUrl ? (
      <img src={logoUrl} alt={t("common.logo")} className={className} />
    ) : (
      <Wordmark className={wordmarkClassName} />
    );

  return (
    <SettingsCard
      width="full"
      title={t("workspace.look.email_customization")}
      description={t("workspace.look.email_customization_description")}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="mb-10">
          <Small>{t("workspace.settings.general.logo_in_email_header")}</Small>
          <div className="mt-2 mb-6 flex items-center gap-4">
            {logoUrl !== "" && (
              <div className="flex flex-col gap-2">
                <div className="flex w-max items-center justify-center rounded-lg border border-slate-200 px-4 py-2">
                  {logo("max-h-24 max-w-full object-contain", "text-4xl")}
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    data-testid="replace-logo-button"
                    variant="secondary"
                    onClick={() => {
                      if (!isStorageConfigured) {
                        showStorageNotConfiguredToast(t);
                        return;
                      }
                      inputRef.current?.click();
                    }}
                    disabled={isReadOnly || isSaving}>
                    <RepeatIcon className="size-4" />
                    {t("workspace.settings.general.replace_logo")}
                  </Button>
                  <Button
                    data-testid="remove-logo-button"
                    variant="outline"
                    onClick={() => void removeLogo()}
                    disabled={isReadOnly || isSaving}>
                    <Trash2Icon className="size-4" />
                    {t("workspace.settings.general.remove_logo")}
                  </Button>
                </div>
              </div>
            )}
            <Uploader
              id="email-customization"
              name="email-customization"
              inputRef={inputRef}
              uploaderClassName={logoUrl !== "" ? "hidden" : "h-20 w-96 border border-slate-200 bg-white"}
              allowedFileExtensions={allowedFileExtensions}
              multiple={false}
              onUpload={pickFile}
              disabled={isReadOnly}
            />
          </div>
          <div className="flex gap-4">
            <Button
              data-testid="send-test-email-button"
              variant="secondary"
              aria-describedby={emailUnavailable ? "test-email-unavailable" : undefined}
              disabled={isReadOnly || isSaving || emailUnavailable}
              loading={sendTest.isPending}
              onClick={() => void sendTestEmail()}>
              {t("common.send_test_email")}
            </Button>
            <Button
              onClick={() => void save()}
              disabled={!logoFile || isReadOnly || !workspaceId}
              loading={isSaving}>
              {t("common.save")}
            </Button>
          </div>
          {emailUnavailable && (
            <p id="test-email-unavailable" className="mt-2 max-w-sm text-sm text-slate-600">
              {t("workspace.settings.general.test_email_requires_smtp")}
            </p>
          )}
        </div>
        <div className="min-h-52 w-[446px] max-w-full rounded-t-lg border border-slate-100 px-10 pt-10 pb-4 shadow-xl">
          <div data-testid="email-customization-preview" className="mb-4 flex justify-center">
            {logo("mx-auto max-h-[100px] max-w-full object-contain", "mx-auto text-4xl")}
          </div>
          <p className="font-bold">
            {t("workspace.settings.general.email_customization_preview_email_heading", {
              userName: me?.name,
            })}
          </p>
          <Muted>{t("workspace.settings.general.email_customization_preview_email_text")}</Muted>
        </div>
      </div>
    </SettingsCard>
  );
};
