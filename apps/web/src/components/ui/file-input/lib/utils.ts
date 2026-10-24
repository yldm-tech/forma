import { toast } from "../../../../lib/toast";

/** forma2's HEIC conversion (convertHeicToJpegAction), done in the browser: heic-convert's browser entry decodes the image and the browser's own encoder writes a JPEG at quality 0.9. The converter is loaded only when a HEIC file is picked. Returns null when the file cannot be converted. */
export const convertHeicToJpeg = async (file: File): Promise<File | null> => {
  try {
    const { default: convert } = await import("heic-convert/browser");
    const jpeg = await convert({
      buffer: new Uint8Array(await file.arrayBuffer()),
      format: "JPEG",
      quality: 0.9,
    });
    return new File([new Uint8Array(jpeg)], file.name.replace(/\.heic$/i, ".jpg"), { type: "image/jpeg" });
  } catch {
    return null;
  }
};

const isFileSizeExceeded = (fileSizeInMB: number, maxSizeInMB?: number) =>
  Boolean(maxSizeInMB && fileSizeInMB > maxSizeInMB);

/**
 * forma2's getAllowedFiles: the files with an allowed extension and within the size limit, HEIC images converted to JPEG and their size checked again. One toast names what was left out.
 */
export const getAllowedFiles = async (
  files: File[],
  allowedFileExtensions: readonly string[],
  maxSizeInMB?: number
): Promise<File[]> => {
  const sizeExceedFiles: string[] = [];
  const unsupportedExtensionFiles: string[] = [];
  const allowedFiles: File[] = [];

  for (const file of files) {
    const extension = file.name.split(".").pop()?.toLowerCase();
    if (!extension || !allowedFileExtensions.includes(extension)) {
      unsupportedExtensionFiles.push(file.name);
      continue;
    }
    if (isFileSizeExceeded(file.size / 1000000, maxSizeInMB)) {
      sizeExceedFiles.push(file.name);
      continue;
    }
    if (extension === "heic") {
      const converted = await convertHeicToJpeg(file);
      if (!converted) {
        unsupportedExtensionFiles.push(file.name);
        continue;
      }
      if (isFileSizeExceeded(converted.size / 1000000, maxSizeInMB)) {
        sizeExceedFiles.push(file.name);
        continue;
      }
      allowedFiles.push(converted);
      continue;
    }
    allowedFiles.push(file);
  }

  // forma2's messages, which it never translated.
  let message = "";
  if (sizeExceedFiles.length === 1) message += `File exceeds ${maxSizeInMB} MB size limit.`;
  else if (sizeExceedFiles.length > 1) {
    message += `${sizeExceedFiles.length} files exceed ${maxSizeInMB} MB size limit.`;
  }
  if (unsupportedExtensionFiles.length > 0) {
    if (message) message += " ";
    message +=
      unsupportedExtensionFiles.length === 1
        ? "Unsupported file type."
        : `${unsupportedExtensionFiles.length} files have unsupported types.`;
  }
  if (message) toast.error(message);
  return allowedFiles;
};

/** Whether a YouTube embed URL already uses the privacy-enhanced (no-cookie) host. */
export const checkForYoutubePrivacyMode = (url: string): boolean => {
  if (!url || typeof url !== "string" || url.trim() === "") {
    return false;
  }
  try {
    return new URL(url).host === "www.youtube-nocookie.com";
  } catch {
    return false;
  }
};
