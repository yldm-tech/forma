import { ArrowUpFromLineIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../../lib/cn";
import { showStorageNotConfiguredToast } from "../../storage-not-configured-toast";

interface UploaderProps {
  uploaderClassName: string;
  uploadMore?: boolean;
  disabled?: boolean;
}

/** forma2's upload drop zone on an instance without file storage: a click or a drop explains that uploads are unavailable. */
export const Uploader = ({ uploaderClassName, uploadMore = false, disabled = false }: UploaderProps) => {
  const { t } = useTranslation();
  const refuse = () => {
    if (!disabled) showStorageNotConfiguredToast(t);
  };
  return (
    <button
      type="button"
      data-testid="upload-file-label"
      disabled={disabled}
      className={cn(
        "relative flex cursor-pointer flex-col items-center justify-center rounded-lg border-2 border-dashed border-slate-300 bg-slate-50",
        uploaderClassName,
        disabled ? "cursor-not-allowed opacity-50" : "hover:bg-slate-100"
      )}
      onClick={refuse}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        refuse();
      }}>
      <span className="flex flex-col items-center justify-center pt-5 pb-6">
        <ArrowUpFromLineIcon className="h-6 text-slate-500" />
        <span className={cn("mt-2 text-center text-sm text-slate-500", uploadMore && "text-xs")}>
          <span className="font-semibold">{t("common.upload_input_description")}</span>
        </span>
      </span>
    </button>
  );
};
