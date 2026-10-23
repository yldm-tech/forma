# syntax=docker/dockerfile:1

# The Forma image: one static `forma` binary with the admin SPA, the public survey page and the SDK bundles (/js/*) embedded, on a distroless base with no shell or package manager. Build from the repository root: `docker build -f deploy/docker/Dockerfile -t forma .`.
#
# The container's default command is `forma start`, forma2's entrypoint: validate the configuration, apply the migrations unless SKIP_STARTUP_MIGRATION=true, then serve on :3000 with the job worker in the same process unless BULLMQ_WORKER_ENABLED=0. The same image runs every other process by naming its subcommand: `serve`, `worker`, `migrate`, `wait-for-database`, `authz ...` (deploy/docker/docker-compose.yml and deploy/helm/forma do).
#
# Both build stages run on the builder's own platform. The web build is platform independent, and Go cross-compiles, so a multi-platform build needs no emulation.

ARG NODE_IMAGE=node:24.14.0-alpine3.23@sha256:7fddd9ddeae8196abf4a3ef2de34e11f7b1a722119f91f28ddf1e99dcafdf114
ARG GO_IMAGE=golang:1.26.10-alpine3.23@sha256:0525c952defebc693cdaf62cdf43afa32be5a7f377e99260642ea19c240c708e
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13@sha256:034df5c1737e8241f6ed6423a12e720ee0f6d6f088609ee172f98f6f2166ef5d

# ---- web: the SPA, the survey page and the SDK bundles (`make web`) ----
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web

# pnpm is the version package.json pins (packageManager), through the corepack release forma2's image used.
RUN npm install --ignore-scripts -g corepack@0.35.0 \
    && corepack enable \
    && corepack prepare pnpm@11.7.0 --activate

WORKDIR /src

# Only the TypeScript workspace: a server change leaves these layers cached. The package store is a cache mount, so a reinstall after a source change downloads nothing. (`pnpm fetch` would let the install layer survive source changes, but it fails on the workspace's patched dependencies.)
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml vite.config.ts .node-version ./
COPY patches ./patches
COPY apps/storybook ./apps/storybook
COPY apps/web ./apps/web
COPY packages ./packages
COPY tooling/typescript ./tooling/typescript
COPY tooling/vite-plugins ./tooling/vite-plugins
# VP_GIT_HOOKS=0: the root `prepare` script installs git hooks, and the build context has no repository.
RUN --mount=type=cache,id=forma-pnpm-store,target=/root/.local/share/pnpm/store \
    VP_GIT_HOOKS=0 pnpm install --frozen-lockfile

# Builds the SDK packages first: web ships their bundles under /js.
RUN pnpm exec vp run -r build

# ---- server: the Go binary with the web build embedded (`make embed server`) ----
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS server

# The toolchain is the image's own; a go.mod asking for another one fails the build instead of downloading it.
ENV GOTOOLCHAIN=local CGO_ENABLED=0

WORKDIR /src/server
COPY apps/server/go.mod apps/server/go.sum ./
RUN --mount=type=cache,id=forma-go-mod,target=/go/pkg/mod \
    go mod download

COPY apps/server ./
COPY --from=web /src/apps/web/dist ./internal/webui/dist

ARG TARGETOS
ARG TARGETARCH
# The release version (release.yml passes it), which telemetry, Sentry and the usage report carry; a build without it reports 0.0.0, an unreleased build.
ARG VERSION=0.0.0
RUN --mount=type=cache,id=forma-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=forma-go-build,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/yldm-tech/forma/apps/server/internal/platform/buildinfo.Version=${VERSION}" -o /out/forma ./cmd/forma

# ---- runtime ----
FROM ${RUNTIME_IMAGE}

COPY --from=server /out/forma /usr/local/bin/forma

# forma2's working directory, kept because SAML SSO reads the identity provider's metadata from saml-connection/ under it: a volume mounted at forma2's /home/nextjs/apps/web/saml-connection keeps working.
WORKDIR /home/nextjs/apps/web

# forma2's uid, which the Helm chart pins in its securityContext (runAsUser 1001). The process writes nothing to disk: uploads go to S3 and the job queue lives in PostgreSQL.
USER 1001:1001

EXPOSE 3000

# Probes /health, which answers without touching a dependency. Containers running `worker` or `migrate` serve no HTTP and turn this off (deploy/docker/docker-compose.yml does).
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/forma", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/forma"]
CMD ["start"]
