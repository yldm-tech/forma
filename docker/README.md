# Self Host Forma Production Instance

Follow this guide to get your Forma instance up and running with a Postgres DB and SSL certificate using a single script:

## Requirements

Before you proceed, make sure you have the following:

- A Linux Ubuntu Virtual Machine deployed with SSH access.

- An A record set up to connect a custom domain to your instance. Forma will automatically create an SSL certificate for your domain using Let's Encrypt.

## Single Command Setup

Copy and paste the following command into your terminal:

```bash
/bin/sh -c "$(curl -fsSL https://raw.githubusercontent.com/yldm-tech/forma/stable/docker/forma.sh)"
```

The script will prompt you for the following information:

1. **Overwriting Docker GPG Keys**: If Docker GPG keys already exist, the script will ask if you want to overwrite them.

2. **Email Address**: Provide your email address for SSL certificate registration with Let's Encrypt.

3. **Domain Name**: Enter the domain name that Traefik will use to create the SSL certificate and forward requests to Forma.

That's it! After running the command and providing the required information, visit the domain name you entered, and you should see the Forma home wizard!

## AuthZed / SpiceDB

The production and development Compose stacks include one SpiceDB v1.52.0 service backed by a dedicated
`spicedb` database and login in the bundled PostgreSQL server. `authzed-db-bootstrap` creates or updates the
database credentials, `spicedb-migrate` applies datastore migrations, and only then does `spicedb` start. Both
one-shot services are idempotent.

For production Docker, generate `AUTHZED_TOKEN` and `AUTHZED_DATABASE_PASSWORD` with
`openssl rand -hex 32` and keep them in the mode-`0600` `.env` file. SpiceDB remains internal at
`spicedb:50051`; it is not published through Traefik. The one-click installer generates both values and
downloads `authzed-postgres-bootstrap.sh` automatically.

For repository development, `pnpm dev:setup` generates and preserves the same credentials and `pnpm db:up`
starts SpiceDB on `127.0.0.1:50051`. Run the isolated persistence test with:

```bash
pnpm authzed:smoke
```

Run the read-only application client health check with:

```bash
docker compose --profile authzed-ops run --rm authzed-ops health
```

The opt-in operations service uses the same release image and environment as Forma, but never starts during
normal `docker compose up`. The health command accepts an empty schema as healthy, prints exactly one JSON
result, and exits `0` only for a healthy connection. Disabled, invalid, authentication, permission, timeout,
overload, unavailable, and unexpected states exit `1` with a stable `authzed_*` code. It never prints the
token, schema, raw SDK error, or stack trace. It is intentionally not exposed through a browser or HTTP route,
and SpiceDB availability does not affect the normal Forma `/health` result. Restart Forma after
changing AuthZed configuration.

Check or explicitly apply the canonical Forma schema with:

```bash
docker compose --profile authzed-ops run --rm authzed-ops schema check

# Empty instances only
docker compose --profile authzed-ops run --rm authzed-ops schema apply

# Non-empty instances: use the remoteDigest returned by the immediately preceding check
docker compose --profile authzed-ops run --rm authzed-ops schema apply \
  --expected-current-digest sha256:<digest-from-check>

# Relationship audit (dry run)
docker compose --profile authzed-ops run --rm authzed-ops backfill

# Release-matched v6 readiness gate
docker compose --profile authzed-ops run --rm authzed-ops upgrade prepare
docker compose --profile authzed-ops run --rm authzed-ops upgrade check
```

The first apply to an empty SpiceDB needs no additional argument. Replacing a non-empty schema requires
`--expected-current-digest sha256:<digest-from-check>`. The command verifies the write by reading and comparing
the schema again. Fresh installs run the idempotent `authzed-initialize` service independently; Forma
startup and `/health` do not depend on it. Existing upgrades require the explicit preparation and read-only gate. See
the [public operations guide](../docs/self-hosting/advanced/authzed-operations.mdx) for the JSON contract, exit
codes, backup requirements, repair, and rollback rules. Repository development retains the equivalent
`pnpm authzed:*` commands.

`AUTHZED_ENABLED` and `AUTHZED_INSECURE` accept `true`, `false`, `1`, and `0`. Unset means disabled and secure
TLS, respectively. `AUTHZED_ENDPOINT` is a bare `host:port` (including bracketed IPv6) with no scheme or path;
`AUTHZED_CONSISTENCY` accepts both client values, but released v6 Compose deployments require and default to
`fully_consistent`.

To use the optional authenticated grpcui browser in development:

```bash
docker compose -f docker-compose.dev.yml --profile authzed-ui up -d authzed-ui
```

Open `http://127.0.0.1:50052`. The browser UI and gRPC port are development-only.

Existing one-click installations keep their customized Compose file during `forma.sh update`. Merge all
release-matched AuthZed services and the two generated secrets manually, pass `upgrade prepare` and `upgrade
check`, and only then set `FORMA_AUTHZED_V6_MIGRATION_ACKNOWLEDGED=true`. Back up both databases first and
never use `docker compose down -v` during migration or rollback.

The bundled PostgreSQL service keeps `track_commit_timestamp` at its default `off` value. SpiceDB therefore
logs that its Watch API is disabled; schema, relationship, and permission-check APIs are unaffected. A future
consumer of the Watch API must explicitly enable that PostgreSQL setting and account for the required restart.

## Smart Functionality AI with Qwen/vLLM

The Docker stack can optionally run Qwen through vLLM as an OpenAI-compatible `/v1` endpoint. Baseline installs are unchanged: `docker compose up -d` does not start the vLLM service and Forma can still run without AI.

To use the bundled Qwen/vLLM service, add these values to `.env`:

```bash
COMPOSE_PROFILES=qwen
AI_PROVIDER=openai-compatible
AI_MODEL=qwen3-14b-awq
AI_OPENAI_COMPATIBLE_BASE_URL=http://vllm:8000/v1
AI_OPENAI_COMPATIBLE_PROVIDER_NAME=vllm
AI_OPENAI_COMPATIBLE_SUPPORTS_STRUCTURED_OUTPUTS=1
```

Then start the stack after updating `.env`:

```bash
docker compose up -d
docker compose ps
docker compose logs vllm forma
curl -fsS "http://127.0.0.1:${QWEN_VLLM_PORT:-8000}/health"
```

If you set `QWEN_VLLM_PORT` only in `.env`, replace the port in the health check with that value or export it
in your shell first.

The vLLM service requires a GPU-capable Docker host with the NVIDIA Container Toolkit installed. It stores downloaded model files in the `qwen-model-cache` Docker volume and binds the OpenAI-compatible endpoint to `127.0.0.1:8000` by default for local checks.

Use these optional overrides when needed:

```bash
QWEN_VLLM_IMAGE=vllm/vllm-openai:v0.14.0
QWEN_MODEL_ID=Qwen/Qwen3-14B-AWQ
QWEN_SERVED_MODEL_NAME=qwen3-14b-awq
QWEN_MAX_MODEL_LEN=8192
QWEN_MAX_NUM_SEQS=8
QWEN_GPU_MEMORY_UTILIZATION=0.9
QWEN_VLLM_PORT=8000
```

If you run your own Qwen/vLLM service, do not enable the `qwen` profile. Set `AI_PROVIDER=openai-compatible`, `AI_MODEL`, and `AI_OPENAI_COMPATIBLE_BASE_URL` to your endpoint instead.
