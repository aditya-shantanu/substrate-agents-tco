# Build context must contain BOTH repos (see build.sh):
#   ctx/substrate/            — the agent-substrate checkout (replace target)
#   ctx/service/              — this module
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY substrate/ substrate/
COPY service/ substrate-agents-tco/service/
# go.mod replaces ../../substrate, so keep the relative layout intact.
WORKDIR /src/substrate-agents-tco/service
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/autosuspender ./cmd/autosuspender && \
    CGO_ENABLED=0 go build -o /out/agentsim ./cmd/agentsim

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
