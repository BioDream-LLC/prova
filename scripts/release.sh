#!/usr/bin/env bash
# scripts/release.sh — build prova release bundles.
#
# Produces, under dist/release/, one bundle per platform containing the single prova binary (with the
# web interface and manual embedded) plus, for the host platform, the prova-proof companion. The Go
# binary cross-compiles cleanly; the C++ companion is built per-platform (here, for the build host),
# so the full matrix is produced by CI (see .github/workflows/release.yml), which builds the
# companion natively on each OS and links CaDiCaL.
#
# Usage: scripts/release.sh [version]   (default: `git describe` or v0.0.0)
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo v0.0.0)}"
OUT="dist/release"
LDFLAGS="-s -w -X main.version=${VERSION}"

HOST_OS="$(go env GOOS)"
HOST_ARCH="$(go env GOARCH)"

echo "prova release ${VERSION} (host ${HOST_OS}/${HOST_ARCH})"
rm -rf "${OUT}"
mkdir -p "${OUT}"

# Build the web interface and the manual once; both are embedded / shipped.
if command -v pnpm >/dev/null 2>&1; then
  echo "==> web"; (cd web && pnpm install --frozen-lockfile && pnpm build)
  echo "==> manual"; make manual || echo "   (manual skipped)"
fi

# Build the companion for the host platform.
echo "==> prova-proof (host)"; make proof

PLATFORMS=("linux/amd64" "linux/arm64" "darwin/arm64" "darwin/amd64" "windows/amd64")
for p in "${PLATFORMS[@]}"; do
  os="${p%/*}"; arch="${p#*/}"
  name="prova_${VERSION}_${os}_${arch}"
  dir="${OUT}/${name}"
  mkdir -p "${dir}"
  bin="prova"; [ "${os}" = "windows" ] && bin="prova.exe"
  echo "==> ${os}/${arch}"
  GOOS="${os}" GOARCH="${arch}" CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS}" -o "${dir}/${bin}" ./cmd/prova

  # The companion is native code: ship it only in the bundle matching the build host.
  if [ "${os}" = "${HOST_OS}" ] && [ "${arch}" = "${HOST_ARCH}" ] && [ -f bin/prova-proof ]; then
    cp bin/prova-proof "${dir}/prova-proof"
  else
    echo "prova-proof (the optional SAT companion) is native per-platform; build it on ${os}/${arch} with 'make proof', or use the CI release which builds it for every platform. prova runs and certifies verdicts in-process without it." > "${dir}/PROVA-PROOF.txt"
  fi

  cp LICENSE.md NOTICE README.md "${dir}/"; cp -r licenses "${dir}/"
  [ -f docs/manual/prova-manual.html ] && { mkdir -p "${dir}/docs"; cp docs/manual/prova-manual.html "${dir}/docs/"; }
  [ -f docs/manual/prova-manual.pdf ] && cp docs/manual/prova-manual.pdf "${dir}/docs/"

  # Archive.
  ( cd "${OUT}" && if [ "${os}" = "windows" ]; then zip -qr "${name}.zip" "${name}"; else tar -czf "${name}.tar.gz" "${name}"; fi )
done

echo
echo "release bundles in ${OUT}:"
ls -1 "${OUT}" | sed 's/^/  /'
