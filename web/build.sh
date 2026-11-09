#!/bin/bash

set -eu
cd "$(dirname "$0")/.."
out=${1:-web/dist}
repo=${REPO_URL:-https://git.gammaspectra.live/WeebDataHoarder/pana-fw}
mkdir -p "$out"

GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)" CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm \
        go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -v \
        -o "$out/repack-firmware.wasm" ./cmd/repack-firmware

if [ "${USE_TINYGO-0}" == "1" ]; then
  GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)" CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm \
          tinygo build -trimpath -buildvcs=false -opt=2 -no-debug \
          -o "$out/repack-firmware.wasm" ./cmd/repack-firmware
fi

commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)
date=$(git log -1 --format=%cs 2>/dev/null || echo unknown)
short=$(echo "$commit" | cut -c1-12)
git diff --quiet HEAD 2>/dev/null || short="$short-dirty"
sed -e "s|@REPO@|$repo|g" -e "s|@COMMIT@|$commit|g" -e "s|@COMMIT_SHORT@|$short|g" -e "s|@COMMIT_DATE@|$date|g" web/index.html > "$out/index.html"
sed -e "s|@REPO@|$repo|g" -e "s|@COMMIT@|$commit|g" -e "s|@COMMIT_SHORT@|$short|g" -e "s|@COMMIT_DATE@|$date|g" web/index.js > "$out/index.js"
cp web/worker.js "$out/"

if [ ! -d "$out/presets" ]; then
  mkdir "$out/presets"
else
  rm -r "$out/presets/"*
fi

# Compile the ELF-patch stub sources
for mk in patch-presets/*/src/Makefile; do
  [ -e "$mk" ] || continue
  echo "building stubs in $(dirname "$mk")"
  make -C "$(dirname "$mk")"
done

for f in patch-presets/*; do
  presetName=$(basename "${f}")
  case "$presetName" in *-common) continue;; esac
  if [ -d "${f}" ]; then
    zipPath="$(cd "$out/presets" && pwd)/${presetName}.zip"
    pushd "${f}"
    if [ -f .include ]; then
      while IFS= read -r inc; do
        [ -n "$inc" ] || continue
        (cd "../$inc" && zip -r -9 -o -X "$zipPath" . -x 'src/*.[ch]' 'src/Makefile' 'src/.gitignore')
      done < .include
    fi
    zip -r -9 -o -X "$zipPath" . -x '.include'
    popd
  else
    cp -v "${f}" "$out/presets"
  fi
done