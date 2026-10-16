#!/bin/sh
# print-pdf.sh HTML PDF — prints the manual to a PDF with a headless Chrome or Chromium, when one is installed.
# The manual's stylesheet has print rules, so the PDF and the HTML cannot disagree about layout.
set -eu
html=$1 pdf=$2
for c in "${CHROME:-}" "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" /opt/homebrew/bin/chromium \
         /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/google-chrome; do
  if [ -n "$c" ] && [ -x "$c" ]; then
    "$c" --headless --no-sandbox --disable-gpu --no-pdf-header-footer --print-to-pdf="$pdf" "file://$(cd "$(dirname "$html")" && pwd)/$(basename "$html")" >/dev/null 2>&1
    echo "manual: wrote $pdf"
    exit 0
  fi
done
echo "manual: no headless Chrome found; skipping the PDF (set CHROME=/path/to/chrome)"
