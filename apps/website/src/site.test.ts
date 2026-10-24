import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vite-plus/test";
import {
  ASSET_PREFIX,
  LOCALES,
  PAGES,
  REDIRECTS,
  ROUTES,
  SIGNUP_OPEN,
  isReservedPath,
  localeOfPath,
  notFoundPath,
  ogImagePath,
} from "./site";

describe("routes", () => {
  it("has every page in every locale, each once", () => {
    expect(ROUTES).toHaveLength(LOCALES.length * PAGES.length);
    expect(new Set(ROUTES.map((route) => route.path)).size).toBe(ROUTES.length);
  });

  it("puts English at the root and the others under their prefix, with a trailing slash", () => {
    const paths = ROUTES.map((route) => route.path);
    expect(paths).toContain("/");
    expect(paths).toContain("/zh/");
    expect(paths).toContain("/ja/website-survey/");
    for (const path of paths) expect(path.endsWith("/")).toBe(true);
  });

  it("claims no path the app, the server or the docs own", () => {
    const owned = [
      ...ROUTES.map((route) => route.path),
      ...LOCALES.map(notFoundPath),
      ...ROUTES.map((route) => ogImagePath(route.locale, route.page)),
      ...REDIRECTS.map((redirect) => redirect.from),
      ASSET_PREFIX,
      "/robots.txt",
      "/sitemap.xml",
    ];
    expect(owned.filter(isReservedPath)).toEqual([]);
  });

  it("recognises a reserved prefix and everything below it, but not a longer name", () => {
    expect(isReservedPath("/app")).toBe(true);
    expect(isReservedPath("/app/auth/login")).toBe(true);
    expect(isReservedPath("/s/abc")).toBe(true);
    expect(isReservedPath("/favicon/favicon-32x32.png")).toBe(true);
    expect(isReservedPath("/security/")).toBe(false);
    expect(isReservedPath("/setup-guide")).toBe(false);
    expect(isReservedPath("/favicon.ico")).toBe(true);
  });

  // The root paths apps/server/internal/webui serves or redirects (RootPublicPaths, LegacyAdminPaths): the ingress sends them to the server, so a file the site builds there would never be served.
  it("reserves every root path the server owns", () => {
    const server = [
      "/animated-bgs",
      "/favicon",
      "/favicon.ico",
      "/image-backgrounds",
      "/js",
      "/logo-transparent.png",
      "/sample-csv",
      "/smiley-icons",
      "/star-icons",
      "/account",
      "/auth",
      "/billing-confirmation",
      "/email-change-without-verification-success",
      "/environments",
      "/i",
      "/invite",
      "/organizations",
      "/projects",
      "/setup",
      "/verify-email-change",
      "/workspaces",
    ];
    expect(server.filter((path) => !isReservedPath(path))).toEqual([]);
  });

  it("finds the locale of any path by its prefix", () => {
    expect(localeOfPath("/zh/anything")).toBe("zh");
    expect(localeOfPath("/ja")).toBe("ja");
    expect(localeOfPath("/japan/")).toBe("en");
    expect(localeOfPath("/")).toBe("en");
  });

  it("sends /signup to self-hosting while sign-up is closed", () => {
    expect(SIGNUP_OPEN).toBe(false);
    expect(REDIRECTS.find((redirect) => redirect.from === "/signup")?.to).toBe("/#get-forma");
  });

  it("has a committed share image for every page (pnpm --filter @forma/website og-images)", () => {
    const missing = ROUTES.map((route) => ogImagePath(route.locale, route.page)).filter(
      (path) => !existsSync(fileURLToPath(new URL(`../public${path}`, import.meta.url)))
    );
    expect(missing).toEqual([]);
  });
});
