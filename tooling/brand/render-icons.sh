#!/usr/bin/env bash
# Renders the raster icons and the email logo from the brand's vector sources in this directory, which are the source of truth: edit an SVG here, run this, and commit what it writes. Needs rsvg-convert (librsvg) and ImageMagick 7 (magick).
#
#   tooling/brand/render-icons.sh
#
# What it writes, and from what:
#   apps/web/public/favicon.ico                      16, 32 and 48 px, 16 px from favicon-16.svg and 32/48 px from favicon.svg: white bars and one white selected dot on the crimson tile, drawn on the 16 and 32 px pixel grids so nothing lands on a half pixel (the detailed mark with rings turned to mush at tab size)
#   apps/web/public/favicon/favicon-{16x16,32x32}.png from favicon-16.svg and favicon.svg
#   apps/web/public/favicon/apple-touch-icon.png     180 px, from app-icon-crimson.svg on a full crimson square: iOS rounds the corners itself and fills transparent ones with black
#   apps/web/public/favicon/android-chrome-{192x192,512x512}.png from app-icon-crimson.svg
#   apps/web/public/favicon/safari-pinned-tab.svg    symbol.svg in one colour, the mask Safari tints
#   apps/web/public/logo-transparent.png             lockup.svg at twice its size, 424 × 96 with no added margin: every email embeds this file at a fixed width (212 × 48 from apps/server/internal/platform/mailer/templates/layout.html, and the 240 px of emails already sent), so its name stays, and a padded or square canvas would only push the card down by its empty margin
#   apps/website/public/_site/icons/                 the same favicon-32x32.png and apple-touch-icon.png
set -euo pipefail

brand="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${brand}/../.." && pwd)"
web="${root}/apps/web/public"
site="${root}/apps/website/public/_site/icons"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

png() { rsvg-convert --width "$2" --height "$3" --keep-aspect-ratio "$1" -o "$4"; }
# strip <in> <out> [operations...]: applies the operations and drops the timestamps and text chunks ImageMagick would otherwise write, so an unchanged source renders to identical bytes.
strip() {
  local in=$1 out=$2
  shift 2
  magick "${in}" "$@" -strip -define png:exclude-chunks=date,time "${out}"
}

png "${brand}/favicon-16.svg" 16 16 "${tmp}/favicon-16.png"
png "${brand}/favicon.svg" 32 32 "${tmp}/favicon-32.png"
png "${brand}/favicon.svg" 48 48 "${tmp}/favicon-48.png"
strip "${tmp}/favicon-16.png" "${web}/favicon/favicon-16x16.png"
strip "${tmp}/favicon-32.png" "${web}/favicon/favicon-32x32.png"
magick "${tmp}/favicon-16.png" "${tmp}/favicon-32.png" "${tmp}/favicon-48.png" "${web}/favicon.ico"

png "${brand}/app-icon-crimson.svg" 180 180 "${tmp}/apple.png"
strip "${tmp}/apple.png" "${web}/favicon/apple-touch-icon.png" -background "#D3264A" -alpha remove -alpha off
png "${brand}/app-icon-crimson.svg" 192 192 "${tmp}/android-192.png"
png "${brand}/app-icon-crimson.svg" 512 512 "${tmp}/android-512.png"
strip "${tmp}/android-192.png" "${web}/favicon/android-chrome-192x192.png"
strip "${tmp}/android-512.png" "${web}/favicon/android-chrome-512x512.png"

sed -e 's/#0F172A/#000000/g' -e 's/#D3264A/#000000/g' "${brand}/symbol.svg" >"${web}/favicon/safari-pinned-tab.svg"

png "${brand}/lockup.svg" 424 96 "${tmp}/lockup.png"
strip "${tmp}/lockup.png" "${web}/logo-transparent.png"

cp "${web}/favicon/favicon-32x32.png" "${site}/favicon-32x32.png"
cp "${web}/favicon/apple-touch-icon.png" "${site}/apple-touch-icon.png"
