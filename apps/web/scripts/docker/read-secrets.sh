#!/bin/sh

set -eu

# Build-time fallbacks used only when Docker secrets are unavailable (for example
# in forked PR validations where repository secrets are not exposed).
DEFAULT_DATABASE_URL="postgresql://test:test@localhost:5432/forma"
DEFAULT_ENCRYPTION_KEY="0123456789abcdef0123456789abcdef"
DEFAULT_REDIS_URL="redis://localhost:6379"

if [ -f "/run/secrets/database_url" ]; then
  IFS= read -r DATABASE_URL < /run/secrets/database_url || true
fi
if [ -z "${DATABASE_URL:-}" ]; then
  DATABASE_URL="${DEFAULT_DATABASE_URL}"
  echo "⚠️  DATABASE_URL secret not found or empty. Using build-time fallback value."
fi
export DATABASE_URL

if [ -f "/run/secrets/encryption_key" ]; then
  IFS= read -r ENCRYPTION_KEY < /run/secrets/encryption_key || true
fi
if [ -z "${ENCRYPTION_KEY:-}" ]; then
  ENCRYPTION_KEY="${DEFAULT_ENCRYPTION_KEY}"
  echo "⚠️  ENCRYPTION_KEY secret not found or empty. Using build-time fallback value."
fi
export ENCRYPTION_KEY

if [ -f "/run/secrets/redis_url" ]; then
  IFS= read -r REDIS_URL < /run/secrets/redis_url || true
fi
if [ -z "${REDIS_URL:-}" ]; then
  REDIS_URL="${DEFAULT_REDIS_URL}"
  echo "⚠️  REDIS_URL secret not found or empty. Using build-time fallback value."
fi
export REDIS_URL





if [ -f "/run/secrets/posthog_key" ]; then
  IFS= read -r POSTHOG_KEY < /run/secrets/posthog_key || true
fi
if [ -n "${POSTHOG_KEY:-}" ]; then
  export POSTHOG_KEY
  echo "✅ POSTHOG_KEY secret found. PostHog proxy rewrites will be generated."
else
  echo "ℹ️  POSTHOG_KEY secret not found. PostHog proxy rewrites will not be generated."
fi

if [ -f "/run/secrets/sentry_auth_token" ]; then
  # The token must be exported on every platform, not just amd64. next.config.mjs only wraps
  # the app in withSentryConfig when SENTRY_AUTH_TOKEN is set, so gating the export by arch
  # meant an arm64 image got no Debug IDs at all and its stack traces could never be
  # symbolicated. Uploading the same Debug IDs twice is idempotent and costs a little build
  # time; shipping an unsymbolicatable image is not a trade worth making.
  IFS= read -r SENTRY_AUTH_TOKEN < /run/secrets/sentry_auth_token || true
  export SENTRY_AUTH_TOKEN
  echo "✅ Sentry auth token found. Debug IDs will be injected (${TARGETARCH:-unknown} platform). Sourcemaps are uploaded only on a release build."
else
  echo "⚠️  SENTRY_AUTH_TOKEN secret not found. Sourcemaps will not be uploaded and Debug IDs will NOT be injected."
fi

# Verify environment variables are set before starting build
echo "  DATABASE_URL: $([ -n "${DATABASE_URL:-}" ] && printf '[SET]' || printf '[NOT SET]')"
echo "  ENCRYPTION_KEY: $([ -n "${ENCRYPTION_KEY:-}" ] && printf '[SET]' || printf '[NOT SET]')"
echo "  REDIS_URL: $([ -n "${REDIS_URL:-}" ] && printf '[SET]' || printf '[NOT SET]')"
echo "  SENTRY_AUTH_TOKEN: $([ -n "${SENTRY_AUTH_TOKEN:-}" ] && printf '[SET]' || printf '[NOT SET]')"
echo "  POSTHOG_KEY: $([ -n "${POSTHOG_KEY:-}" ] && printf '[SET]' || printf '[NOT SET]')"
echo "  TARGETARCH: $([ -n "${TARGETARCH:-}" ] && printf '%s' "${TARGETARCH}" || printf '[NOT SET]')"

exec "$@" 
