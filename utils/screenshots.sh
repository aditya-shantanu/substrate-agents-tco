#!/usr/bin/env bash
# Regenerates docs/screenshots/*.svg from the tco TUI (no cluster needed).
# Pattern borrowed from substrate-gke: uishots writes ANSI captures, freeze
# renders them. Needs freeze: go install github.com/charmbracelet/freeze@latest
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v freeze >/dev/null || {
  echo "freeze not found: go install github.com/charmbracelet/freeze@latest" >&2
  exit 1
}

cd "${ROOT}/service" && go run ./cmd/tco screenshots "${ROOT}/docs/screenshots"
for f in "${ROOT}"/docs/screenshots/*.ans; do
  freeze --execute "cat $f" --window -o "${f%.ans}.svg" </dev/null
  rm "$f"
  echo "rendered ${f%.ans}.svg"
done
