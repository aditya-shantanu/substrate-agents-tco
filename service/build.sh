#!/usr/bin/env bash
# Builds and pushes the agents-tco image (autosuspender + agentsim).
# The Go module `replace`s the sibling substrate checkout, so the docker
# context is staged with both repos in the layout the Dockerfile expects.
#
# Usage: PROJECT_ID=my-project [SUBSTRATE_REPO=~/repos/substrate] ./build.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBSTRATE_REPO="${SUBSTRATE_REPO:-${SCRIPT_DIR}/../../substrate}"
: "${PROJECT_ID:?set PROJECT_ID}"
IMAGE="${IMAGE:-us-docker.pkg.dev/${PROJECT_ID}/gcr.io/ate-images/agents-tco:latest}"

CTX="$(mktemp -d)"
trap 'rm -rf "${CTX}"' EXIT
echo "staging build context in ${CTX}"
rsync -a --exclude .git "${SUBSTRATE_REPO}/" "${CTX}/substrate/"
rsync -a --exclude .git "${SCRIPT_DIR}/" "${CTX}/service/"

docker build --platform=linux/amd64 -f "${SCRIPT_DIR}/Dockerfile" -t "${IMAGE}" "${CTX}"
docker push "${IMAGE}"
echo "pushed ${IMAGE}"
