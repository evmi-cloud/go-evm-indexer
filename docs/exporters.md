# Exporters

Exporters let users run **custom Go plugins** against indexed data. The EVMI
server calls a plugin's `NewLogEvent` once for every log stored in a pipeline's
log store, in block order, and tracks how far each exporter has progressed so it
resumes after a restart.

A typical use case: a plugin that maintains ERC-20 balances in the user's own
database by processing `Transfer` events as they are exported.

## Architecture

```
 log store (ClickHouse)          EvmiExporterSourceCursor rows
   logs for pipeline's  ──►  ExporterService  ──►  plugin.NewLogEvent(log)
   sources, ordered by         (per exporter)        (gRPC → subprocess)
   (block, log_index)               │
                                     └─ commit the source's cursor per log
```

- An **exporter is bound to one pipeline** (`EvmiExporter.EvmLogPipelineID`). A
  pipeline has one blockchain, one log store, and one or more sources.
- `ExporterServiceManager` (`internal/exporter/service.go`) starts one
  `ExporterService` per enabled exporter under a `suture` supervisor and reacts
  to `exporter.enable` / `exporter.disable` bus events.
- `ExporterService` (`internal/exporter/exporter.go`) runs the export loop: on
  every pass it re-reads the pipeline's enabled sources, pulls the next range of
  logs for each from the store, merges them into one ordered stream, delivers each
  log to the plugin, and commits that source's cursor.
- The plugin is a **separate process**: an ordinary executable that EVMI launches
  and talks to over gRPC via
  [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)
  (`internal/exporter/plugin_process.go`). The process lives exactly as long as
  the exporter's `Serve` call and is killed when the exporter stops.

### What that buys, and what it costs

A plugin is built with any Go toolchain and any dependency versions, needs no
CGO, works on every platform Go targets, is isolated (a panic fails only that
exporter), and is actually terminated when its exporter stops. The cost is an
inter-process gRPC call per delivered log.

### Sync model

- **The cursor is per source, not per exporter.** Each `(exporter, source)` pair
  has an `EvmiExporterSourceCursor` row, and that row is what the export loop
  resumes from. `EvmiExporter.SyncBlock` / `SyncLogIndex` still exist, but they
  are now the *aggregate* — the minimum across those rows, recomputed once per
  batch for the API, the UI and the progress metrics. Nothing resumes from them.
- Each cursor is a **(block, log index) pair**: `SyncBlock` is the last
  fully-exported block of that source, `SyncLogIndex` the last `log_index`
  delivered within the in-progress block (`SyncBlock+1`), or `-1` when none of it
  has been. Together they pin the exact last log delivered for the source, so a
  restart resumes **mid-block** instead of replaying a partially-processed block.
- A cursor is persisted **after every delivered log** (fetching uses the store
  method `GetLogsAfter`, which returns logs strictly after the cursor). A failure
  leaves each source's cursor at its last successfully delivered log; the failing
  log is replayed on restart.
- **Why per source.** The set of sources is not fixed: a `FACTORY` rule or a
  plugin calling `Host.CreateLogSource` can attach one at any time, typically well
  behind where the exporter already stands. With a single pipeline-wide cursor
  such a source could only ever be exported from the exporter's current position,
  so every log already stored for it below that point was silently dropped. With
  its own cursor it is picked up on the next loop pass, seeded from its own
  `StartBlock`, and its whole backlog is delivered.
- **Head is per source too:** each source is exported up to *its own* `SyncBlock`.
  A source that is behind no longer stalls the ones that are caught up (the old
  pipeline-wide minimum did), and it never causes a partially-exported block for
  itself.
- The exporter's own `StartBlock` still gates every source, new ones included: a
  cursor is never seeded below `StartBlock - 1`.
- **Seeing the cursors.** `ListEvmiExporterSourceCursors(exporter_id)` returns one
  row per source of the exporter's pipeline — its cursor, the source's indexed
  head, and the lag between them — and `StreamEvmiExporterSourceCursors` streams
  the same rows live (one event per source per exported batch, plus one the moment
  a source is first tracked). The web UI renders both under the **Details** button
  on the Exporters tab. A source with no cursor row yet is listed at its own
  `StartBlock`, which is where the exporter would seed it.
- **Upgrading.** `LoadDatabase` backfills cursor rows for exporters that already
  made progress under the old scheme, copying their aggregate position onto every
  source of their pipeline, so upgrading does not replay a pipeline into a plugin.
  It runs once per exporter — a source attached *after* the upgrade is correctly
  treated as new.

### Delivery guarantees

- **At-least-once.** After a crash or plugin error, the current block is replayed
  from the start. Plugins **must be idempotent** — key writes on
  `LogEvent.Id` (`chainId:blockNumber:logIndex`), which is stable and unique.
- Logs of a **single source** are always delivered in ascending
  `(block_number, log_index)` order, and sources sitting at the same cursor are
  merged into one ordered stream, so in steady state the whole pipeline is
  delivered in `(block_number, log_index)` order. A source that joins late is by
  definition behind the others, so its backlog arrives while they are already
  further ahead: **do not rely on a pipeline-wide monotonic block order** across
  sources.
- Delivery is strictly sequential: one `NewLogEvent` call is in flight at a time,
  so a plugin never has to be goroutine-safe.
- Reorgs are **not** handled in v1: a reorged log is delivered with
  `Removed = true` and the cursor is not rewound. Plugins that care must inspect
  `Removed`. (A future confirmation-depth option can lag the safe head by N
  blocks to reduce reorg exposure.)

## Writing a plugin

Import the public SDK, implement `exporter.Exporter` in a `package main`, and
hand it to `exporter.Serve`:

```go
package main

import exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"

type myExporter struct{ /* ... */ }

func (e *myExporter) Name() string                          { return "my-exporter" }
func (e *myExporter) Init(ctx exporter.Context) error       { /* open DB, parse ctx.Config */ return nil }
func (e *myExporter) NewLogEvent(l exporter.LogEvent) error { /* upsert by l.Id */ return nil }
func (e *myExporter) Close() error                          { /* flush */ return nil }

// Serve blocks until EVMI disconnects.
func main() { exporter.Serve(&myExporter{}) }
```

Build it like any other program:

```bash
go build -o my-exporter .
```

In practice EVMI builds it for you: push it to a git repository with `main` at
the root and install it as a `Plugin` (below).

Two rules specific to running as a plugin:

- **Never write to stdout.** Stdout carries the go-plugin handshake and the gRPC
  connection. Log to **stderr** (the standard library `log` package does by
  default) — EVMI captures it and forwards it to its own log, tagged with the
  plugin name.
- **Errors are values, not panics.** A returned error stops the exporter with
  that message recorded on the row. A panic kills the plugin process; the
  supervisor restarts the exporter, which replays from the last committed cursor.

See `examples/exporters/logcount` for a working template.

### Wire protocol

`pkg/exporter/proto/exporter.proto` defines the host↔plugin gRPC service, and
`grpc_client.go` / `grpc_server.go` adapt it to the `Exporter` interface on each
side. Adding a field to `LogEvent` means adding it in all three. Regenerate with:

```bash
cd pkg/exporter/proto && buf generate   # protoc-gen-go v1.35.1 + protoc-gen-go-grpc v1.5.1
```

This module is deliberately separate from the root `buf.yaml` (the public Connect
API), so regenerating one never touches the other.

`Handshake.ProtocolVersion` in `pkg/exporter/plugin.go` guards compatibility: bump
it on an incompatible protocol change and plugins built against an older SDK are
rejected at startup with a clear handshake error instead of misbehaving.

### Host API (plugin → EVMI)

The protocol runs both ways. Alongside `ExporterPluginService` (EVMI calling the
plugin), `exporter.proto` defines **`ExporterHostService`** — EVMI functions the
plugin calls back into.

The transport is hashicorp/go-plugin's **`GRPCBroker`**, which multiplexes extra
connections over the existing plugin link. On `Init` the host calls
`broker.NextId()`, serves `ExporterHostService` on that id
(`GRPCClient.serveHost`), and passes the id in `InitRequest.host_broker_id`; the
plugin dials it back (`grpcServer.dialHost`) and hands the author a `Host` on
`exporter.Context`. Under `AutoMTLS` the broker connection inherits the same
per-launch certificate as the main one, so the callback channel is authenticated
too.

`host_broker_id == 0` means "no host API" — an older server, or a host that
installed none. The plugin then receives a nil `Context.Host` rather than an
error, which is why adding this needed **no `ProtocolVersion` bump**: an old
plugin ignores the new field, and a new plugin against an old host sees nil and
can degrade or refuse on its own terms.

The implementation is `internal/exporter/host.go` (`exporterHost`), built per
running exporter in `ExporterService.Serve` and installed with
`pluginProcess.SetHost` **before `Init`** — plugins are expected to register ABIs
and read the chain from inside `Init`. Its lifetime is the exporter's: `Close`
stops the broker server after the plugin's own `Close` returns, so a plugin may
still call back while flushing.

What it exposes, and why each is scoped the way it is:

| Call | Backed by | Notes |
|---|---|---|
| `Blockchain()` | the pipeline's `EvmBlockchain` | includes `RpcUrl`, the endpoint the indexer itself polls |
| `CreateLogSource()` | `EvmLogSource` insert + `source.enable` | mirrors `registerFactoryChild` exactly |
| `UpsertAbi()` / `GetAbi()` / `GetAbiByID()` / `ListAbis()` | `EvmJsonAbi` | upsert never overwrites; ABI JSON is validated on insert |

**Scoping.** `exporterHost` carries the exporter's `pipelineID`, and
`CreateLogSource` rejects a parent belonging to any other pipeline. That single
check is what keeps a plugin inside its own topology — there is no other
authorization layer, because the plugin is already trusted code the operator
installed.

**`CreateLogSource` deliberately mirrors the factory system.** A plugin-created
child is indistinguishable from a rule-created one: same pipeline/store/chain as
its parent, created enabled, started best-effort over the `source.enable` topic,
nested under the parent in the UI, and read-only there (`ParentSourceID != 0`).
It exists for deployments a rule cannot catch — chiefly when the creation event
does not carry the new contract's address, so `CreationAddressLogArg` has nothing
to read and the plugin must resolve it another way.

It is idempotent per `(parent, address)` for the same reason the factory path is,
but the stakes are higher here: export delivery is **at-least-once**, so a plugin
*will* re-see deployment logs after a restart. Addresses are lowercased on both
store and compare, so a checksummed address and its lowercase form cannot produce
two sources for one contract.

**Cost.** A host call is a local gRPC round trip on top of the one already spent
delivering the log. Calls made per-log therefore double the per-log cost; prefer
resolving what you can in `Init` (ABI ids, chain info) and calling out only on
the rare events that need it.

### Plugins are a separate entity

The plugin **code** is a first-class `Plugin` row, installed independently of any
exporter. An exporter just references an already-installed plugin by `PluginID`.

A `Plugin` row (managed via the API / the web UI's **Plugins** tab):

| field          | meaning                                                        |
|----------------|----------------------------------------------------------------|
| `Name`         | display name (shown in the exporter's plugin picker)           |
| `GitUrl`       | git repository to clone and build (**the only source**)        |
| `GitRef`       | optional branch or tag (empty = the repo's default branch)     |
| `Path`         | subdirectory holding the plugin's `main` package (empty = repo root) |
| `BinaryPath`   | the compiled plugin executable (set on install)                |
| `Status`       | `NOT_INSTALLED` → `INSTALLING` → `INSTALLED` / `FAILED`         |

**Install** (`InstallPlugin` RPC → `exporter.InstallPlugin`) is **git-only** and
idempotent: if the plugin is already `INSTALLED` and its binary is present it does
nothing; otherwise it clones `GitUrl` at `GitRef` and runs a plain `go build` in
`<clone>/<Path>` (the clone root when `Path` is empty), then records the result.
Editing a plugin's source — url, ref **or path** — resets it to `NOT_INSTALLED`.

`Path` is what lets **one repository host several plugins**: the build runs from
the plugin's own directory, so `go build` picks up whichever module encloses it —
a monorepo with a single `go.mod` at the root and one `main` package per plugin
works, and so does a repo where each plugin carries its own `go.mod`. The value is
always relative and is rejected if it escapes the clone
(`exporter.ValidatePluginPath`).

### Plugin catalog

A repository hosting several plugins can describe them in a **catalog file** at
`evmi-plugins.json` or `.evmi/plugins.json` (first found wins), so nobody has to
know its layout:

```json
{
  "plugins": [
    { "name": "erc20-balances", "description": "Track ERC-20 balances", "path": "exporters/erc20" },
    { "name": "webhook",        "description": "POST every log to a webhook",  "path": "exporters/webhook" }
  ]
}
```

A bare JSON array of the same entries is accepted too. `path` is relative to the
repo root; `name` is required. This repository is itself an example — see
[`evmi-plugins.json`](../evmi-plugins.json), which lists the two example plugins.

The catalog is **read-only discovery**, never a source of truth: it is used by

- the **web UI** — the plugin form's *Path in repository* field suggests the
  catalog's entries (`ListPluginCatalog` RPC → `exporter.FetchPluginCatalog`,
  which shallow-clones the repo into a temp dir, reads the file and deletes the
  clone). Nothing is built and no row is created, and a repo without a catalog
  simply offers no suggestion — the path stays free text;
- the **server config**, with `"catalog": true` on a `plugins` entry (below).


**Config schema.** If the plugin implements the optional `Configurable`
interface, install also extracts its declared config schema (a JSON array of
`{name,type,required,description,default}`) into `Plugin.ConfigSchema` — by
launching the freshly built binary once, asking it, and killing it again. When an
exporter is created/updated, `CreateEvmiExporter`/`UpdateEvmiExporter` validate
its `PluginConfig` against that schema (`exporter.ValidatePluginConfig`) —
required fields present, correct JSON types — returning `InvalidArgument` on
mismatch. The web UI renders a typed form from the schema. Plugins without a
schema accept any config.

An `EvmiExporter` row then binds it to a pipeline:

| field              | meaning                                                    |
|--------------------|------------------------------------------------------------|
| `EvmLogPipelineID` | pipeline whose logs are exported                           |
| `PluginID`         | the installed plugin to run                                |
| `Enabled`          | manager starts it when true                                |
| `StartBlock`       | first block to process                                     |
| `SyncBlock`        | aggregate cursor for display (per-source rows are the truth) |
| `PluginConfig`     | raw JSON passed to the plugin's `Init` as `Context.Config` |

An exporter only launches a plugin whose `Status` is `INSTALLED`; otherwise it
fails to start with "plugin is not installed".

**Storage paths.** A plugin is cloned + compiled in an ephemeral per-plugin work
dir `<buildDir>/<pluginName>` (default `<tmp>/evmi/<pluginName>`), then the built
executable is **copied** to `<installDir>/<pluginName>` (default `/evmi/plugins/…`)
and that install path becomes the `Plugin.BinaryPath`. Both bases are configurable
via `config.pluginStorage.{buildDir,installDir}` (`exporter.Configure`). To avoid
rebuilding on every restart, mount a **persistent volume at `installDir`** — then
the binary survives and `VerifyPlugins` finds it; the build dir can stay on tmpfs.

**Startup verification.** If the install dir is *not* persisted it is wiped across
restarts / container recreations, so on every boot `exporter.VerifyPlugins` checks
that each `INSTALLED` plugin's `BinaryPath` still exists on disk. If it is missing
it is **rebuilt** automatically from its `GitUrl` (a malformed row with no
`GitUrl` is set to **`FAILED`**).

**Config-declared plugins.** The server config may include a `plugins` array,
each entry `{name, description, gitUrl, gitRef, path}`. On startup
`exporter.ImportConfigPlugins` creates a `Plugin` row for any that don't exist yet
(matched by name) and installs them — so git-hosted plugins are available out of
the box. See `configs/exemple-postgres.config.json`.

An entry may instead name a **whole plugin repo** with `"catalog": true`:

```json
{ "gitUrl": "https://github.com/your-org/evmi-plugins", "gitRef": "main", "catalog": true }
```

`exporter.expandConfigPlugins` then reads that repo's catalog file and imports one
plugin per entry (`name`, `description` and `path` come from the catalog; the
entry's own `name`/`description`/`path` are ignored). It stays idempotent — an
existing plugin of the same name is left alone — and a repo whose catalog cannot
be read is logged and skipped rather than failing the boot. `ExportConfiguration`
always writes plugins back out **one entry per installed plugin**, never as a
`catalog` entry, so an exported config describes exactly what is installed.

## Operational caveats

- **A Go toolchain and `git` must be present at runtime.** Plugins install from
  git and are compiled on the instance, so the runtime image ships the `go`
  toolchain and `git` (see the `Dockerfile`) — do not swap it for a distroless
  image unless you don't use exporter plugins.
- **One process per running exporter.** Each enabled exporter holds a plugin
  subprocess for as long as it runs. Sizing an instance means accounting for
  them.
- **A slow plugin slows its exporter.** Delivery is sequential and synchronous:
  the export loop waits for `NewLogEvent` to return. Batch or buffer inside the
  plugin if the per-log work is expensive, and flush in `Close`.
- **A killed plugin replays.** Stopping an exporter kills the process; the
  cursor is committed per log, so the next start resumes from the last delivered
  log. Combined with at-least-once delivery, writes must be idempotent.
- **Plugins run with the server's privileges.** They are not sandboxed — separate
  process, same user, same filesystem and network. Only install plugins from
  sources you trust.

## Managing exporters

`EvmiExporter` rows are managed over the Connect API — `CreateEvmiExporter`,
`GetEvmiExporter`, `UpdateEvmiExporter`, `ListEvmiExporters`, `DeleteEvmiExporter`,
plus `StartExporter` / `StopExporter` (which emit the enable/disable bus events).
The web UI exposes all of this under its **Exporters** tab. `UpdateEvmiExporter`
deliberately does not touch the server-managed cursor (`sync_block`,
`sync_log_index`) or `status`.

## Not yet implemented (Phase 2)

- Confirmation-depth lag and reorg-aware rollback.
- Batched delivery (one gRPC call per block instead of per log) for plugins that
  opt in.
- Non-Go plugins: the transport is gRPC, so any language with a gRPC
  implementation could serve `exporter.proto`; only the Go SDK and the git-clone
  + `go build` install path exist today.
