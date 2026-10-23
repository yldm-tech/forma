import type { TFunction } from "i18next";
import { toast } from "../../lib/toast";

/** forma2's storage-not-configured toast: the instance has no file storage ("notConfigured"), or the bucket refused or could not be reached ("uploadUnavailable"). */
export const showStorageNotConfiguredToast = (
  t: TFunction,
  variant: "notConfigured" | "uploadUnavailable" = "notConfigured"
) =>
  toast.error(
    variant === "uploadUnavailable"
      ? t("common.file_upload_service_unavailable")
      : t("common.file_storage_not_set_up"),
    { id: "storage-not-configured-toast" }
  );
