#!/usr/bin/env bash
set -euo pipefail

# Builds the documentation as a static site served from `/docs/` on the app's own domain.
#
# Mintlify exports a site that assumes it owns the root: every asset and link is an absolute path.
# There is no base-path option — `mintlify export` takes only --output, --groups and
# --disable-openapi — and the value is baked into the build, so the paths are rewritten here
# instead. Verified in a browser: pages render, styles and scripts load, and the only requests that
# still miss are Next.js RSC prefetches, whose routes are compiled into the chunks and cannot be
# rewritten. Navigation still works; it falls back to a full page load.

readonly DOCS_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly OUT_DIR="${1:-${DOCS_DIR}/.out}"
readonly BASE_PATH="/docs"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

echo "==> exporting"
(cd "$DOCS_DIR" && npx --yes mintlify@latest export --output "${OUT_DIR}/export.zip")

echo "==> unpacking"
unzip -q "${OUT_DIR}/export.zip" -d "$OUT_DIR"
rm -f "${OUT_DIR}/export.zip"

echo "==> rewriting absolute paths to ${BASE_PATH}"
DOCS_BASE_PATH="$BASE_PATH" DOCS_OUT_DIR="$OUT_DIR" python3 - <<'PY'
import os, pathlib, re

base = os.environ["DOCS_BASE_PATH"]
root = pathlib.Path(os.environ["DOCS_OUT_DIR"])
extensions = {".html", ".js", ".css", ".json", ".txt", ".xml"}
rewritten = 0

# Anchored on a quote, paren or equals so only a path in a reference position is touched — a bare
# slash inside prose or a regex literal is left alone.
patterns = [
    (re.compile(r'(["\'(=])/(_next/)'), rf'\1{base}/\2'),
    (re.compile(r'(["\'(=])/(favicons?/)'), rf'\1{base}/\2'),
    (re.compile(r'(["\'(=])/(images?/)'), rf'\1{base}/\2'),
    (re.compile(rf'(href=")/(?!{base.lstrip("/")}/)([a-z0-9][^"]*)"'), rf'\1{base}/\2"'),
    (re.compile(r'(href=")/"'), rf'\1{base}/"'),
]

for path in root.rglob("*"):
    if not path.is_file() or path.suffix.lower() not in extensions:
        continue
    try:
        text = path.read_text(encoding="utf-8")
    except (UnicodeDecodeError, OSError):
        continue
    original = text
    for pattern, replacement in patterns:
        text = pattern.sub(replacement, text)
    if text != original:
        path.write_text(text, encoding="utf-8")
        rewritten += 1

print(f"rewrote {rewritten} files")
PY

echo "==> done: $OUT_DIR ($(du -sh "$OUT_DIR" | cut -f1))"
