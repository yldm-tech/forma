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
