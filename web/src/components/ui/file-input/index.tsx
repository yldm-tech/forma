import { FileIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { OptionsSwitch } from "../options-switch";
import { Uploader } from "./components/uploader";
import { VideoSettings } from "./components/video-settings";

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
}

interface SelectedFile {
  url: string;
  name: string;
}

const toSelectedFiles = (fileUrl: string | string[] | undefined): SelectedFile[] => {
  const urls = Array.isArray(fileUrl) ? fileUrl : fileUrl ? [fileUrl] : [];
  return urls.map((url) => ({ url, name: url.split("/").pop() || "" }));
};

/**
 * forma2's FileInput: the image (or images) a field already has, a drop zone for new ones and, where allowed, a video URL tab. The Go server has no file storage, so the drop zone answers as forma2 did on an instance without storage configured; images already set stay visible and removable.
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
}: FileInputProps) => {
  const { t } = useTranslation();
  const options = [
    { value: "image", label: t("common.image") },
    { value: "video", label: t("common.video") },
  ];
  const [uploadedVideoUrl, setUploadedVideoUrl] = useState(videoUrl ?? "");
  const [activeTab, setActiveTab] = useState(isVideoAllowed && videoUrl ? "video" : "image");
  const [imageUrlTemp, setImageUrlTemp] = useState(fileUrl ?? "");
  const [videoUrlTemp, setVideoUrlTemp] = useState(videoUrl ?? "");
  const fileType = isVideoAllowed && activeTab === "video" ? "video" : "image";
  const selectedFiles = toSelectedFiles(fileUrl);

  useEffect(() => {
    setActiveTab(isVideoAllowed && videoUrl ? "video" : "image");
  }, [isVideoAllowed, videoUrl]);

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

  const removeButton = (idx: number) => (
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
  );

  const preview = (file: SelectedFile, idx: number, className: string, fit: "cover" | "contain") =>
    isImage(file.name) ? (
      <div className={cn("relative overflow-hidden rounded-lg", className)}>
        <img src={file.url} alt={file.name} className="h-full w-full" style={{ objectFit: fit }} />
        {removeButton(idx)}
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
        {removeButton(idx)}
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
              <Uploader uploaderClassName="h-24 w-40" uploadMore disabled={disabled} />
            </div>
          ) : first ? (
            <div className="h-52">{preview(first, 0, "mx-auto h-full w-full", imageFit)}</div>
          ) : (
            <Uploader uploaderClassName="h-52 w-full" disabled={disabled} />
          )}
        </div>
      )}
    </div>
  );
};
