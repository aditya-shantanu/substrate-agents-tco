#!/usr/bin/env bash
# Regenerates assets/screenshots/*.svg from the tco TUI (no cluster needed).
# Pattern borrowed from substrate-gke: uishots writes ANSI captures, freeze
# renders them. Needs freeze: go install github.com/charmbracelet/freeze@latest
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v freeze >/dev/null || {
  echo "freeze not found: go install github.com/charmbracelet/freeze@latest" >&2
  exit 1
}

cd "${ROOT}/service" && go run ./cmd/tco screenshots "${ROOT}/assets/screenshots"
for f in "${ROOT}"/assets/screenshots/*.ans; do
  freeze --execute "cat $f" --window -o "${f%.ans}.svg" </dev/null
  rm "$f"
  echo "rendered ${f%.ans}.svg"
done

# Also capture the Phase 1 calculator (calculator.html) with headless Chrome
# when available.
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
command -v google-chrome >/dev/null && CHROME=google-chrome
if [[ -x "${CHROME}" || "$(command -v "${CHROME}" 2>/dev/null)" ]]; then
  "${CHROME}" --headless=new --disable-gpu --user-data-dir=/tmp/chrome-shots \
    --window-size=1440,1120 --screenshot="${ROOT}/assets/screenshots/calculator.png" \
    "file://${ROOT}/calculator.html" 2>/dev/null
  echo "rendered ${ROOT}/assets/screenshots/calculator.png"
fi
