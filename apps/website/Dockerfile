# The marketing site on forma.yldm.ai: static HTML for every page in en, zh and ja, served by nginx on :8080 next to the app (/app, the Go server). Build from the repository root, like the other images: `docker build -f apps/website/Dockerfile -t forma-website .`.
#
# SIGNUP_OPEN=1 switches the primary call to action from self-hosting to sign-up; set it only once forma.yldm.ai runs with IS_FORMA_CLOUD=1, or /app/auth/signup is a 404 for visitors.
#
# The runtime is non-root (uid 10001) and writes only to /tmp (pid file, temp paths), so it runs with a read-only root filesystem when /tmp is mounted writable.

ARG NODE_IMAGE=node:24.14.0-alpine3.23@sha256:7fddd9ddeae8196abf4a3ef2de34e11f7b1a722119f91f28ddf1e99dcafdf114
ARG NGINX_IMAGE=nginx:1.28-alpine@sha256:a8b39bd9cf0f83869a2162827a0caf6137ddf759d50a171451b335cecc87d236

# ---- build: the static site (apps/website `build`) ----
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS build

# The same corepack release as deploy/docker/Dockerfile. It provides pnpm at the version package.json pins (packageManager), installed once package.json is in.
RUN npm install --ignore-scripts -g corepack@0.35.0 \
    && corepack enable

WORKDIR /src

# Only the website and what it builds against; the lockfile covers the whole workspace and the filtered install takes its part of it.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .node-version ./
# pnpm as package.json's packageManager names it, the one place its version is written.
RUN corepack install
COPY patches ./patches
COPY tooling/typescript ./tooling/typescript
COPY apps/website ./apps/website
# VP_GIT_HOOKS=0: the root `prepare` script installs git hooks, and the build context has no repository.
RUN --mount=type=cache,id=forma-pnpm-store,target=/root/.local/share/pnpm/store \
    VP_GIT_HOOKS=0 pnpm install --frozen-lockfile --filter "@forma/website..."

ARG SIGNUP_OPEN=0
RUN SIGNUP_OPEN=${SIGNUP_OPEN} pnpm --filter @forma/website build

# ---- runtime ----
FROM ${NGINX_IMAGE}

# The base image's own pages (index.html, 50x.html) go first: only the site's build is served from this root, so /50x.html is a 404 like any other path the site does not have.
RUN rm -rf /usr/share/nginx/html/*

COPY apps/website/nginx/nginx.conf /etc/nginx/nginx.conf
COPY apps/website/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/apps/website/.prerender/redirects.conf /etc/nginx/website/redirects.conf
COPY --from=build /src/apps/website/dist /usr/share/nginx/html

# The files stay root-owned and read-only to the server; the user only needs to read them and write /tmp.
RUN addgroup -g 10001 -S website \
    && adduser -u 10001 -S -G website -H -h /nonexistent website \
    && nginx -t -c /etc/nginx/nginx.conf \
    && rm -rf /tmp/*

USER 10001:10001
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]

# nginx directly, not the base image's entrypoint: its scripts rewrite files under /etc/nginx at start, which a read-only root filesystem refuses.
ENTRYPOINT ["nginx", "-g", "daemon off;"]
