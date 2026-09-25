#!/usr/bin/env bash
# Builds and pushes the autosuspender + agentsim images with ko (no Docker
# daemon needed; the dashboard is embedded in the binary via go:embed).
# The Dockerfile remains for docker-preferring environments.
#
# Usage: KO_DOCKER_REPO=gcr.io/<project>/ate-images ./build.sh
# Prints AUTOSUSPENDER_IMAGE / AGENTSIM_IMAGE exports for envsubst on
# manifests/agent-sim.yaml.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
: "${KO_DOCKER_REPO:?set KO_DOCKER_REPO, e.g. gcr.io/<project>/ate-images}"
export KO_DEFAULTPLATFORMS="${KO_DEFAULTPLATFORMS:-linux/amd64}"

cd "${SCRIPT_DIR}"
# --base-import-paths names images by the binary's directory (autosuspender,
# agentsim) instead of an md5 hash.
AUTOSUSPENDER_IMAGE=$(ko build --base-import-paths ./cmd/autosuspender)
AGENTSIM_IMAGE=$(ko build --base-import-paths ./cmd/agentsim)

echo
echo "export AUTOSUSPENDER_IMAGE=${AUTOSUSPENDER_IMAGE}"
echo "export AGENTSIM_IMAGE=${AGENTSIM_IMAGE}"
