# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

EVMI is a Go service that indexes EVM (Ethereum-compatible) contract logs by polling
JSON-RPC endpoints, decodes them against contract ABIs, and writes logs + transactions
into a pluggable log store (currently ClickHouse). All indexing topology
(blockchains, ABIs, stores, pipelines, sources) lives in a relational metadata database
(GORM: SQLite/Postgres/MySQL) and is administered at runtime over a Connect/gRPC API.

## Commands

```bash
# Build the binary (module has a single entrypoint under cmd/)
cd cmd/evm-indexer && go build

# Run the indexer + gRPC server
go run ./cmd/evm-indexer start --config <config.json> --instance <INSTANCE_ID>
# flags: -c/--config (env CONFIG_FILE_PATH), -i/--instance (env EVMI_INSTANCE_ID)

# Regenerate gRPC/Connect stubs after editing internal/grpc/proto/**/*.proto
buf generate           # writes to internal/grpc/generated (config in buf.gen.yaml)
buf lint               # STANDARD lint ruleset (buf.yaml)

# Full local stack (indexer + ClickHouse + ch-ui + Prometheus + Grafana)
docker compose up --build

go test ./...          # no tests currently exist in the repo
```

The gRPC server always listens on `0.0.0.0:8080` (hardcoded in `internal/grpc/server.go`),
served as HTTP/2 cleartext (h2c) with permissive CORS.

## Architecture

The process boots in `cmd/evm-indexer/main.go` in this order: parse config JSON → init
event bus → start metrics → open metadata DB (auto-migrates GORM models) → upsert this
`EvmiInstance` row (marks it `RUNNING`, records local IP) → start the indexer supervisor
→ start the gRPC server (blocking).

**Metadata DB vs. log store — two separate databases.** Do not conflate them:
- **Metadata DB** (`internal/database/evmi-database`): GORM-managed relational store holding
  the indexing configuration and progress. Models in `models.go`, opened/migrated in
  `database.go`. Entity graph: `EvmiInstance` → `EvmLogPipeline` → `EvmLogSource`, with
  `EvmBlockchain`, `EvmJsonAbi`, and `EvmLogStore` referenced by pipelines/sources.
  `EvmLogSource.SyncBlock` is the per-source cursor persisted after every batch.
- **Log store** (`internal/database/log-stores`): where decoded logs/transactions land.
  Pluggable behind the `EvmIndexerStorage` interface (`interface.go`); `LoadStore` dispatches
  on `storeType`. Only `clickhouse` is implemented. A store is selected per-`EvmLogStore` row
  via its `StoreType` + JSON `StoreConfig`, so different pipelines can target different stores.

**Indexing runtime** (`internal/indexer`): `IndexerService` (`service.go`) loads all
pipelines/sources for this instance and, for each *enabled* source, adds a
`SourceIndexerService` to a `thejerf/suture` supervisor (auto-restart on crash — note the
code frequently `logger.Fatal()`s on RPC errors, which exits the process rather than
returning an error to the supervisor). Each `SourceIndexerService` (`indexer.go`) runs an
independent poll loop: `eth_blockNumber` → `eth_getLogs` in `BlockRange`-sized windows →
batch-fetch transactions (`RpcMaxBatchSize` per batch) → decode → insert → advance
`SyncBlock` → emit `logs.new`. Sources can be enabled/disabled live via bus events
(`source.enable`/`source.disable`) emitted from the gRPC handlers.

Source **types** (`LogSourceType` in `models.go`) select the poll strategy in `Serve`:
- `CONTRACT` / `FACTORY` → `serveStaticIndexation` (filter by contract address)
- `TOPIC` → `serveTopicIndexation` (filter by topic0 + optional topic filters)
- `FULL` → `serveFullIndexation` (all logs, no ABI decode)

`FACTORY` additionally watches for a creation event and emits `logs.newFactoryItem` to spawn
a child `CONTRACT` source for each newly deployed contract.

ABI decoding lives in `SourceIndexerService.GetLogMetadata`: it matches `log.Topics[0]`
against the ABI's events and unpacks indexed topics + data into a `map[string]string`. It
handles Solidity types via an explicit `reflect`-based type switch and **panics on any
unmapped type** — when adding support for a new argument type, extend that switch.

**Event bus** (`internal/bus`): thin wrapper over `mustafaturan/bus/v3` with a monotonic ID
generator. Topics are declared as constants in `bus.go` (`logs.new`, `source.enable`,
`source.disable`, `signal.shutwown`). This is the decoupling seam between the gRPC control
plane and the indexing workers.

**gRPC/Connect API** (`internal/grpc`): one Connect service, `EvmIndexerService`, defined in
`internal/grpc/proto/evm_indexer/v1/evm_indexer.proto` and implemented across
`*-handlers.go` files (one per entity: blockchain, abi, store, pipeline, source, logs,
instance). Handlers are mostly CRUD over the metadata DB; source start/stop handlers emit bus
events rather than mutating workers directly. Generated code under
`internal/grpc/generated/` is checked in — regenerate with `buf generate`, don't hand-edit.
**Caveat:** the committed generated code was produced with `protoc-gen-go v1.35.1`; if the
locally-installed generator differs, `buf generate` rewrites the whole file with the other
version (verify `protoc-gen-go --version` before regenerating).

**Auth** (`internal/auth`): bearer-token authentication over the API. Every Connect RPC is
gated by `Authenticator.Interceptor(publicProcedures...)` (wired in `server.go`), which
validates an `Authorization: Bearer <token>` header and injects the `*User` into context
(`auth.UserFromContext`) — except the public procedures `Login` and `GetOAuthLoginUrl`, passed
by their generated procedure constants. Tokens are opaque, SHA-256-hashed in the DB
(`AccessToken`); passwords are bcrypt (`User`). Auth *operations* are Connect RPCs implemented
in `internal/grpc/auth-handlers.go` (Login, Me, CreateAccessToken/List/Revoke, Get/Update
OAuthConfig, GetOAuthLoginUrl) delegating to the `Authenticator`. The **only** HTTP endpoint is
`GET /auth/oauth/callback` (`auth.RegisterRoutes`) — a browser redirect target that can't be an
RPC; OAuth CSRF uses a stateless HMAC-signed `state` (secret in `OAuthConfig.StateSecret`), not
a cookie. A default `admin`/`admin` user is seeded by `LoadDatabase` when the users table is
empty. Full surface + flows in `AUTH.md`. Note: auth RPCs live in the shared proto, so
regenerating requires `protoc-gen-go v1.35.1` (see the codegen caveat above).

**Exporters** (`internal/exporter`, `pkg/exporter`): the plugin subsystem. Mirrors the
indexer's manager/supervisor shape — `ExporterServiceManager` starts one `ExporterService`
per enabled `EvmiExporter` (bound to a pipeline), reacting to `exporter.enable`/
`exporter.disable` bus events. Each service streams the pipeline's stored logs, in
`(block_number, log_index)` order, into a user-written native Go plugin's `NewLogEvent`,
committing `EvmiExporter.SyncBlock` at block boundaries (at-least-once; plugins dedupe on the
stable `LogEvent.Id`). The safe export head is the *min* `SyncBlock` across the pipeline's
sources. Plugins implement the public `pkg/exporter.Exporter` interface (public so external
plugin repos can import it — `internal/` can't be imported out-of-module) and export a
`func New() exporter.Exporter` symbol; `loader.go` builds the `.so` with `-buildmode=plugin`
and loads it via `plugin.Open`. Wired into `main.go` after the indexer. **Design + native-
plugin caveats (CGO, toolchain/version match, no isolation, scratch-image incompatibility)
live in `docs/exporters.md` — read it before touching this subsystem.** Cross-source ordered
reads use `EvmIndexerStorage.GetLogsForSources`.

**Metrics** (`internal/metrics`, Prometheus): live and scraped per the compose Prometheus
config.

## Config file vs. runtime topology

The config file is deliberately tiny. The Go `Config` struct (`internal/types/config.go`)
only unmarshals two keys:
- `database` — `type` = `SQLITE`/`POSTGRES`/`MYSQL`, plus a `config` string map. SQLite reads
  `config.filename`; Postgres/MySQL read `config.dsn`.
- `metrics` — `enabled` / `path` / `port`.

Everything else — blockchains, ABIs, log stores, pipelines, sources — lives in the metadata
DB and is created through the gRPC API at runtime, **not** in the config file. Accurate
examples: `configs/exemple.config.json` (SQLite), `configs/exemple-postgres.config.json`
(Postgres), `docker/ambiant.config.json` (the compose stack).

Note the field is `config` (a `map[string]string`), so `dsn`/`filename` must be nested
under `database.config`, not at the top of `database`. `cmd/evm-indexer/staging.config.json`
places `dsn` at the wrong level (top of `database`) — it only works because it uses SQLite
and `filename` is correctly nested.

**Log store config lives in the DB, not the config file.** A `clickhouse` `EvmLogStore` row's
JSON `StoreConfig` (read in `clickhouse/store.go:Init`) expects: `addr` (comma-separated for
multiple nodes), `database`, `username`, `password`, `logsTableName`, `transactionsTableName`.
Set these when creating the store over the gRPC API.
