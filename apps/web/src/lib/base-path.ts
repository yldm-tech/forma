/** The path the SPA is served under: the Go server serves the app only below it (config.AppBasePath in apps/server), while APIs, survey pages, stored files and the files of public/ stay at the root. vite.config.ts builds for it and the router takes it as its basepath. */
export const APP_BASE_PATH = "/app";

/** The browser path of an in-app path ("/workspaces/w1" is "/app/workspaces/w1"): what a plain anchor, window.open or a full page load needs. The router's links, navigate and redirect add the base themselves and take the in-app path. */
export const appHref = (path: string): string => `${APP_BASE_PATH}${path}`;
