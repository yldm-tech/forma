<div id="top"></div>

<h3 align="center">Forma</h3>

<p align="center">
Open source experience management — surveys, responses and workflows.
</p>

<p align="center">
<a href="https://github.com/yldm-tech/forma/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-AGPLv3-purple" alt="License"></a>
</p>

## About

Forma is a survey and experience-management platform. Collect feedback with in-app, website, link and
email surveys, read the results in each survey's own summary and response views, and act on them with
Workflows.

It is a fork of [Formbricks](https://github.com/formbricks/formbricks), maintained by
[yldm-tech](https://github.com/yldm-tech). Two things differ from upstream and are worth knowing before
you compare documentation:

- **Unify Feedback and Dashboards are not part of this fork.** They were built on Forma Hub, an external
  service, and on a Cube.js semantic layer over its records. Both are gone, along with the charts and
  dashboards that had no data source without them. Survey response analysis is unaffected.
- **The database history is a single init migration.** Upstream's migration history was squashed, so a
  database created from this repository cannot be upgraded from one that ran upstream's migrations.

## Features

- Build surveys in a no-code editor with a range of question types, or start from a template.
- Target surveys at specific user groups without shipping application code.
- Share link surveys, or embed them in your website or app.
- Invite your organization to collaborate, with teams and per-workspace roles.
- Automate follow-ups with Workflows.
- Integrate with Slack, Notion, Zapier, n8n and others.
- Drive it programmatically over the v3 REST API or the built-in MCP server.

## Built on

[TypeScript](https://www.typescriptlang.org/) · [Next.js](https://nextjs.org/) ·
[React](https://reactjs.org/) · [Tailwind CSS](https://tailwindcss.com/) · [Prisma](https://prisma.io/) ·
[Better Auth](https://www.better-auth.com/) · [SpiceDB](https://authzed.com/spicedb) ·
[Zod](https://zod.dev/) · [Vitest](https://vitest.dev/)

## Development

You need [Node.js](https://nodejs.org/en) (see `.nvmrc`), [pnpm](https://pnpm.io/) and
[Docker](https://www.docker.com/) — Docker runs PostgreSQL, Valkey, MailHog and SpiceDB for local work.

```bash
pnpm install
pnpm db:up      # start the backing services and write .env from .env.example
pnpm dev        # run every app and worker
```

Useful commands:

| Command                     | What it does                                |
| --------------------------- | ------------------------------------------- |
| `pnpm build`                | Production build for every package and app  |
| `pnpm test`                 | Vitest suites across the workspace          |
| `pnpm typecheck`            | TypeScript across the workspace             |
| `pnpm lint` / `pnpm format` | ESLint and Prettier                         |
| `pnpm test:e2e`             | Playwright browser suite                    |
| `pnpm db:migrate:dev`       | Apply Prisma migrations to the dev database |

`AGENTS.md` is the working reference for this repository: project layout, testing policy, caching rules,
i18n, and the conventions CI enforces. Read it before your first change.

## Self-hosting

Forma is AGPLv3 and can be self-hosted with Docker or the Helm chart in `charts/forma`. See `docs/` for
the setup guides, and `docker/docker-compose.yml` for the production Compose stack.

## Security

Report vulnerabilities to security@forma.ylam.ai rather than opening a public issue. See
[`SECURITY.md`](./SECURITY.md).

## License

Copyright for the upstream code remains with Formbricks GmbH; this fork does not alter the licences it
inherited, and both files are unchanged from upstream.

- **Core** — [AGPLv3](./LICENSE). Free to use, modify and distribute; a modified version you distribute
  or run as a network service must itself be AGPLv3, with your changes documented.
- **Enterprise Edition** — everything under [`apps/web/modules/ee`](./apps/web/modules/ee) is covered by
  a separate [Enterprise licence](./apps/web/modules/ee/LICENSE) and needs a licence key to run in
  production.
