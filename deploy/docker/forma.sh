#!/usr/bin/env bash
# Forma's one-click installer for a single Ubuntu host: the production stack of deploy/docker/docker-compose.yml behind Traefik, in ./forma.
#
#   ./forma.sh install               ask a few questions, write ./forma (compose file, .env with generated secrets, Traefik) and start it
#   ./forma.sh update                bring ./forma to the current release
#   ./forma.sh stop | restart        docker compose down | restart
#   ./forma.sh logs [args]           docker compose logs [args]
#   ./forma.sh cleanup-rustfs-init   remove the finished RustFS bootstrap container (cleanup-minio-init is an alias)
#   ./forma.sh uninstall             stop the stack and delete ./forma
#
# Environment: FORMA_DIR (default forma) is the installation directory; FORMA_REF (default stable, the release `latest` points at) is the git ref of this repository the compose file and its helper scripts are downloaded from; FORMA_SOURCE_DIR, when set, is a local deploy/docker/ directory to copy them from instead.

set -euo pipefail

FORMA_DIR="${FORMA_DIR:-forma}"
FORMA_REF="${FORMA_REF:-stable}"
FORMA_SOURCE_DIR="${FORMA_SOURCE_DIR:-}"
FORMA_RAW_URL="https://raw.githubusercontent.com/yldm-tech/forma/${FORMA_REF}/deploy/docker"

# The files the stack is made of; docker-compose.yml mounts the two scripts.
FORMA_STACK_FILES="docker-compose.yml authzed-postgres-bootstrap.sh rustfs-init.sh"

# Variables an .env may still set that the server does not read; an update comments them out of .env. BULLMQ_WORKER_CONCURRENCY, BULLMQ_WORKER_COUNT and FORMA_RUSTFS_* are mapped to their replacements first, and forma2's names the server still reads with a deprecation warning are renamed (migrate_env).
FORMA_IGNORED_VARIABLES="CRON_SECRET AUTHZED_SYSTEM_KEY FORMA_AUTHZED_V6_MIGRATION_ACKNOWLEDGED BULLMQ_EXTERNAL_WORKER_ENABLED AUTH_SKIP_INVITE_FOR_SSO AUTH_SSO_DEFAULT_TEAM_ID OIDC_SIGNING_ALGORITHM ENTERPRISE_LICENSE_KEY DEBUG DEBUG_SHOW_RESET_LINK ASSET_PREFIX_URL SENTRY_AUTH_TOKEN OTEL_LOGS_ENABLED"
FORMA_MAPPED_VARIABLES="NEXTAUTH_SECRET BETTER_AUTH_SECRET BULLMQ_WORKER_ENABLED BETTER_AUTH_URL NEXTAUTH_URL SAML_DATABASE_URL BULLMQ_WORKER_CONCURRENCY BULLMQ_WORKER_COUNT FORMA_RUSTFS_ADMIN_USER FORMA_RUSTFS_ADMIN_PASSWORD FORMA_RUSTFS_SERVICE_USER FORMA_RUSTFS_SERVICE_PASSWORD FORMA_RUSTFS_BUCKET_NAME FORMA_RUSTFS_POLICY_NAME FORMA_RUSTFS_REGION"

TRAEFIK_IMAGE="traefik:v3.6.4"

die() {
  echo "Error: $*" >&2
  exit 1
}

# ---- docker ----

forma_docker=(docker)

# configure_docker uses docker directly when the daemon answers, else through sudo.
configure_docker() {
  if docker info >/dev/null 2>&1; then
    forma_docker=(docker)
  elif command -v sudo >/dev/null 2>&1 && sudo docker info >/dev/null 2>&1; then
    forma_docker=(sudo docker)
  else
    die "the Docker daemon is not reachable. Start Docker and run this script again."
  fi
}

compose() {
  "${forma_docker[@]}" compose "$@"
}

# ---- .env ----

# dotenv_get prints the value of key in file, unquoted; nothing when it is unset.
dotenv_get() {
  local key="$1" file="$2"
  [ -f "$file" ] || return 0
  FORMA_KEY="$key" awk '
    BEGIN { key = ENVIRON["FORMA_KEY"] }
    {
      line = $0
      sub(/^[[:space:]]*export[[:space:]]+/, "", line)
      if (index(line, key "=") != 1) next
      value = substr(line, length(key) + 2)
      if (value ~ /^".*"$/) {
        quoted = substr(value, 2, length(value) - 2)
        value = ""
        for (i = 1; i <= length(quoted); i++) {
          c = substr(quoted, i, 1)
          if (c == "\\" && i < length(quoted)) c = substr(quoted, ++i, 1)
          value = value c
        }
      } else if (value ~ /^'"'"'.*'"'"'$/) {
        value = substr(value, 2, length(value) - 2)
      }
      found = value
      seen = 1
    }
    END { if (seen) printf "%s", found }
  ' "$file"
}

dotenv_has() {
  [ -f "$2" ] && grep -Eq "^[[:space:]]*(export[[:space:]]+)?$1=" "$2"
}

# dotenv_quote writes value double-quoted. Docker Compose interpolates ${...} in .env values; literal is 1 for a value that must reach the container as typed (a generated secret, an answer), whose $ is doubled, and 0 for a value taken from a compose file, which Compose already interpolated there.
dotenv_quote() {
  local value="$1" literal="$2"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  if [ "$literal" = 1 ]; then
    value=${value//\$/\$\$}
  fi
  printf '"%s"' "$value"
}

# dotenv_set replaces key's line in file (the first one; later duplicates are removed) or appends it. The file stays mode 0600.
dotenv_set() {
  local key="$1" value="$2" file="$3" literal="${4:-1}" tmp
  touch "$file"
  chmod 600 "$file"
  tmp=$(mktemp "${file}.XXXXXX")
  FORMA_KEY="$key" FORMA_LINE="$key=$(dotenv_quote "$value" "$literal")" awk '
    BEGIN { key = ENVIRON["FORMA_KEY"]; line = ENVIRON["FORMA_LINE"] }
    {
      current = $0
      sub(/^[[:space:]]*export[[:space:]]+/, "", current)
      if (index(current, key "=") == 1) {
        if (!done) print line
        done = 1
        next
      }
      print
    }
    END { if (!done) print line }
  ' "$file" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$file"
}

dotenv_set_if_missing() {
  dotenv_has "$1" "$3" || dotenv_set "$@"
}

# dotenv_comment_out turns key's lines into comments that say why, so nothing is lost.
dotenv_comment_out() {
  local key="$1" reason="$2" file="$3" tmp
  dotenv_has "$key" "$file" || return 0
  tmp=$(mktemp "${file}.XXXXXX")
  FORMA_KEY="$key" FORMA_REASON="$reason" awk '
    BEGIN { key = ENVIRON["FORMA_KEY"]; reason = ENVIRON["FORMA_REASON"] }
    {
      current = $0
      sub(/^[[:space:]]*export[[:space:]]+/, "", current)
      if (index(current, key "=") == 1) { print "# " reason ": " current; next }
      print
    }
  ' "$file" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$file"
}

# strip_schema_public removes Prisma's schema=public parameter from a connection URL. The server drops it too, and refuses any other schema value.
strip_schema_public() {
  printf '%s' "$1" | sed -E \
    -e 's/\?[sS][cC][hH][eE][mM][aA]=public&/?/' \
    -e 's/&[sS][cC][hH][eE][mM][aA]=public(&|$)/\1/' \
    -e 's/\?[sS][cC][hH][eE][mM][aA]=public$//'
}

cpu_count() {
  getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || echo 2
}

is_positive_integer() {
  case "$1" in
    '' | *[!0-9]* | 0*) return 1 ;;
    *) return 0 ;;
  esac
}

# rename_env gives new the value of old, unless new is already set, and comments old out. The value is written back as it was spelled (literal 0).
rename_env() {
  local old="$1" new="$2" file="$3" value
  dotenv_has "$old" "$file" || return 0
  value=$(dotenv_get "$old" "$file")
  if [ -z "$value" ]; then
    echo "  $old: dropped, it was empty"
  elif [ -n "$(dotenv_get "$new" "$file")" ]; then
    echo "  $old: dropped, $new is already set"
  else
    dotenv_set "$new" "$value" "$file" 0
    echo "  $old renamed to $new"
  fi
  dotenv_comment_out "$old" "renamed to $new" "$file"
}

# migrate_env rewrites the retired variables of an .env into the ones the server reads. Every step is idempotent, so it runs on every update. A value read from .env is written back as it was spelled there (literal 0).
migrate_env() {
  local file="$1" key value concurrency count cpus profiles

  for key in DATABASE_URL MIGRATE_DATABASE_URL; do
    if dotenv_has "$key" "$file"; then
      value=$(dotenv_get "$key" "$file")
      if [ "$(strip_schema_public "$value")" != "$value" ]; then
        dotenv_set "$key" "$(strip_schema_public "$value")" "$file" 0
        echo "  $key: removed Prisma's ?schema=public"
      fi
    fi
  done

  # BULLMQ_WORKER_COUNT workers of BULLMQ_WORKER_CONCURRENCY jobs each (one worker of 2 to 4 jobs by default) become WORKER_CONCURRENCY, which forma worker applies to each of its default and webhooks queues.
  concurrency=$(dotenv_get BULLMQ_WORKER_CONCURRENCY "$file")
  count=$(dotenv_get BULLMQ_WORKER_COUNT "$file")
  if [ -n "$concurrency$count" ]; then
    if [ -n "$(dotenv_get WORKER_CONCURRENCY "$file")" ]; then
      echo "  BULLMQ_WORKER_CONCURRENCY/BULLMQ_WORKER_COUNT: dropped, WORKER_CONCURRENCY is already set"
    elif { [ -z "$concurrency" ] || is_positive_integer "$concurrency"; } && { [ -z "$count" ] || is_positive_integer "$count"; }; then
      if [ -z "$concurrency" ]; then
        cpus=$(cpu_count)
        concurrency=$((cpus < 2 ? 2 : (cpus > 4 ? 4 : cpus)))
      fi
      dotenv_set WORKER_CONCURRENCY "$((concurrency * ${count:-1}))" "$file"
      echo "  WORKER_CONCURRENCY=$((concurrency * ${count:-1})) replaces BULLMQ_WORKER_CONCURRENCY x BULLMQ_WORKER_COUNT; it applies to each of the worker's default and webhooks queues, so the worker runs up to $((2 * concurrency * ${count:-1} + 2)) jobs at once (lower WORKER_CONCURRENCY to keep the old total)"
    else
      die "BULLMQ_WORKER_CONCURRENCY ($concurrency) and BULLMQ_WORKER_COUNT ($count) in $file must be positive integers; fix or remove them and run the update again"
    fi
    dotenv_comment_out BULLMQ_WORKER_CONCURRENCY "replaced by WORKER_CONCURRENCY" "$file"
    dotenv_comment_out BULLMQ_WORKER_COUNT "replaced by WORKER_CONCURRENCY" "$file"
  fi

  # Older installs kept the bundled RustFS credentials under FORMA_RUSTFS_*; the compose file's storage profile reads RUSTFS_ADMIN_* and the S3_* settings the server uses.
  if dotenv_has FORMA_RUSTFS_ADMIN_USER "$file"; then
    dotenv_set_if_missing RUSTFS_ADMIN_USER "$(dotenv_get FORMA_RUSTFS_ADMIN_USER "$file")" "$file" 0
    dotenv_set_if_missing RUSTFS_ADMIN_PASSWORD "$(dotenv_get FORMA_RUSTFS_ADMIN_PASSWORD "$file")" "$file" 0
    dotenv_set_if_missing S3_ACCESS_KEY "$(dotenv_get FORMA_RUSTFS_SERVICE_USER "$file")" "$file" 0
    dotenv_set_if_missing S3_SECRET_KEY "$(dotenv_get FORMA_RUSTFS_SERVICE_PASSWORD "$file")" "$file" 0
    dotenv_set_if_missing S3_BUCKET_NAME "$(dotenv_get FORMA_RUSTFS_BUCKET_NAME "$file")" "$file" 0
    dotenv_set_if_missing S3_REGION "$(dotenv_get FORMA_RUSTFS_REGION "$file")" "$file" 0
    dotenv_set_if_missing S3_FORCE_PATH_STYLE 1 "$file"
    profiles=$(dotenv_get COMPOSE_PROFILES "$file")
    case ",$profiles," in
      *,storage,*) ;;
      ,,) dotenv_set COMPOSE_PROFILES storage "$file" ;;
      *) dotenv_set COMPOSE_PROFILES "$profiles,storage" "$file" ;;
    esac
    echo "  bundled RustFS: FORMA_RUSTFS_* mapped to RUSTFS_ADMIN_* and S3_*, COMPOSE_PROFILES includes storage"
  fi
  for key in $FORMA_MAPPED_VARIABLES; do
    case "$key" in
      FORMA_RUSTFS_*) dotenv_comment_out "$key" "mapped to RUSTFS_ADMIN_* and S3_* by forma.sh" "$file" ;;
    esac
  done

  # forma2's names the server still reads, with a deprecation warning, until a later release drops them.
  rename_env NEXTAUTH_SECRET LINK_SURVEY_TOKEN_SECRET "$file"
  rename_env BETTER_AUTH_SECRET LINK_SURVEY_PIN_TOKEN_SECRET "$file"
  rename_env BULLMQ_WORKER_ENABLED WORKER_ENABLED "$file"
  # BETTER_AUTH_URL, else NEXTAUTH_URL, became AUTH_URL. forma2's installer set NEXTAUTH_URL to WEBAPP_URL, which the server falls back to on its own, so that value is not carried over.
  value=$(dotenv_get BETTER_AUTH_URL "$file")
  [ -n "$value" ] || value=$(dotenv_get NEXTAUTH_URL "$file")
  if [ -n "$value" ] && [ -z "$(dotenv_get AUTH_URL "$file")" ]; then
    if [ "${value%/}" = "$(dotenv_get WEBAPP_URL "$file" | sed 's:/*$::')" ]; then
      echo "  BETTER_AUTH_URL/NEXTAUTH_URL: dropped, the same as WEBAPP_URL"
    else
      dotenv_set AUTH_URL "$value" "$file" 0
      echo "  AUTH_URL replaces BETTER_AUTH_URL/NEXTAUTH_URL"
    fi
  fi
  dotenv_comment_out BETTER_AUTH_URL "replaced by AUTH_URL" "$file"
  dotenv_comment_out NEXTAUTH_URL "replaced by AUTH_URL" "$file"
  # Any value of SAML_DATABASE_URL switched SAML on.
  if [ -n "$(dotenv_get SAML_DATABASE_URL "$file")" ]; then
    if [ -n "$(dotenv_get SAML_ENABLED "$file")" ]; then
      echo "  SAML_DATABASE_URL: dropped, SAML_ENABLED is already set"
    else
      dotenv_set SAML_ENABLED 1 "$file"
      echo "  SAML_ENABLED=1 replaces SAML_DATABASE_URL"
    fi
  fi
  dotenv_comment_out SAML_DATABASE_URL "replaced by SAML_ENABLED" "$file"

  for key in $FORMA_IGNORED_VARIABLES; do
    if dotenv_has "$key" "$file"; then
      dotenv_comment_out "$key" "not read by the Go server" "$file"
      echo "  $key: commented out, the Go server does not read it"
    fi
  done
}

# ---- compose files ----

# fetch writes one file of the stack: from FORMA_SOURCE_DIR when set, else downloaded from FORMA_REF. A file it replaces with different content is kept as <name>.<backup>.
fetch() {
  local name="$1" backup="$2"
  if [ -n "$FORMA_SOURCE_DIR" ]; then
    cp "$FORMA_SOURCE_DIR/$name" "$name.tmp"
  elif ! curl -fsSL -o "$name.tmp" "$FORMA_RAW_URL/$name"; then
    rm -f "$name.tmp"
    die "could not download $FORMA_RAW_URL/$name. Check the network, or set FORMA_REF to a release tag (v0.2.<n>)."
  fi
  if [ -f "$name" ] && ! cmp -s "$name" "$name.tmp"; then
    cp -p "$name" "$name.$backup"
    echo "  $name replaced; the previous one is $name.$backup"
  fi
  mv "$name.tmp" "$name"
}

fetch_stack() {
  local name
  for name in $FORMA_STACK_FILES; do
    fetch "$name" "$1"
  done
  chmod 700 authzed-postgres-bootstrap.sh rustfs-init.sh
}

# is_forma2_compose recognises the compose file of the retired Next.js release (forma2): its containers ran shell, node and forma-authzed entrypoints, which the distroless image does not have. Its data lives in tables this server does not read, so this script neither installs over nor updates such an installation.
is_forma2_compose() {
  grep -Eq 'apply-migrations\.js|validate-env\.mjs|^x-environment:|^[[:space:]]+(entrypoint|command):.*forma-authzed' "$1"
}

override_header() {
  echo "# Written by forma.sh: the Traefik reverse proxy in front of the stack in docker-compose.yml. Docker Compose merges this file into docker-compose.yml on its own; forma.sh update replaces docker-compose.yml with the release's and keeps this file, so put local changes here."
  echo "services:"
}

# ---- database ----

psql_forma() {
  compose exec -T postgres psql -U postgres -d forma -tA -c "$1" | tr -d '[:space:]'
}

# check_database is the server's own startup refusal (database.RefuseUnreadForma2Data), run before anything but PostgreSQL is started: the database holds forma2's data (its "User" table has rows) and no user of this server's. The server does not read forma2's tables, so started there it would serve an empty instance whose first sign-up becomes its administrator. FORMA_IGNORE_FORMA2_DATA=1 in .env skips the check here and in the server.
check_database() {
  local tables counts
  case "$(dotenv_get FORMA_IGNORE_FORMA2_DATA .env)" in
    1 | t | T | true | TRUE | True) return 0 ;;
  esac
  compose up -d --wait postgres >/dev/null
  tables=$(psql_forma "SELECT (to_regclass('public.\"User\"') IS NOT NULL)::text || ',' || (to_regclass('public.users') IS NOT NULL)::text")
  case "$tables" in
    true,true) counts=$(psql_forma "SELECT (EXISTS (SELECT 1 FROM \"User\"))::text || ',' || (EXISTS (SELECT 1 FROM users))::text") ;;
    true,*) counts=$(psql_forma "SELECT (EXISTS (SELECT 1 FROM \"User\"))::text || ',false'") ;;
    *) return 0 ;;
  esac
  if [ "$counts" = "true,false" ]; then
    cat >&2 <<EOF
The database holds the data of the retired Next.js release (forma2), and the server has no user of its own: it does not read forma2's tables, so started now it would serve an empty instance whose first sign-up becomes its administrator.
Nothing but PostgreSQL was started. To keep forma2's data, import it with release v0.2.48 or earlier (forma import-forma2) first. To start empty on purpose, set FORMA_IGNORE_FORMA2_DATA=1 in $PWD/.env and run ./forma.sh update again.
EOF
    exit 1
  fi
}

# ---- commands ----

ask() {
  local prompt="$1" default="${2:-}" answer
  read -r -p "$prompt " answer
  answer=$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]')
  printf '%s' "${answer:-$default}"
}

install_docker() {
  echo "Updating the package list and installing the dependencies."
  sudo apt-get update >/dev/null 2>&1
  sudo apt-get install -y ca-certificates curl jq lsb-release openssl >/dev/null 2>&1

  if command -v docker >/dev/null 2>&1; then
    echo "Docker is already installed."
  else
    echo "Legacy Docker packages (docker, docker-engine, docker.io, containerd, runc) can conflict with Docker CE; they are removed only with your consent."
    if [ "$(ask "Remove the legacy packages? [y/N]" n)" = y ]; then
      sudo apt-get remove -y docker docker-engine docker.io containerd runc >/dev/null 2>&1 || true
    fi
    echo "Installing Docker from Docker's repository."
    sudo apt-get install -y gnupg >/dev/null 2>&1
    sudo mkdir -m 0755 -p /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
    sudo apt-get update >/dev/null 2>&1
    sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >/dev/null 2>&1
    docker --version >/dev/null 2>&1 || die "Docker could not be installed."
  fi
  configure_docker
  compose version >/dev/null 2>&1 || die "'docker compose' is not available. Install the Docker Compose plugin and run this script again."
  sudo groupadd docker >/dev/null 2>&1 || true
  sudo usermod -aG docker "$USER" >/dev/null 2>&1 || true
}

write_traefik_config() {
  local https_setup="$1" hsts="$2" email="$3"
  {
    echo "entryPoints:"
    echo "  web:"
    echo "    address: \":80\""
    if [ "$hsts" = y ]; then
      echo "    http:"
      echo "      redirections:"
      echo "        entryPoint:"
      echo "          to: websecure"
      echo "          scheme: https"
      echo "          permanent: true"
    fi
    echo "    transport:"
    echo "      respondingTimeouts:"
    echo "        readTimeout: 60s"
    echo "  websecure:"
    echo "    address: \":443\""
    echo "    transport:"
    echo "      respondingTimeouts:"
    echo "        readTimeout: 60s"
    echo "    http:"
    echo "      tls:"
    if [ "$https_setup" = y ]; then
      echo "        certResolver: default"
    fi
    echo "        options: default"
    if [ "$hsts" = y ]; then
      echo "      middlewares:"
      echo "        - hstsHeader"
    fi
    echo "providers:"
    echo "  docker:"
    echo "    watch: true"
    echo "    exposedByDefault: false"
    echo "  file:"
    echo "    filename: /traefik-dynamic.yaml"
    if [ "$https_setup" = y ]; then
      echo "certificatesResolvers:"
      echo "  default:"
      echo "    acme:"
      echo "      email: $email"
      echo "      storage: acme.json"
      echo "      caServer: \"https://acme-v02.api.letsencrypt.org/directory\""
      echo "      tlsChallenge: {}"
    fi
  } >traefik.yaml
  cat >traefik-dynamic.yaml <<'EOF'
# Minimum TLS version and cipher suites; TLS 1.3 suites are not configurable in Traefik and are always on.
tls:
  options:
    default:
      minVersion: VersionTLS12
      cipherSuites:
        - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
        - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
        - TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305
        - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
        - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
        - TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305
      curvePreferences:
        - CurveP521
        - CurveP384
      sniStrict: true
      alpnProtocols:
        - h2
        - http/1.1
        - acme-tls/1
EOF
  if [ "$https_setup" = y ]; then
    touch acme.json
    chmod 600 acme.json
  fi
}

write_install_override() {
  local domain="$1" https_setup="$2" hsts="$3" files_domain="$4" scheme=https entrypoint=websecure
  if [ "$https_setup" != y ]; then
    scheme=http
    entrypoint=web
  fi
  {
    override_header
    echo "  forma:"
    echo "    labels:"
    echo "      - \"traefik.enable=true\""
    echo "      - \"traefik.http.routers.forma.rule=Host(\`$domain\`)\""
    echo "      - \"traefik.http.routers.forma.entrypoints=websecure\""
    echo "      - \"traefik.http.routers.forma.tls=true\""
    if [ "$https_setup" = y ]; then
      echo "      - \"traefik.http.routers.forma.tls.certresolver=default\""
    fi
    echo "      - \"traefik.http.services.forma.loadbalancer.server.port=3000\""
    if [ "$hsts" = y ]; then
      echo "      - \"traefik.http.middlewares.hstsHeader.headers.stsSeconds=31536000\""
      echo "      - \"traefik.http.middlewares.hstsHeader.headers.forceSTSHeader=true\""
      echo "      - \"traefik.http.middlewares.hstsHeader.headers.stsPreload=true\""
      echo "      - \"traefik.http.middlewares.hstsHeader.headers.stsIncludeSubdomains=true\""
    else
      echo "      - \"traefik.http.routers.forma_http.entrypoints=web\""
      echo "      - \"traefik.http.routers.forma_http.rule=Host(\`$domain\`)\""
    fi
    if [ -n "$files_domain" ]; then
      echo "  rustfs:"
      echo "    labels:"
      echo "      - \"traefik.enable=true\""
      echo "      - \"traefik.http.routers.rustfs-s3.rule=Host(\`$files_domain\`)\""
      echo "      - \"traefik.http.routers.rustfs-s3.entrypoints=$entrypoint\""
      if [ "$https_setup" = y ]; then
        echo "      - \"traefik.http.routers.rustfs-s3.tls=true\""
        echo "      - \"traefik.http.routers.rustfs-s3.tls.certresolver=default\""
      fi
      echo "      - \"traefik.http.routers.rustfs-s3.service=rustfs-s3\""
      echo "      - \"traefik.http.services.rustfs-s3.loadbalancer.server.port=9000\""
      echo "      - \"traefik.http.routers.rustfs-s3.middlewares=rustfs-cors,rustfs-ratelimit\""
      echo "      - \"traefik.http.middlewares.rustfs-cors.headers.accesscontrolallowmethods=GET,PUT,POST,DELETE,HEAD,OPTIONS\""
      echo "      - \"traefik.http.middlewares.rustfs-cors.headers.accesscontrolallowheaders=*\""
      echo "      - \"traefik.http.middlewares.rustfs-cors.headers.accesscontrolalloworiginlist=$scheme://$domain\""
      echo "      - \"traefik.http.middlewares.rustfs-cors.headers.accesscontrolmaxage=100\""
      echo "      - \"traefik.http.middlewares.rustfs-cors.headers.addvaryheader=true\""
      echo "      - \"traefik.http.middlewares.rustfs-ratelimit.ratelimit.average=100\""
      echo "      - \"traefik.http.middlewares.rustfs-ratelimit.ratelimit.burst=200\""
    fi
    echo "  traefik:"
    echo "    image: \"$TRAEFIK_IMAGE\""
    echo "    restart: always"
    echo "    container_name: \"traefik\""
    echo "    depends_on:"
    echo "      - forma"
    echo "    ports:"
    echo "      - \"80:80\""
    echo "      - \"443:443\""
    echo "    volumes:"
    echo "      - ./traefik.yaml:/traefik.yaml"
    echo "      - ./traefik-dynamic.yaml:/traefik-dynamic.yaml"
    if [ "$https_setup" = y ]; then
      echo "      - ./acme.json:/acme.json"
    fi
    echo "      - /var/run/docker.sock:/var/run/docker.sock:ro"
  } >docker-compose.override.yml
}

install_forma() {
  local domain https_setup hsts=n email="" files_domain="" scheme answer key

  echo "Forma setup: the Forma server, its job worker, PostgreSQL, Valkey and SpiceDB with Docker Compose, behind Traefik."
  install_docker

  mkdir -p "$FORMA_DIR"
  cd "$FORMA_DIR"
  if [ -f docker-compose.yml ] && is_forma2_compose docker-compose.yml; then
    die "$PWD holds an installation of the retired Next.js release (forma2), which this script does not install over. Install into another directory (FORMA_DIR)."
  fi

  read -r -p "Your app domain (for example surveys.example.com), without http:// or https://: " domain
  [ -n "$domain" ] || die "a domain is required."
  https_setup=$(ask "Set up an HTTPS certificate with Let's Encrypt? [Y/n]" y)
  if [ "$https_setup" = y ]; then
    if [ "$(ask "Does $domain point at this server, with ports 80 and 443 open? [Y/n]" y)" = y ]; then
      read -r -p "Email address for the certificate: " email
    else
      echo "Without that a certificate cannot be issued; set up HTTPS yourself."
      https_setup=n
    fi
  fi
  hsts=$(ask "Enforce HTTPS (HSTS)? [Y/n]" y)
  scheme=https
  [ "$https_setup" = y ] || [ "$hsts" = y ] || scheme=http

  # Secrets already in .env (a second run of install) are kept.
  umask 077
  touch .env
  chmod 600 .env
  dotenv_set WEBAPP_URL "$scheme://$domain" .env
  dotenv_set_if_missing ENCRYPTION_KEY "$(openssl rand -hex 32)" .env
  if ! dotenv_has POSTGRES_PASSWORD .env; then
    answer=$(openssl rand -hex 32)
    dotenv_set POSTGRES_PASSWORD "$answer" .env
    dotenv_set POSTGRES_PASSWORD_URL_ENCODED "$answer" .env
  fi
  dotenv_set_if_missing AUTHZED_TOKEN "$(openssl rand -hex 32)" .env
  dotenv_set_if_missing AUTHZED_DATABASE_PASSWORD "$(openssl rand -hex 32)" .env

  if [ "$(ask "Set up email (you need SMTP credentials)? [y/N]" n)" = y ]; then
    for key in MAIL_FROM MAIL_FROM_NAME SMTP_HOST SMTP_PORT SMTP_USER; do
      read -r -p "$key: " answer
      dotenv_set "$key" "$answer" .env
    done
    read -r -s -p "SMTP_PASSWORD: " answer
    echo
    dotenv_set SMTP_PASSWORD "$answer" .env
    dotenv_set SMTP_AUTHENTICATED "$(ask "Authenticated SMTP? 1 for yes, 0 for no [1]" 1)" .env
    dotenv_set SMTP_SECURE_ENABLED "$(ask "Implicit TLS (port 465)? 1 for yes, 0 for no [0]" 0)" .env
  else
    # Without mail, nobody could confirm an address or reset a password.
    dotenv_set_if_missing EMAIL_VERIFICATION_DISABLED 1 .env
    dotenv_set_if_missing PASSWORD_RESET_DISABLED 1 .env
  fi

  echo "File uploads power images in surveys, the File Upload and Picture Selection questions, workspace logos and the organization logo in emails."
  if [ "$(ask "Configure file uploads? [Y/n]" y)" = y ]; then
    if [ "$(ask "Use an external S3-compatible storage (AWS S3, DigitalOcean Spaces, ...)? [y/N]" n)" = y ]; then
      read -r -p "S3 access key: " answer
      dotenv_set S3_ACCESS_KEY "$answer" .env
      read -r -s -p "S3 secret key: " answer
      echo
      dotenv_set S3_SECRET_KEY "$answer" .env
      read -r -p "S3 region (for example us-east-1): " answer
      dotenv_set S3_REGION "$answer" .env
      read -r -p "S3 bucket name: " answer
      dotenv_set S3_BUCKET_NAME "$answer" .env
      read -r -p "S3 endpoint URL (leave empty for AWS S3): " answer
      if [ -n "$answer" ]; then
        dotenv_set S3_ENDPOINT_URL "$answer" .env
        dotenv_set S3_FORCE_PATH_STYLE 1 .env
      fi
    else
      read -r -p "Subdomain for the bundled object storage [files.$domain]: " files_domain
      files_domain="${files_domain:-files.$domain}"
      dotenv_set_if_missing RUSTFS_ADMIN_USER "forma-$(openssl rand -hex 4)" .env
      dotenv_set_if_missing RUSTFS_ADMIN_PASSWORD "$(openssl rand -hex 20)" .env
      dotenv_set_if_missing S3_ACCESS_KEY "forma-service-$(openssl rand -hex 4)" .env
      dotenv_set_if_missing S3_SECRET_KEY "$(openssl rand -hex 20)" .env
      dotenv_set_if_missing S3_BUCKET_NAME forma-uploads .env
      dotenv_set S3_REGION us-east-1 .env
      dotenv_set S3_FORCE_PATH_STYLE 1 .env
      dotenv_set S3_ENDPOINT_URL "$scheme://$files_domain" .env
      dotenv_set COMPOSE_PROFILES storage .env
    fi
  else
    echo "File uploads are off; run the installer again or set S3_* in $PWD/.env to turn them on."
  fi

  echo "Downloading the compose file and its helper scripts ($FORMA_REF)."
  fetch_stack "before-install-$(date +%Y%m%d%H%M%S)"
  write_traefik_config "$https_setup" "$hsts" "$email"
  write_install_override "$domain" "$https_setup" "$hsts" "$files_domain"
  compose config -q || die "the generated compose configuration is invalid."

  echo "Starting Forma: the database, SpiceDB and the migrations come first, then the server and the worker."
  compose pull
  compose up -d --remove-orphans
  echo "Forma is starting at $scheme://$domain. Settings live in $PWD/.env (mode 0600); edit it and run 'docker compose up -d' in $PWD to apply a change."
  echo "Make sure the DNS record of $domain${files_domain:+ and $files_domain} points at this server. Check the containers with 'cd $PWD && docker compose ps'."
}

update_forma() {
  local stamp name
  [ -d "$FORMA_DIR" ] || die "no installation in ./$FORMA_DIR. Run this script from the directory you installed from, or set FORMA_DIR."
  cd "$FORMA_DIR"
  [ -f docker-compose.yml ] || die "$PWD has no docker-compose.yml."
  if is_forma2_compose docker-compose.yml; then
    die "$PWD holds an installation of the retired Next.js release (forma2), which this script does not update: its data lives in tables this server does not read."
  fi
  configure_docker
  stamp=$(date +%Y%m%d%H%M%S)

  touch .env
  chmod 600 .env
  cp -p .env ".env.$stamp"
  echo "Updating .env:"
  migrate_env .env
  [ -n "$(dotenv_get WEBAPP_URL .env)" ] || die "WEBAPP_URL is not set in $PWD/.env."
  [ -n "$(dotenv_get ENCRYPTION_KEY .env)" ] || die "ENCRYPTION_KEY is not set in $PWD/.env. Copy the value the instance has always used; a new key cannot open the secrets sealed with the old one."
  [ -n "$(dotenv_get POSTGRES_PASSWORD .env)" ] || die "POSTGRES_PASSWORD is not set in $PWD/.env."
  dotenv_set_if_missing AUTHZED_TOKEN "$(openssl rand -hex 32)" .env
  dotenv_set_if_missing AUTHZED_DATABASE_PASSWORD "$(openssl rand -hex 32)" .env

  echo "Writing the release's compose file ($FORMA_REF)."
  fetch_stack "$stamp"
  if ! compose config -q; then
    echo "The new compose configuration is invalid; restoring the previous files." >&2
    cp -p ".env.$stamp" .env
    for name in $FORMA_STACK_FILES; do
      if [ -f "$name.$stamp" ]; then cp -p "$name.$stamp" "$name"; fi
    done
    exit 1
  fi

  compose pull
  check_database
  # forma-migrate applies the schema and authzed-initialize the authorization schema before the server and the worker start (depends_on).
  compose up -d --remove-orphans
  echo "Forma is updated. Follow the start with 'cd $PWD && docker compose logs -f forma forma-worker'."
}

stop_forma() {
  cd "$FORMA_DIR"
  configure_docker
  compose down
  echo "Forma is stopped."
}

restart_forma() {
  cd "$FORMA_DIR"
  configure_docker
  compose restart
  echo "Forma is restarted."
}

get_logs() {
  cd "$FORMA_DIR"
  configure_docker
  compose logs "$@"
}

cleanup_rustfs_init() {
  cd "$FORMA_DIR"
  configure_docker
  compose rm -f -s rustfs-init >/dev/null 2>&1 || true
  compose rm -f -s minio-init >/dev/null 2>&1 || true
  echo "The RustFS bootstrap container is removed; it runs again, harmlessly, on the next 'docker compose up'."
}

uninstall_forma() {
  if [ "$(ask "Uninstall Forma? This stops it and deletes ./$FORMA_DIR with its configuration and secrets. (yes/no)" no)" != yes ]; then
    echo "Uninstall cancelled."
    return 0
  fi
  (
    cd "$FORMA_DIR"
    configure_docker
    compose down
  )
  sudo rm -rf "$FORMA_DIR"
  echo "Forma is uninstalled. Its data volumes are kept; 'docker volume ls' lists them (forma_postgres, forma_redis, ...), 'docker volume rm' deletes them."
}

main() {
  case "${1:-install}" in
    install) install_forma ;;
    update) update_forma ;;
    stop) stop_forma ;;
    restart) restart_forma ;;
    logs)
      shift
      get_logs "$@"
      ;;
    cleanup-rustfs-init | cleanup-minio-init) cleanup_rustfs_init ;;
    uninstall) uninstall_forma ;;
    *) die "unknown command $1 (install, update, stop, restart, logs, cleanup-rustfs-init, uninstall)" ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
