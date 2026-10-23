#!/bin/sh
# Prepares RustFS for `forma serve` (docker-compose.dev.yml, run by `make deps-up`, and the `storage` profile of docker/docker-compose.yml): the bucket, its CORS rule for browser uploads, and a service account limited to that bucket. Every step is idempotent. forma2's docker/rustfs-init.sh, on RustFS's own client (rc), since the MinIO client image it pinned is no longer published.
set -e

rustfs_endpoint_url="${RUSTFS_ENDPOINT_URL:-http://rustfs:9000}"

echo 'Waiting for RustFS to be ready...'
attempts=0
max_attempts=30
until rc -q alias set rustfs "$rustfs_endpoint_url" "$RUSTFS_ADMIN_USER" "$RUSTFS_ADMIN_PASSWORD" --bucket-lookup path >/dev/null 2>&1 \
  && rc -q ready rustfs >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  if [ $attempts -ge $max_attempts ]; then
    printf 'Failed to connect to RustFS after %s attempts\n' $max_attempts
    exit 1
  fi
  printf '...still waiting, attempt %s/%s\n' $attempts $max_attempts
  sleep 2
done

echo 'Creating the bucket...'
rc -q bucket create "rustfs/$RUSTFS_BUCKET_NAME" --ignore-existing

if [ -n "${RUSTFS_CORS_ALLOWED_ORIGINS:-}" ]; then
  echo 'Applying the bucket CORS rule...'
  cors_file="/tmp/forma-cors.xml"
  printf '<CORSConfiguration>\n  <CORSRule>\n' > "$cors_file"
  old_ifs=$IFS
  IFS=','
  for origin in $RUSTFS_CORS_ALLOWED_ORIGINS; do
    trimmed_origin=$(printf '%s' "$origin" | tr -d '[:space:]')
    if [ -n "$trimmed_origin" ]; then
      printf '    <AllowedOrigin>%s</AllowedOrigin>\n' "$trimmed_origin" >> "$cors_file"
    fi
  done
  IFS=$old_ifs
  cat >> "$cors_file" << CORS
    <AllowedMethod>GET</AllowedMethod>
    <AllowedMethod>HEAD</AllowedMethod>
    <AllowedMethod>POST</AllowedMethod>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedMethod>DELETE</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
    <ExposeHeader>ETag</ExposeHeader>
    <MaxAgeSeconds>3000</MaxAgeSeconds>
  </CORSRule>
</CORSConfiguration>
CORS
  rc -q bucket cors set "rustfs/$RUSTFS_BUCKET_NAME" "$cors_file"
fi

echo 'Creating the service account policy...'
cat > /tmp/forma-policy.json << POLICY
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"],
      "Resource": ["arn:aws:s3:::$RUSTFS_BUCKET_NAME/*"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::$RUSTFS_BUCKET_NAME"]
    }
  ]
}
POLICY
if ! rc -q admin policy info rustfs "$RUSTFS_POLICY_NAME" >/dev/null 2>&1; then
  rc -q admin policy create rustfs "$RUSTFS_POLICY_NAME" /tmp/forma-policy.json
fi

echo 'Creating the service account...'
if ! rc -q admin user info rustfs "$RUSTFS_SERVICE_USER" >/dev/null 2>&1; then
  rc -q admin user add rustfs "$RUSTFS_SERVICE_USER" "$RUSTFS_SERVICE_PASSWORD"
fi
rc -q admin policy attach rustfs "$RUSTFS_POLICY_NAME" --user "$RUSTFS_SERVICE_USER"

echo 'RustFS is ready.'
