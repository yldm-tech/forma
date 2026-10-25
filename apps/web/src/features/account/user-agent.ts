// The sessions card names a session by the browser and operating system its User-Agent header reports. Only the names are read, not versions or devices, so a short ordered list of tokens is enough; a header that matches none is shown as an unknown device, with the raw header in a tooltip.

export interface DeviceDescription {
  browser: string | null;
  os: string | null;
}

// Order matters: browsers built on Chromium also send "Chrome" and "Safari", and Chrome on iOS sends "Safari", so the more specific tokens come first.
const BROWSERS: [RegExp, string][] = [
  [/\bEdg(?:e|A|iOS)?\//, "Edge"],
  [/\b(?:OPR|Opera)\//, "Opera"],
  [/\bSamsungBrowser\//, "Samsung Internet"],
  [/\bVivaldi\//, "Vivaldi"],
  [/\bYaBrowser\//, "Yandex Browser"],
  [/\b(?:Firefox|FxiOS)\//, "Firefox"],
  [/\b(?:HeadlessChrome|Chrome|CriOS|Chromium)\//, "Chrome"],
  [/\bSafari\//, "Safari"],
];

const OPERATING_SYSTEMS: [RegExp, string][] = [
  [/\b(?:iPhone|iPad|iPod)\b/, "iOS"],
  [/\bAndroid\b/, "Android"],
  [/\bCrOS\b/, "ChromeOS"],
  [/\bWindows\b/, "Windows"],
  [/\bMac OS X\b|\bMacintosh\b/, "macOS"],
  [/\bLinux\b/, "Linux"],
];

const firstMatch = (ua: string, patterns: [RegExp, string][]): string | null =>
  patterns.find(([pattern]) => pattern.test(ua))?.[1] ?? null;

/** The browser and operating system a User-Agent header names, each null when not recognised. */
export const describeUserAgent = (ua: string | null): DeviceDescription => {
  if (!ua) return { browser: null, os: null };
  return { browser: firstMatch(ua, BROWSERS), os: firstMatch(ua, OPERATING_SYSTEMS) };
};
