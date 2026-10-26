<div id="top"></div>

<h3 align="center">Forma</h3>

<p align="center">
Experience management — surveys, responses and workflows.
</p>

## About

Forma is a survey and experience-management platform. Collect feedback with in-app, website, link and
email surveys, read the results in each survey's own summary and response views, and act on them with
Workflows.

Forma is a Go server and a TanStack single-page app. It replaced a Next.js monolith ([`yldm-tech/forma2`](https://github.com/yldm-tech/forma2), now archived, whose history continues here), which is retired: an installation of it cannot be upgraded in place, and its data is not read.

The tags `v1.0.0` to `v3.1.0` belong to a different, earlier project that used this repository name; they are unrelated to Forma.

## Features

- Build surveys in a no-code editor with a range of question types, or start from a template after previewing it as respondents will see it: product and experience surveys, and forms such as contact forms, event registration, lead capture, job applications and course evaluations, in English, Japanese and Chinese. Owners and managers can save a survey as a template of their organization, which every workspace of it then offers in its template gallery.
- Target surveys at specific user groups without shipping application code.
- Share link surveys, or embed them in your website or app.
- Share a survey's results with people who have no account through a read-only link: it shows the summary under the filter it was created with, can expire and be revoked, and shows only the counts of cards with individual answers unless you choose to show them. It never shows who answered or the files they uploaded.
- Invite your organization to collaborate, with teams and per-workspace roles.
- Automate follow-ups with Workflows.
- Integrate with Slack, Notion, Zapier, n8n and others.
- Drive it programmatically over the v3 REST API or the built-in MCP server.

## Built on

[Go](https://go.dev/) · [Gin](https://gin-gonic.com/) · [GORM](https://gorm.io/) · [PostgreSQL](https://www.postgresql.org/) · [SpiceDB](https://authzed.com/spicedb) · [React](https://react.dev/) · [TanStack Router and Query](https://tanstack.com/) · [Tailwind CSS](https://tailwindcss.com/) · [Vite+](https://viteplus.dev/) · [Preact](https://preactjs.com/) (survey runtime)

## Development

You need [Go](https://go.dev/) (see `apps/server/go.mod`), [Node.js](https://nodejs.org/en) (see `.node-version`), [pnpm](https://pnpm.io/) and [Docker](https://www.docker.com/).

```bash
pnpm install
cp .env.example .env
make deps-up      # PostgreSQL, Valkey, SpiceDB, RustFS and Mailpit in Docker
make dev-server   # Go server on :3000
make dev-web      # Vite dev server, the app at http://localhost:5173/app/, proxying the API to :3000
```

| Command      | What it does                                                   |
| ------------ | -------------------------------------------------------------- |
| `make build` | Build the SPA and SDK bundles, embed them, compile `bin/forma` |
| `make test`  | `go test -race` and the Vitest suites                          |
| `make lint`  | golangci-lint and `vp check` (Oxfmt, Oxlint, type-check)       |
| `make check` | The lint, test and generated-file checks CI runs               |

`AGENTS.md` is the working reference for this repository: layout, conventions, the API contracts that must not change, and testing policy. Read it before your first change.

## Self-hosting

Run Forma with [`deploy/docker/docker-compose.yml`](./deploy/docker/docker-compose.yml) or the Helm chart in [`deploy/helm/forma`](./deploy/helm/forma/README.md). Two things to set up deliberately:

- Audit events (sign-ins, changes to members, settings, surveys and other records) are written to the server's log stream as JSON lines with `"level":"audit"`. Forma keeps no audit table and has no page that shows them, so your log pipeline is where they are kept and searched. Set `AUDIT_LOG_GET_USER_IP=1`, with `TRUSTED_PROXY_HOP_COUNT` matching the proxies in front of the server, to record client addresses; without it they read `unknown`.
- Data retention: organization owners choose how long finished workflow runs and survey impressions without a response are kept. The daily sweep only counts what those windows would delete until you set `RETENTION_SWEEP_DRY_RUN=0`. Responses and contacts are never swept.

## Security

Report vulnerabilities to security@yldm.ai rather than opening a public issue. See
[`SECURITY.md`](./SECURITY.md).

## License

Private. All rights reserved.
