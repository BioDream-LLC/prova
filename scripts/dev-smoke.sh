#!/bin/sh
# Dev smoke test: vet, build, run, and probe the server. Run on the build host.
export PATH="/opt/homebrew/bin:$PATH"
cd "$(dirname "$0")/.." || exit 1

echo "--- go vet"
go vet ./... && echo "VET_OK"

echo "--- build"
make build && echo "BUILD_OK"

echo "--- run + probe"
./bin/prova -addr :8099 &
pid=$!
sleep 1
printf 'version: '; curl -sk https://localhost:8099/api/version; echo
printf 'health:  '; curl -sk https://localhost:8099/api/health; echo
printf 'root:    '; curl -sk https://localhost:8099/ | head -1
kill "$pid" 2>/dev/null

echo "--- binary size"
ls -lh bin/prova | awk '{print $5}'
