# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

EVMI is a Go service that indexes EVM (Ethereum-compatible) contract logs by polling
JSON-RPC endpoints, decodes them against contract ABIs, and writes logs + transactions
into a pluggable log store (ClickHouse, Parquet files, or Elasticsearch). All indexing topology
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
  on `storeType`. Implemented backends: `clickhouse`, `parquet` (files on disk, partitioned per
  source, read-and-filter queries), `elasticsearch` (bulk-indexed docs), `postgres`/`mysql` (one
  shared GORM store in `.../sql`, dialect-selected), and `mongodb` (upserted docs). A store is
  selected per-`EvmLogStore` row via its `StoreType` + JSON `StoreConfig`, so different pipelines
  can target different stores. The Parquet and SQL stores have self-contained unit tests (SQL via
  SQLite); the Elasticsearch and MongoDB stores have integration tests gated behind
  `ELASTICSEARCH_URL` / `MONGODB_URI`.

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

`FACTORY` additionally watches for a creation event (matched by `EventName` against the
source's `FactoryCreationFunctionName`), reads the new contract address from the decoded event
args, and calls `SourceIndexerService.registerFactoryChild` **inline, before the cursor
advances**. That method **creates a new enabled `CONTRACT` child source** (uniqueness is per
`(ParentSourceID, Address)`, so re-seeing the same deployment is a no-op) and starts it
best-effort by emitting `source.enable`. If child creation fails it returns the error, which
propagates out of `serve*Indexation` → `Serve`; suture then restarts the range (SyncBlock was
not advanced) so the factory blocks and retries rather than skipping the deployment. The child
is created enabled, so it also starts on the next manager boot even if the enable emit was
missed. The manager's service maps are mutex-guarded since enable/disable and startup can fire
concurrently.

ABI decoding lives in `SourceIndexerService.GetLogMetadata`: it matches `log.Topics[0]`
against the ABI's events and unpacks indexed topics + data into a `map[string]string`. It
handles Solidity types via an explicit `reflect`-based type switch and **panics on any
unmapped type** — when adding support for a new argument type, extend that switch.

**Event bus** (`internal/bus`): thin wrapper over `mustafaturan/bus/v3` with a monotonic ID
generator. Topics are declared as constants in `bus.go` (`logs.new`, `source.update`,
`source.enable`, `source.disable`, `exporter.enable`/`disable`, `signal.shutwown`). This is the
decoupling seam between the gRPC control plane and the indexing workers. `source.update` /
`exporter.update` carry the updated `EvmLogSource` / `EvmiExporter` (emitted by
`SourceIndexerService.emitSourceUpdate` and `ExporterService.emitUpdate` on every sync-block
advance and status change) and are relayed to clients by the `StreamEvmLogSourceUpdates` /
`StreamEvmiExporterUpdates` server-streaming RPCs (`stream-handlers.go`): each stream registers a
bus handler for its lifetime, forwards events through a buffered channel (non-blocking send —
drops if the client lags, since the next update supersedes it), and optionally filters by
`pipeline_id`. The web UI consumes both (generic `stream` capability on a resource → the
`ResourceManager` merges updates by id, with a **● live** badge). Note bus `Matcher` is a regex
matched against topic names. **Because this added a streaming RPC, `Authenticator.Interceptor`
now implements the full `connect.Interceptor` (WrapUnary + WrapStreamingHandler) so streams are
authenticated too — a plain `UnaryInterceptorFunc` would leave streams open.**

**gRPC/Connect API** (`internal/grpc`): one Connect service, `EvmIndexerService`, defined in
`internal/grpc/proto/evm_indexer/v1/evm_indexer.proto` and implemented across
`*-handlers.go` files (one per entity: blockchain, abi, store, pipeline, source, logs,
instance). Handlers are mostly CRUD over the metadata DB; source start/stop handlers emit bus
events rather than mutating workers directly. Generated code under
`internal/grpc/generated/` is checked in — regenerate with `buf generate`, don't hand-edit.
**Caveat:** the committed generated code was produced with `protoc-gen-go v1.35.1`; if the
locally-installed generator differs, `buf generate` rewrites the whole file with the other
version (verify `protoc-gen-go --version` before regenerating).

**Gateway** (`internal/gateway`): a stateful load balancer for a multi-instance fleet. Because
instances are stateful (each indexes specific pipelines and may keep their logs in a *local* store
like parquet), a plain L4/L7 balancer can't be used — a request must reach the instance that owns
the data. The gateway **replicates the whole Connect service** (`var _ ...ServiceHandler = (*Gateway)`)
and, per request, resolves the owning instance and forwards the call there. Started via the
`evm-indexer gateway` subcommand (flags `--config` for the shared metadata DB, `--port`,
`--cache-ttl`). Ownership comes from the entity graph: pipeline→instance (`EvmLogPipeline.EvmiInstanceID`),
source→pipeline→instance, exporter→pipeline→instance; the forward target is the instance's
`EvmiInstance.IpV4:Port` (the **`Port` column** on `EvmiInstance`, set from `grpc.ServerPort` at
boot — added for exactly this). `resolver.go` caches every hop in TTL `ttlCache`s (default 30s) so
routing doesn't hammer the DB; `pool.go` memoizes one h2c Connect client per backend. Routing
policy (`handlers_gen.go`, generated — don't hand-edit; regenerate the same way it was created):
data/control RPCs (logs reads, source/exporter CRUD + start/stop, keyed by `source_id`/`id`/
`pipeline_id`) go to the **owning** instance; instance-agnostic shared-metadata RPCs (blockchains,
ABIs, stores, auth, users, plugins, `List*`) go to **any RUNNING** instance round-robin. The two
server-streams route by `pipeline_id`, or **fan out and merge across the whole fleet** when
`pipeline_id == 0` (`gateway.go`). Auth is pass-through — the caller's bearer token is forwarded and
each instance validates it against the shared DB. Non-API paths (web UI at `/`, `/auth/oauth/callback`)
are reverse-proxied to any instance (`server.go`). **`InstallPlugin` is the one exception to
single-instance routing:** it is hand-written in `gateway.go` (not in `handlers_gen.go`) to **fan
out to every RUNNING instance concurrently**, because installing builds the plugin `.so` on that
instance's *local* disk — so every instance that might run an exporter using the plugin needs its
own build. The aggregate (via `aggregateInstall`) succeeds only if all instances succeed; otherwise
the response error names each failing instance.

**Deployment (Helm, `charts/evmi`)**: one image (`Dockerfile`), one binary, two roles selected by
`args`. Instances run as a **StatefulSet** (`evm-indexer start -i $(POD_NAME)`) — stable pod names
mean a pod always maps to the same `EvmiInstance` row (and its pipeline assignments) across
restarts, and each pod gets a PVC (`volumeClaimTemplates`) for local parquet stores / plugin build
cache. The gateway runs as a **Deployment** (`evm-indexer gateway`). Both mount the *same* config
from a Secret rendered from `.Values.config` (`toPrettyJson`) — so the shared metadata DB must be
POSTGRES/MYSQL, never SQLITE. Each instance registers its **pod IP + 8080** in the DB on boot; the
gateway (in-cluster) forwards straight to pod IPs and re-resolves after the cache TTL when a pod
restarts with a new IP. `helm lint charts/evmi` and `helm template` validate it; optional
Ingress + PodMonitor behind values flags.

**Auth** (`internal/auth`): bearer-token authentication over the API. Every Connect RPC is
gated by `Authenticator.Interceptor(publicProcedures...)` (wired in `server.go`), which
validates an `Authorization: Bearer <token>` header and injects the `*User` into context
(`auth.UserFromContext`) — except the public procedures `Login` and `ListOAuthLoginUrls`, passed
by their generated procedure constants. Tokens are opaque, SHA-256-hashed in the DB
(`AccessToken`); passwords are bcrypt (`User`). Auth *operations* are Connect RPCs implemented
in `internal/grpc/auth-handlers.go` (Login, Me, access-token CRUD, **OAuth provider CRUD** and
**user CRUD** — both admin-only via `requireAdmin`) delegating to the `Authenticator`.
**Multiple OAuth providers** are supported (`OAuthProvider` rows, `internal/auth/oauth.go`): the
signed `state` encodes the provider id, so the callback resolves which provider (and secret) to
use — CSRF is a stateless per-provider HMAC-signed `state`, not a cookie. The **only** HTTP
endpoint is `GET /auth/oauth/callback` (`auth.RegisterRoutes`) — a browser redirect target that
redirects to `/login#token=…`. A default `admin`/`admin` user is seeded by `LoadDatabase` when
the users table is empty. The web UI has admin-only **Users** and **OAuth providers** tabs. Full
surface + flows in `AUTH.md`. Note: auth RPCs live in the shared proto, so regenerating requires
`protoc-gen-go v1.35.1` (see the codegen caveat above).

**Exporters** (`internal/exporter`, `pkg/exporter`): the plugin subsystem. Mirrors the
indexer's manager/supervisor shape — `ExporterServiceManager` starts one `ExporterService`
per enabled `EvmiExporter` (bound to a pipeline), reacting to `exporter.enable`/
`exporter.disable` bus events. Each service streams the pipeline's stored logs, in
`(block_number, log_index)` order, into a user-written native Go plugin's `NewLogEvent`,
committing `EvmiExporter.SyncBlock` at block boundaries (at-least-once; plugins dedupe on the
stable `LogEvent.Id`). The safe export head is the *min* `SyncBlock` across the pipeline's
sources. Plugins implement the public `pkg/exporter.Exporter` interface (public so external
plugin repos can import it — `internal/` can't be imported out-of-module) and export a
`func New() exporter.Exporter` symbol. **Plugin code is a separate `Plugin` entity**, not an
exporter field: `exporter.InstallPlugin` (via the `InstallPlugin` RPC) resolves the source and
builds the `.so` with `-buildmode=plugin`, recording `SoPath`/`Status` on the `Plugin` row; an
exporter references it by `PluginID` and `loader.go:loadInstalledPlugin` opens the installed
`.so` via `plugin.Open` (an exporter only starts if its plugin is `INSTALLED`). A plugin may
implement the optional `pkg/exporter.Configurable` interface to declare a config schema;
install extracts it into `Plugin.ConfigSchema`, the exporter handlers validate `PluginConfig`
against it (`exporter.ValidatePluginConfig`, → `InvalidArgument`), and the web UI renders a
typed config form from it. Wired into
`main.go` after the indexer, which first calls `exporter.VerifyPlugins` — the `.so` build cache
is ephemeral across restarts, so each boot re-checks every `INSTALLED` plugin's `SoPath` and
rebuilds missing GitHub-sourced plugins (or marks non-GitHub ones `FAILED`). **Design + native-plugin caveats (CGO, toolchain/version match, no
isolation, runtime image needs a Go toolchain to build plugins from source) live in
`docs/exporters.md` — read it before touching this subsystem.** Cross-source ordered reads use
`EvmIndexerStorage.GetLogsAfter`. Plugins and exporters are managed over the Connect API
(`internal/grpc/plugin-handlers.go` CRUD + `InstallPlugin`; `exporter-handlers.go` CRUD +
`StartExporter`/`StopExporter` emitting the enable/disable bus events), with **Plugins** and
**Exporters** tabs in the web UI. `UpdateEvmiExporter` never overwrites the server-managed
cursor (`sync_block`/`sync_log_index`) or `status`.

**Metrics** (`internal/metrics`, Prometheus): definitions live in `metrics.go`, the guarded API
in `service.go`. Every method is nil-safe and no-ops when the service is disabled or nil (so
tests can pass a nil `*MetricService`). Metric names are `evm_indexer_*`; labels are snake_case
and consistent — per-source metrics all go through `SourceLabels`
(`chain_id`/`pipeline`/`store`/`source_id`/`source_type`) via `SourceIndexerService.sourceLabels()`,
per-exporter through `ExporterLabels` via `ExporterService.exporterLabels()`. Families: chain head
(`chain_head_block`), per-source progress (`source_synced_block`, `source_lag_blocks` = head−synced
clamped ≥0, `source_up`) and throughput (`logs_indexed_total`, `transactions_indexed_total`,
`batch_duration_seconds`), store writes (`logs_stored`, `store_write_duration_seconds`,
`store_write_errors_total`, `store_disk_bytes`), RPC (`rpc_requests_total{status}`,
`rpc_request_duration_seconds`), and exporters (`exporter_synced_block`, `exporter_lag_blocks`,
`exporter_up`, `exporter_events_total`, `exporter_errors_total`, `exporter_process_duration_seconds`).
RPC calls are timed via `SourceIndexerService.timedRPC(method, fn)`; the server runs on its own
`http.ServeMux` (not the default mux). Scraped per the compose Prometheus config
(`docker/prometheus.yml` scrapes `indexer:9999/metrics`; the port is `metrics.port` in the config
file). The compose stack **auto-provisions Grafana**: `docker/grafana/provisioning/` wires a
Prometheus datasource (uid `prometheus` → `http://prometheus:9090`) and a dashboard provider, and
`docker/grafana/dashboards/evmi-indexer.json` is the dashboard (uid `evmi-indexer`, `$pipeline`
template var; rows: Overview / Indexing / JSON-RPC / Log store / Exporters). Open Grafana at
`localhost:30000` (admin/admin). **Cardinality note:** per-source series are keyed by `source_id`,
so a factory that spawns many children produces many series — aggregate in PromQL (by
pipeline/chain) rather than graphing every source.

**Web UI** (`webui/`, served by `internal/grpc/webui.go`): a Next.js app (App Router) built as
a **static export** (`output: 'export'` → `webui/out/`). The Go server mounts it at `/` via
`newWebUIHandler`, serving files from `EVMI_WEBUI_DIR` (default `public`) with an index.html
fallback for client routes; it returns nil (skips mounting) when no `index.html` is present, so
a dev checkout without a build is fine. Route precedence matters: the Connect service path and
`/auth/oauth/callback` are registered as more-specific patterns, so `/` never shadows them. The
Dockerfile has a `node:20` stage that runs `npm run build` and copies `out/` into the image at
`/public` (with `ENV EVMI_WEBUI_DIR=/public`). The UI talks to the API via a **generated,
typed Connect-ES v2 client**: `webui/buf.gen.yaml` runs `protoc-gen-es` over
`internal/grpc/proto` into `webui/gen/` (committed — the Docker webui stage has no proto in its
context), and `webui/lib/client.ts` wraps it with a `connect-web` transport + a bearer-token
interceptor. So the proto now has **two** codegen consumers: Go (`buf generate` at repo root,
needs `protoc-gen-go v1.35.1`) and the web UI (`npm run generate` in `webui/`, needs
`@bufbuild/protoc-gen-es`) — regenerate both after editing the proto. The UI is a routed
CRUD app: one App Router route per entity (`/blockchains`, `/abis`, `/stores`, `/pipelines`,
`/sources`, `/exporters`) under the `app/(app)/` route group (its `layout.tsx` is the
auth guard + sidebar), plus `/login`. Each entity is defined declaratively one-per-file in
`webui/lib/resources/` and rendered by the generic `webui/components/ResourceManager.tsx`.

## Config file vs. runtime topology

The config file is deliberately tiny. The Go `Config` struct (`internal/types/config.go`)
unmarshals these keys:
- `database` — `type` = `SQLITE`/`POSTGRES`/`MYSQL`, plus a `config` string map. SQLite reads
  `config.filename`; Postgres/MySQL read `config.dsn`.
- `metrics` — `enabled` / `path` / `port`.
- `plugins` — a list of `{name, description, gitUrl, relativePath}` git-hosted exporter plugins
  imported (created if absent, matched by name) and installed on startup by
  `exporter.ImportConfigPlugins`.
- `resources` — an **optional autoloader** (`internal/autoloader`) that creates metadata-DB rows
  on startup with a **create-if-not-exists** policy (see below).

Everything under `resources` — blockchains, ABIs, log stores, pipelines, sources, exporters —
otherwise lives in the metadata DB and is created through the gRPC API at runtime. Accurate
examples: `configs/exemple.config.json` (SQLite), `configs/exemple-postgres.config.json`
(Postgres), `docker/ambiant.config.json` (the compose stack), and
`configs/exemple-autoload.config.json` (the full `resources` autoloader).

**Config autoloader** (`internal/autoloader/autoloader.go`, `autoloader.Load`): provisions the
`resources` block idempotently. It runs in `main.go` **after** `ImportConfigPlugins` and **before**
the indexer/exporter services start, so autoloaded sources/exporters are picked up on that same
boot. Each resource is matched by its **natural key** and skipped if present (created previously,
or via the API): blockchain by `name`, ABI by `contractName`, store by `identifier`, pipeline by
`(name, instance)`, source by `(pipeline, type, address|topic0)` (FULL by `(pipeline, type)`),
exporter by `(name, pipeline)`. Cross-references are declared by **name, not DB id** (ids aren't
known ahead of time) and resolved against the DB, so a resource may reference one declared earlier
in the same file or a pre-existing row; a source's `blockchain` defaults to its pipeline's. Load
order is dependency-first (blockchains/ABIs/stores → pipelines → sources → exporters) and
best-effort — an unresolved reference logs an error and skips just that row. Exporters reference a
`plugin` by name, which is why `ImportConfigPlugins` must run first. Types live in
`internal/types/config.go` (`AutoloadResources` + `Config*` structs; store/plugin config blobs are
`json.RawMessage` → `datatypes.JSON`).

Note the field is `config` (a `map[string]string`), so `dsn`/`filename` must be nested
under `database.config`, not at the top of `database`. `cmd/evm-indexer/staging.config.json`
places `dsn` at the wrong level (top of `database`) — it only works because it uses SQLite
and `filename` is correctly nested.

**Log store config lives in the DB, not the config file.** A `clickhouse` `EvmLogStore` row's
JSON `StoreConfig` (read in `clickhouse/store.go:Init`) expects: `addr` (comma-separated for
multiple nodes), `database`, `username`, `password`, `logsTableName`, `transactionsTableName`.
Set these when creating the store over the gRPC API.
