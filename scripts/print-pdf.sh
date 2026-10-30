#!/bin/sh
# print-pdf.sh HTML PDF — prints the manual to a PDF with a headless Chrome or Chromium, when one is installed.
# The manual's stylesheet has print rules, so the PDF and the HTML cannot disagree about layout.
#
# Some Chrome builds write the PDF and then keep running (crash reporter, background networking). The script
# therefore runs Chrome with a throwaway profile, waits for the PDF to appear and stop growing, and then stops it.
set -eu
html=$1 pdf=$2
url="file://$(cd "$(dirname "$html")" && pwd)/$(basename "$html")"
# chrome-headless-shell is preferred: full Chrome's headless mode can stall before printing on some Linux hosts.
for c in "${CHROME:-}" "$(command -v chrome-headless-shell 2>/dev/null || true)" "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" /opt/homebrew/bin/chromium \
         /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/google-chrome; do
  if [ -n "$c" ] && [ -x "$c" ]; then
    profile=$(mktemp -d)
    tmp="$pdf.tmp"
    rm -f "$tmp"
    "$c" --headless --no-sandbox --disable-gpu --no-first-run --no-default-browser-check --disable-extensions \
      --disable-background-networking --disable-crash-reporter --user-data-dir="$profile" \
      --no-pdf-header-footer --print-to-pdf="$tmp" "$url" >/dev/null 2>&1 &
    pid=$!
    last=-1 i=0
    while [ $i -lt 120 ]; do
      if ! kill -0 "$pid" 2>/dev/null; then break; fi
      if [ -s "$tmp" ]; then
        size=$(wc -c < "$tmp")
        if [ "$size" = "$last" ]; then break; fi
        last=$size
      fi
      sleep 1
      i=$((i + 1))
    done
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    rm -rf "$profile"
    if [ -s "$tmp" ] && [ "$(head -c 5 "$tmp")" = "%PDF-" ]; then
      mv "$tmp" "$pdf"
      echo "manual: wrote $pdf"
      exit 0
    fi
    rm -f "$tmp"
    echo "manual: Chrome did not write a PDF" >&2
    exit 1
  fi
done
echo "manual: no headless Chrome found; skipping the PDF (set CHROME=/path/to/chrome)"
