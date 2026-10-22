import type { TFunction } from "i18next";
import { toast } from "./toast";

/** Copies text and says so; the Clipboard API refuses in an insecure context or without permission, which the user is told. */
export const copyText = async (text: string, t: TFunction, successMessage?: string): Promise<boolean> => {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(successMessage ?? t("common.copied_to_clipboard"));
    return true;
  } catch {
    toast.error(t("common.something_went_wrong_please_try_again"));
    return false;
  }
};
