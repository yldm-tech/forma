import type { TFunction } from "i18next";
import { toast } from "../../lib/toast";

/** forma2's storage-not-configured toast: the instance has no file storage, so nothing can be uploaded. */
export const showStorageNotConfiguredToast = (t: TFunction) =>
  toast.error(t("common.file_storage_not_set_up"), { id: "storage-not-configured-toast" });
