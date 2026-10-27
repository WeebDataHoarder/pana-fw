#!/bin/bash

set -eu
cd "$(dirname "$0")/.."
out=${1:-web/dist}
repo=${REPO_URL:-https://git.gammaspectra.live/WeebDataHoarder/pana-fw}
mkdir -p "$out"

GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)" CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm \
        go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -v \
        -o "$out/repack-firmware.wasm" ./cmd/repack-firmware


commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)
date=$(git log -1 --format=%cs 2>/dev/null || echo unknown)
short=$(echo "$commit" | cut -c1-12)
git diff --quiet HEAD 2>/dev/null || short="$short-dirty"
sed -e "s|@REPO@|$repo|g" -e "s|@COMMIT@|$commit|g" -e "s|@COMMIT_SHORT@|$short|g" -e "s|@COMMIT_DATE@|$date|g" web/index.html > "$out/index.html"
sed -e "s|@REPO@|$repo|g" -e "s|@COMMIT@|$commit|g" -e "s|@COMMIT_SHORT@|$short|g" -e "s|@COMMIT_DATE@|$date|g" web/index.js > "$out/index.js"
cp web/worker.js "$out/"

if [ ! -d "$out/presets" ]; then
  mkdir "$out/presets"
fi

for f in patch-presets/*; do
  if [ -d "${f}" ]; then
    presetName=$(basename "${f}")
    pushd "${f}"
    zip -r -9 -o "../../$out/presets/${presetName}.zip" .
    popd
  else
    cp -v "${f}" "$out/presets"
  fi
done