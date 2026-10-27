import { queryOptions } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { api } from "../api";

/** What the running instance reports about itself (GET /api/app/instance). */
export interface InstanceInfo {
  /** forma2's IS_STORAGE_CONFIGURED: uploads work only when it is true. */
  storageConfigured: boolean;
  /** Whether workspaces and surveys may set custom head scripts here, or why not: Forma Cloud never runs them, and a self-hosted instance only once PUBLIC_URL gives the survey pages a domain of their own. */
  customScripts: CustomScriptsAvailability;
  /** The release the server was built as; a development build reports DEVELOPMENT_VERSION. */
  version: string;
}

export type CustomScriptsAvailability = "available" | "forma_cloud" | "requires_public_url";

/** Why custom head scripts cannot be set here, or null when they can (or while it is not known yet). */
export const customScriptsUnavailableMessage = (
  t: TFunction,
  availability: CustomScriptsAvailability | undefined
): string | null => {
  switch (availability) {
    case "forma_cloud":
      return t("workspace.custom_scripts_access.unavailable_cloud");
    case "requires_public_url":
      return t("workspace.custom_scripts_access.requires_public_url");
    default:
      return null;
  }
};

/** The version an unreleased server build reports, forma2's version of an unpublished package. */
export const DEVELOPMENT_VERSION = "0.0.0";

export const instanceKeys = {
  info: ["instance"] as const,
  latestRelease: ["instance", "latest-release"] as const,
};

/** The instance does not change while the SPA runs, so it is read once. */
export const instanceQuery = queryOptions({
  queryKey: instanceKeys.info,
  queryFn: ({ signal }) => api.get<InstanceInfo>("/api/app/instance", { signal }),
  staleTime: Infinity,
});

/** The tag of the latest stable Forma release, null while it is unknown. The server caches GitHub's answer for an hour, so the SPA keeps it as long. */
export const latestReleaseQuery = queryOptions({
  queryKey: instanceKeys.latestRelease,
  queryFn: ({ signal }) =>
    api
      .get<{ tagName: string | null }>("/api/app/instance/latest-release", { signal })
      .then((res) => res.tagName),
  staleTime: 60 * 60 * 1000,
});

interface VersionInfo {
  major: number;
  minor: number;
  patch: number;
  prerelease?: string;
}

/** forma2's parseVersion: a semantic version with an optional "v", an optional patch and an optional prerelease. */
const parseVersion = (version: string): VersionInfo | null => {
  const match = /^(\d+)\.(\d+)(?:\.(\d+))?(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(
    version.replace(/^v/, "")
  );
  if (!match) return null;
  const [, major = "", minor = "", patch, prerelease] = match;
  return {
    major: Number.parseInt(major, 10),
    minor: Number.parseInt(minor, 10),
    patch: patch ? Number.parseInt(patch, 10) : 0,
    prerelease,
  };
};

/** forma2's comparePrereleaseVersions: positive when latest is ahead. */
const comparePrereleaseVersions = (current: string, latest: string): number => {
  const currentParts = current.split(".");
  const latestParts = latest.split(".");
  const isNumeric = (s: string) => /^\d+$/.test(s);
  for (let i = 0; i < Math.max(currentParts.length, latestParts.length); i++) {
    const a = currentParts[i];
    const b = latestParts[i];
    if (a === undefined) return 1;
    if (b === undefined) return -1;
    const diff =
      isNumeric(a) && isNumeric(b) ? Number.parseInt(b, 10) - Number.parseInt(a, 10) : b.localeCompare(a);
    if (diff !== 0) return diff;
  }
  return 0;
};

/** forma2's compareVersions: positive when latest is ahead of current; an unparseable pair is equal only when identical. */
export const compareVersions = (current: string, latest: string): number => {
  const c = parseVersion(current);
  const l = parseVersion(latest);
  if (!c || !l) return current === latest ? 0 : -1;
  if (l.major !== c.major) return l.major - c.major;
  if (l.minor !== c.minor) return l.minor - c.minor;
  if (l.patch !== c.patch) return l.patch - c.patch;
  if (!c.prerelease && !l.prerelease) return 0;
  if (!c.prerelease) return -1;
  if (!l.prerelease) return 1;
  return comparePrereleaseVersions(c.prerelease, l.prerelease);
};

/** forma2's isNewerVersion: whether latest is ahead of current. */
export const isNewerVersion = (current: string, latest: string): boolean =>
  compareVersions(current, latest) > 0;
