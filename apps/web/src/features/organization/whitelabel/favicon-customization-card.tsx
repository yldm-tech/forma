import { type ChangeEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useUnsavedChanges } from "../../../components/shell/unsaved-changes";
import { Alert, AlertDescription } from "../../../components/ui/alert";
import { Button } from "../../../components/ui/button";
import { FileInput } from "../../../components/ui/file-input";
import { useIsStorageConfigured } from "../../../components/ui/file-input/components/uploader";
import { SettingsCard } from "../../../components/ui/page";
import { showStorageNotConfiguredToast } from "../../../components/ui/storage-not-configured-toast";
import { errorMessage } from "../../../lib/errors";
import type { OrganizationDetail } from "../../../lib/queries/organizations";
import { accessFlags } from "../../../lib/roles";
import { toast } from "../../../lib/toast";
import { handleFileUpload, showFileUploadErrorToast } from "../../../lib/upload";
import { useFaviconUrl, useUploadWorkspaceId } from "./api";

// PNG and ICO are the most widely supported favicon formats.
const allowedFileExtensions = ["png", "jpeg", "jpg", "ico", "webp"];

/** forma2's MAX_FAVICON_SIZE_MB: 100 KB, plenty for a 512×512 favicon. */
const MAX_FAVICON_SIZE_MB = 0.1;

/**
 * forma2's FaviconCustomizationSettings: the favicon the organization's link surveys show in place of Forma's, uploaded, replaced, saved and removed. Owners and managers only.
 */
export const FaviconCustomizationCard = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const isReadOnly = !accessFlags(organization.membership.role).isOwnerOrManager;
  const workspaceId = useUploadWorkspaceId(organization.id);
  const isStorageConfigured = useIsStorageConfigured();
  const setFavicon = useFaviconUrl(organization.id);
  const [faviconUrl, setFaviconUrl] = useState(organization.whitelabel?.faviconUrl || undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // An uploaded file is only the organization's favicon once saved.
  useUnsavedChanges(isEditing && faviconUrl !== (organization.whitelabel?.faviconUrl || undefined));

  const handleFileChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!isStorageConfigured) {
      showStorageNotConfiguredToast(t);
      return;
    }
    if (!file) return;
    if (file.size / 1000000 > MAX_FAVICON_SIZE_MB) {
      toast.error(t("workspace.settings.domain.favicon_too_large"));
      return;
    }
    setIsLoading(true);
    const result = await handleFileUpload(file, workspaceId, allowedFileExtensions);
    setIsLoading(false);
    if (result.error) showFileUploadErrorToast(result.error, t);
    else setFaviconUrl(result.url);
    setIsEditing(true);
  };

  const saveChanges = async () => {
    if (!isEditing) {
      setIsEditing(true);
      return;
    }
    if (!faviconUrl) return;
    setIsLoading(true);
    try {
      await setFavicon.mutateAsync(faviconUrl);
      toast.success(t("workspace.settings.domain.favicon_saved_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsLoading(false);
      setIsEditing(false);
    }
  };

  const removeFavicon = async () => {
    setFaviconUrl(undefined);
    if (!organization.whitelabel?.faviconUrl) {
      setIsEditing(false);
      return;
    }
    setIsLoading(true);
    try {
      await setFavicon.mutateAsync(null);
      toast.success(t("workspace.settings.domain.favicon_removed_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsLoading(false);
      setIsEditing(false);
    }
  };

  return (
    <SettingsCard
      width="full"
      title={t("workspace.settings.domain.favicon_customization")}
      description={t("workspace.settings.domain.favicon_customization_description")}>
      <div className="w-full space-y-4 pb-4">
        {faviconUrl ? (
          <img
            src={faviconUrl}
            alt="Favicon"
            width={64}
            height={64}
            className="-mb-2 size-16 rounded-lg border object-contain p-1"
          />
        ) : (
          <FileInput
            id="favicon-input"
            allowedFileExtensions={allowedFileExtensions}
            workspaceId={workspaceId}
            onFileUpload={(files) => {
              if (files?.[0]) {
                setFaviconUrl(files[0]);
                setIsEditing(true);
              }
            }}
            disabled={isReadOnly || !workspaceId}
            maxSizeInMB={MAX_FAVICON_SIZE_MB}
          />
        )}
        <input
          ref={fileInputRef}
          type="file"
          accept="image/jpeg, image/png, image/webp, image/x-icon, image/ico"
          className="hidden"
          disabled={isReadOnly || !workspaceId}
          data-testid="favicon-replace-input"
          onChange={(e) => void handleFileChange(e)}
        />
        {isEditing && faviconUrl && (
          <div className="flex gap-2">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                if (!isStorageConfigured) {
                  showStorageNotConfiguredToast(t);
                  return;
                }
                fileInputRef.current?.click();
              }}>
              {t("common.replace")}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void removeFavicon()}>
              {t("common.remove")}
            </Button>
          </div>
        )}
        {faviconUrl && (
          <Button onClick={() => void saveChanges()} disabled={isLoading || isReadOnly} size="sm">
            {isEditing ? t("common.save") : t("common.edit")}
          </Button>
        )}
        <Alert variant="info" role="status">
          <AlertDescription>{t("workspace.settings.domain.favicon_size_hint")}</AlertDescription>
        </Alert>
        {isReadOnly && (
          <Alert variant="warning" role="status">
            <AlertDescription>
              {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
            </AlertDescription>
          </Alert>
        )}
      </div>
    </SettingsCard>
  );
};
