# --- web UI build stage -----------------------------------------------------
# Builds the Next.js app to a static export in /webui/out.
FROM node:20-alpine AS webui
WORKDIR /webui
COPY webui/package.json webui/package-lock.json* ./
RUN npm install
COPY webui/ ./
RUN npm run build


# --- go build stage ---------------------------------------------------------
# CGO is required: the SQLite driver (mattn/go-sqlite3) and go-ethereum's crypto
# use cgo, so this builds on Debian with gcc and links against glibc. The image
# tag must satisfy the toolchain in go.mod (go 1.23).
FROM golang:1.24-bookworm AS builder
ENV CGO_ENABLED=1
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# A single binary provides BOTH roles as subcommands: `evm-indexer start` runs a
# stateful indexer instance, `evm-indexer gateway` runs the routing load balancer
# (internal/gateway). The Helm chart selects the role per workload via `args`.
RUN go build -ldflags="-s -w" -o /evm-indexer ./cmd/evm-indexer


# --- final image ------------------------------------------------------------
# distroless/cc provides glibc, libgcc and ca-certificates — enough to run the
# CGO binary. (scratch cannot: the binary is dynamically linked.)
FROM gcr.io/distroless/cc-debian12
COPY --from=builder /evm-indexer /evm-indexer
# The built web UI is served from EVMI_WEBUI_DIR (see internal/grpc/webui.go).
COPY --from=webui /webui/out /public
ENV EVMI_WEBUI_DIR=/public
# No CMD: the deployment supplies the subcommand — ["start", ...] for an instance
# or ["gateway", ...] for the gateway.
ENTRYPOINT ["/evm-indexer"]
