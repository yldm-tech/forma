import { useQuery } from "@tanstack/react-query";
import { ArrowUpFromLineIcon } from "lucide-react";
import type { DragEvent, Ref } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../../lib/cn";
import { instanceQuery } from "../../../../lib/queries/instance";
import { showStorageNotConfiguredToast } from "../../storage-not-configured-toast";

/** Whether the instance can store uploads; assumed until the server says otherwise, as forma2's isStorageConfigured defaulted to true. */
export const useIsStorageConfigured = (): boolean => {
  const { data } = useQuery(instanceQuery);
  return data?.storageConfigured ?? true;
};

interface UploaderProps {
  id: string;
  name: string;
  uploaderClassName: string;
  allowedFileExtensions: readonly string[];
  multiple: boolean;
  /** Receives the files picked or dropped; never called while disabled or without file storage. */
  onUpload: (files: File[]) => void;
  uploadMore?: boolean;
  disabled?: boolean;
  /** The hidden file input, for callers with a "replace" button of their own. */
  inputRef?: Ref<HTMLInputElement>;
}

/** forma2's upload drop zone: a click opens the file picker and a drop takes the dropped files. Without file storage there is nothing to pick into, so the zone is a button that explains that uploads are unavailable, on a click or a drop alike. */
export const Uploader = ({
  id,
  name,
  uploaderClassName,
  allowedFileExtensions,
  multiple,
  onUpload,
  uploadMore = false,
  disabled = false,
  inputRef,
}: UploaderProps) => {
  const { t } = useTranslation();
  const isStorageConfigured = useIsStorageConfigured();
  const inputId = `${id}-${name}`;
  const className = cn(
    "relative flex cursor-pointer flex-col items-center justify-center rounded-lg border-2 border-dashed border-slate-300 bg-slate-50",
    uploaderClassName,
    disabled ? "cursor-not-allowed opacity-50" : "hover:bg-slate-100"
  );
  const label = (
    <span className="flex flex-col items-center justify-center pt-5 pb-6">
      <ArrowUpFromLineIcon className="h-6 text-slate-500" />
      <span className={cn("mt-2 text-center text-sm text-slate-500", uploadMore && "text-xs")}>
        <span className="font-semibold">{t("common.upload_input_description")}</span>
      </span>
    </span>
  );
  const onDragOver = (e: DragEvent<HTMLElement>) => {
    e.preventDefault();
    e.stopPropagation();
    if (isStorageConfigured && !disabled) e.dataTransfer.dropEffect = "copy";
  };

  if (!isStorageConfigured) {
    const refuse = (e: { preventDefault: () => void; stopPropagation: () => void }) => {
      e.preventDefault();
      e.stopPropagation();
      if (!disabled) showStorageNotConfiguredToast(t);
    };
    return (
      <button
        type="button"
        data-testid="upload-file-label"
        disabled={disabled}
        className={className}
        onClick={refuse}
        onDragOver={onDragOver}
        onDrop={refuse}>
        {label}
      </button>
    );
  }

  return (
    <label
      htmlFor={inputId}
      data-testid="upload-file-label"
      className={className}
      aria-disabled={disabled}
      onDragOver={onDragOver}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (!disabled) onUpload(Array.from(e.dataTransfer.files));
      }}>
      {label}
      <input
        data-testid="upload-file-input"
        type="file"
        id={inputId}
        name={inputId}
        accept={allowedFileExtensions.map((ext) => `.${ext}`).join(",")}
        className="hidden"
        multiple={multiple}
        disabled={disabled}
        ref={inputRef}
        onChange={(e) => {
          const files = Array.from(e.target.files ?? []);
          // Cleared so that picking the same file again is a change too.
          e.target.value = "";
          if (files.length > 0) onUpload(files);
        }}
      />
    </label>
  );
};
