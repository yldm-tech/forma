/**
 * Human-readable alt text derived from a storage URL: the decoded file name
 * without extension or separator noise, so screen readers don't spell out
 * strings like "ChatGPT%20Image%20Jun%205.png". Returns "" when nothing
 * readable is left (callers should fall back to a localized label).
 */
export const getImageAltFromUrl = (fileURL: string): string => {
  let name = getOriginalFileNameFromUrl(fileURL);

  // Stored URLs are sometimes double-encoded; decode until stable.
  for (let i = 0; i < 3; i++) {
    try {
      const decoded = decodeURIComponent(name);
      if (decoded === name) break;
      name = decoded;
    } catch {
      break;
    }
  }

  return name
    .replace(/\.[a-z0-9]+$/i, "")
    .replace(/[-_+]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
};

export const getOriginalFileNameFromUrl = (fileURL: string): string => {
  try {
    const fileNameFromURL = fileURL.startsWith("/storage/")
      ? fileURL.split("/").pop()
      : new URL(fileURL).pathname.split("/").pop();

    const fileExt = fileNameFromURL?.split(".").pop() ?? "";
    const originalFileName = fileNameFromURL?.split("--fid--")[0] ?? "";
    const fileId = fileNameFromURL?.split("--fid--")[1] ?? "";

    if (!fileId) {
      return originalFileName ? decodeURIComponent(originalFileName) : "";
    }

    return originalFileName ? decodeURIComponent(`${originalFileName}.${fileExt}`) : "";
  } catch (error) {
    console.error(`Error parsing file URL: ${String(error)}`);
    return "";
  }
};

/**
 * What an image's `alt` should be, given the alt the caller supplied and the image's own URL.
 *
 * An undescribed image must be exposed as decorative (`alt=""`), never as a placeholder word: a
 * screen reader skips `alt=""` but reads `alt="Image"` aloud for every image in the survey, which is
 * strictly worse than silence because the headline beside it already carries the meaning. The stored
 * file name is used in between — it is authored content often enough to beat nothing — and an author
 * who supplies an explicit alt (including an explicit empty one) always wins.
 */
export const resolveImageAltText = (altText: string | undefined, imgUrl: string | undefined): string => {
  if (altText !== undefined) return altText;
  return getImageAltFromUrl(imgUrl ?? "");
};
