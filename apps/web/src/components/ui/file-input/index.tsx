import { IMAGE_FILE_EXTENSIONS } from "@forma/types/storage";
import { useParams } from "@tanstack/react-router";
import { FileIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { toast } from "../../../lib/toast";
import { handleFileUpload, showFileUploadErrorToast } from "../../../lib/upload";
import { LoadingSpinner } from "../loading-spinner";
import { OptionsSwitch } from "../options-switch";
import { Uploader } from "./components/uploader";
import { VideoSettings } from "./components/video-settings";
import { getAllowedFiles } from "./lib/utils";

const allowedFileTypesForPreview = new Set(["png", "jpeg", "jpg", "webp"]);
const isImage = (name: string) => allowedFileTypesForPreview.has(name.split(".").pop() ?? "");

interface FileInputProps {
  id: string;
  onFileUpload: (uploadedUrl: string[] | undefined, fileType: "image" | "video") => void;
  fileUrl?: string | string[];
  videoUrl?: string;
  multiple?: boolean;
  imageFit?: "cover" | "contain";
  isVideoAllowed?: boolean;
  disabled?: boolean;
  /** The extensions offered and accepted; survey images by default. */
  allowedFileExtensions?: readonly string[];
  /** The workspace whose storage receives the files; the route's workspace when not given. */
  workspaceId?: string;
  maxSizeInMB?: number;
}

interface SelectedFile {
  url: string;
  name: string;
  /** False while the file is still uploading. */
  uploaded: boolean;
}

const toSelectedFiles = (fileUrl: string | string[] | undefined): SelectedFile[] => {
  const urls = Array.isArray(fileUrl) ? fileUrl : fileUrl ? [fileUrl] : [];
  return urls.map((url) => ({ url, name: url.split("/").pop() || "", uploaded: true }));
};

/**
 * forma2's FileInput: the image (or images) a field already has, a drop zone that uploads new ones to the workspace's storage, and, where allowed, a video URL tab. Without file storage the drop zone explains that uploads are unavailable; images already set stay visible and removable.
 */
export const FileInput = ({
  id,
  onFileUpload,
  fileUrl,
  videoUrl,
  multiple = false,
  imageFit = "cover",
  isVideoAllowed = false,
  disabled = false,
  allowedFileExtensions = IMAGE_FILE_EXTENSIONS,
  workspaceId,
  maxSizeInMB,
}: FileInputProps) => {
  const { t } = useTranslation();
  const params: { workspaceId?: string } = useParams({ strict: false });
  const targetWorkspaceId = workspaceId ?? params.workspaceId ?? "";
  const options = [
    { value: "image", label: t("common.image") },
    { value: "video", label: t("common.video") },
  ];
  const [selectedFiles, setSelectedFiles] = useState<SelectedFile[]>(() => toSelectedFiles(fileUrl));
  const [uploadedVideoUrl, setUploadedVideoUrl] = useState(videoUrl ?? "");
  const [activeTab, setActiveTab] = useState(isVideoAllowed && videoUrl ? "video" : "image");
  const [imageUrlTemp, setImageUrlTemp] = useState(fileUrl ?? "");
  const [videoUrlTemp, setVideoUrlTemp] = useState(videoUrl ?? "");
  const fileType = isVideoAllowed && activeTab === "video" ? "video" : "image";
  // Callers rebuild the list on every render, so the effect follows its contents rather than its identity.
  const fileUrlKey = JSON.stringify(fileUrl ?? null);

  useEffect(() => {
    setSelectedFiles(toSelectedFiles(fileUrl));
    // fileUrlKey stands for fileUrl.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileUrlKey]);

  useEffect(() => {
    setActiveTab(isVideoAllowed && videoUrl ? "video" : "image");
  }, [isVideoAllowed, videoUrl]);

  /** Uploads the files, as the field's only files or, with append, after the ones it has. */
  const upload = async (files: File[], append: boolean) => {
    if (!append && !multiple && files.length > 1) {
      files = files.slice(0, 1);
      toast.error(t("common.only_one_file_allowed"));
    }
    const allowedFiles = await getAllowedFiles(files, allowedFileExtensions, maxSizeInMB);
    if (allowedFiles.length === 0) return;

    const pending = allowedFiles.map((file) => ({
      url: URL.createObjectURL(file),
      name: file.name,
      uploaded: false,
    }));
    setSelectedFiles((previous) => (append ? [...previous, ...pending] : pending));

    const results = await Promise.all(
      allowedFiles.map((file) => handleFileUpload(file, targetWorkspaceId, [...allowedFileExtensions]))
    );
    const firstError = results.find((result) => result.error)?.error;
    if (firstError) showFileUploadErrorToast(firstError, t);
    const uploadedUrls = results.filter((result) => result.url).map((result) => encodeURI(result.url));

    if (uploadedUrls.length === 0) {
      setSelectedFiles(toSelectedFiles(fileUrl));
      return;
    }
    const previousUrls = Array.isArray(fileUrl) ? fileUrl : fileUrl ? [fileUrl] : [];
    onFileUpload(append ? [...previousUrls, ...uploadedUrls] : uploadedUrls, fileType);
  };

  const handleRemove = (idx: number) => {
    onFileUpload(
      selectedFiles.filter((_, i) => i !== idx).map((file) => file.url),
      fileType
    );
    setImageUrlTemp("");
  };

  // Switching tabs keeps the other tab's value so that switching back restores it, as forma2 did.
  useEffect(() => {
    if (fileType === "image" && typeof imageUrlTemp === "string") {
      setVideoUrlTemp(videoUrl ?? "");
      if (imageUrlTemp) onFileUpload([imageUrlTemp], "image");
    } else if (fileType === "video") {
      setImageUrlTemp(fileUrl ?? "");
      if (videoUrlTemp) onFileUpload([videoUrlTemp], "video");
    }
    // Runs only on a tab change: the url and temp values would re-fire the update and loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileType]);

  const removeButton = (file: SelectedFile, idx: number) =>
    file.uploaded ? (
      <button
        type="button"
        aria-label={t("common.remove")}
        className="absolute top-2 right-2 flex cursor-pointer items-center justify-center rounded-md bg-slate-100 p-1 hover:bg-white/90"
        onClick={(e) => {
          e.preventDefault();
          handleRemove(idx);
        }}>
        <XIcon className="h-5 text-slate-700 hover:text-slate-900" />
      </button>
    ) : (
      <span className="absolute inset-0">
        <LoadingSpinner />
      </span>
    );

  const preview = (file: SelectedFile, idx: number, className: string, fit: "cover" | "contain") =>
    isImage(file.name) || !file.uploaded ? (
      <div className={cn("relative overflow-hidden rounded-lg", className)}>
        <img
          src={file.url}
          alt={file.name}
          className={cn("h-full w-full", !file.uploaded && "opacity-50")}
          style={{ objectFit: fit }}
        />
        {removeButton(file, idx)}
      </div>
    ) : (
      <div
        className={cn(
          "relative flex flex-col items-center justify-center rounded-lg border border-slate-300 px-2 py-3",
          className
        )}>
        <FileIcon className="h-6 text-slate-500" />
        <p className="mt-2 w-full truncate text-center text-sm text-slate-500" title={file.name}>
          <span className="font-semibold">{file.name}</span>
        </p>
        {removeButton(file, idx)}
      </div>
    );

  const [first] = selectedFiles;

  return (
    <div className="w-full cursor-default">
      {isVideoAllowed && (
        <OptionsSwitch options={options} currentOption={activeTab} handleOptionChange={setActiveTab} />
      )}
      {fileType === "video" && (
        <div className={cn(isVideoAllowed && "rounded-b-lg border-x border-b border-slate-200 p-4")}>
          <VideoSettings
            uploadedVideoUrl={uploadedVideoUrl}
            setUploadedVideoUrl={setUploadedVideoUrl}
            onFileUpload={onFileUpload}
            videoUrl={videoUrl ?? ""}
            setVideoUrlTemp={setVideoUrlTemp}
          />
        </div>
      )}
      {fileType === "image" && (
        <div className={cn(isVideoAllowed && "rounded-b-lg border-x border-b border-slate-200 p-4")}>
          {first && multiple ? (
            <div className="flex flex-wrap gap-2">
              {selectedFiles.map((file, idx) => (
                <div key={`${id}-${idx}`}>{preview(file, idx, "h-24 w-40", "cover")}</div>
              ))}
              <Uploader
                id={id}
                name="uploadMore"
                uploaderClassName="h-24 w-40"
                allowedFileExtensions={allowedFileExtensions}
                multiple={multiple}
                onUpload={(files) => void upload(files, true)}
                uploadMore
                disabled={disabled}
              />
            </div>
          ) : first ? (
            <div className="h-52">{preview(first, 0, "mx-auto h-full w-full", imageFit)}</div>
          ) : (
            <Uploader
              id={id}
              name="selected-file"
              uploaderClassName="h-52 w-full"
              allowedFileExtensions={allowedFileExtensions}
              multiple={multiple}
              onUpload={(files) => void upload(files, false)}
              disabled={disabled}
            />
          )}
        </div>
      )}
    </div>
  );
};
