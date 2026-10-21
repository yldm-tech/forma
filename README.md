<div id="top"></div>

<h3 align="center">Forma</h3>

<p align="center">
Experience management — surveys, responses and workflows.
</p>

## About

Forma is a survey and experience-management platform. Collect feedback with in-app, website, link and
email surveys, read the results in each survey's own summary and response views, and act on them with
Workflows.

**This repository is mid-rewrite.** Forma is moving from a Next.js monolith ([`yldm-tech/forma2`](https://github.com/yldm-tech/forma2), whose history continues here) to a Go server and a TanStack single-page app. Modules land one at a time; until a module is here, forma2 is the running product and the reference for its behaviour. The feature list below describes the product, not what this branch already serves.

The tags `v1.0.0` to `v3.1.0` belong to a different, earlier project that used this repository name; they are unrelated to Forma.

## Features

- Build surveys in a no-code editor with a range of question types, or start from a template.
- Target surveys at specific user groups without shipping application code.
- Share link surveys, or embed them in your website or app.
- Invite your organization to collaborate, with teams and per-workspace roles.
- Automate follow-ups with Workflows.
- Integrate with Slack, Notion, Zapier, n8n and others.
- Drive it programmatically over the v3 REST API or the built-in MCP server.

## Built on

[Go](https://go.dev/) · [Gin](https://gin-gonic.com/) · [GORM](https://gorm.io/) · [PostgreSQL](https://www.postgresql.org/) · [SpiceDB](https://authzed.com/spicedb) · [React](https://react.dev/) · [TanStack Router and Query](https://tanstack.com/) · [Tailwind CSS](https://tailwindcss.com/) · [Vite+](https://viteplus.dev/) · [Preact](https://preactjs.com/) (survey runtime)

## Development

You need [Go](https://go.dev/) (see `server/go.mod`), [Node.js](https://nodejs.org/en) (see `.node-version`), [pnpm](https://pnpm.io/) and [Docker](https://www.docker.com/).

```bash
pnpm install
cp .env.example .env
make deps-up      # PostgreSQL in Docker
make dev-server   # Go server on :3000
make dev-web      # Vite dev server on :5173, proxying the API to :3000
```

| Command      | What it does                                                   |
| ------------ | -------------------------------------------------------------- |
| `make build` | Build the SPA and SDK bundles, embed them, compile `bin/forma` |
| `make test`  | `go test -race` and the Vitest suites                          |
| `make lint`  | golangci-lint and `vp check` (Oxfmt, Oxlint, type-check)       |
| `make check` | Everything CI runs                                             |

`AGENTS.md` is the working reference for this repository: layout, conventions, the API contracts that must not change, and testing policy. Read it before your first change.

## Security

Report vulnerabilities to security@ylam.ai rather than opening a public issue. See
[`SECURITY.md`](./SECURITY.md).

## License

Private. All rights reserved.
