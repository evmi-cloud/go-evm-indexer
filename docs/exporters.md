# Exporters

Exporters let users run **custom Go plugins** against indexed data. The EVMI
server calls a plugin's `NewLogEvent` once for every log stored in a pipeline's
log store, in block order, and tracks how far each exporter has progressed so it
resumes after a restart.

A typical use case: a plugin that maintains ERC-20 balances in the user's own
database by processing `Transfer` events as they are exported.

## Architecture

```
 log store (ClickHouse)              EvmiExporter row
   logs for pipeline's  ──►  ExporterService  ──►  plugin.NewLogEvent(log)
   sources, ordered by         (per exporter)        (gRPC → subprocess)
   (block, log_index)               │
                                     └─ commit SyncBlock at each block boundary
```

- An **exporter is bound to one pipeline** (`EvmiExporter.EvmLogPipelineID`). A
  pipeline has one blockchain, one log store, and one or more sources.
- `ExporterServiceManager` (`internal/exporter/service.go`) starts one
  `ExporterService` per enabled exporter under a `suture` supervisor and reacts
  to `exporter.enable` / `exporter.disable` bus events.
- `ExporterService` (`internal/exporter/exporter.go`) runs the export loop:
  compute a safe head, pull the next range of logs from the store, deliver each
  log to the plugin, and commit the sync cursor.
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

- The cursor is a **(block, log index) pair**, stored on `EvmiExporter` as
  `SyncBlock` + `SyncLogIndex`. `SyncBlock` is the last fully-completed block;
  `SyncLogIndex` is the last `log_index` delivered within the in-progress block
  (`SyncBlock+1`), or `-1` when none of it has been processed. Together they pin
  the exact last log executed, so a restart resumes **mid-block** instead of
  replaying a partially-processed block. On first run the cursor starts at
  `StartBlock`.
- The cursor is persisted **after every delivered log** (via the store method
  `GetLogsAfter`, which fetches strictly after the cursor). A failure leaves the
  cursor at the last successfully delivered log; the failing log is replayed on
  restart.
- **Safe head** is the *minimum* `SyncBlock` across the pipeline's enabled
  sources. The exporter never exports past the least-synced source, so a range is
  never delivered with a source still lagging inside it. A permanently-lagging or
  disabled-but-required source therefore stalls the exporter — this is intended
  (completeness over liveness).

### Delivery guarantees

- **At-least-once.** After a crash or plugin error, the current block is replayed
  from the start. Plugins **must be idempotent** — key writes on
  `LogEvent.Id` (`chainId:blockNumber:logIndex`), which is stable and unique.
- Logs are delivered in ascending `(block_number, log_index)` order across all of
  the pipeline's sources.
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

### Plugins are a separate entity

The plugin **code** is a first-class `Plugin` row, installed independently of any
exporter. An exporter just references an already-installed plugin by `PluginID`.

A `Plugin` row (managed via the API / the web UI's **Plugins** tab):

| field          | meaning                                                        |
|----------------|----------------------------------------------------------------|
| `Name`         | display name (shown in the exporter's plugin picker)           |
| `GitUrl`       | git repository to clone and build (**the only source**)        |
| `GitRef`       | optional branch or tag (empty = the repo's default branch)     |
| `BinaryPath`   | the compiled plugin executable (set on install)                |
| `Status`       | `NOT_INSTALLED` → `INSTALLING` → `INSTALLED` / `FAILED`         |

**Install** (`InstallPlugin` RPC → `exporter.InstallPlugin`) is **git-only** and
idempotent: if the plugin is already `INSTALLED` and its binary is present it does
nothing; otherwise it clones `GitUrl` at `GitRef` and builds the **repo root**
(the deterministic target — the plugin's `main` package must live there; no
package path to configure), then records the result. Editing a plugin's source
resets it to `NOT_INSTALLED`.

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
| `SyncBlock`        | cursor (managed by the server)                             |
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
each entry `{name, description, gitUrl, gitRef}`. On startup
`exporter.ImportConfigPlugins` creates a `Plugin` row for any that don't exist yet
(matched by name) and installs them — so git-hosted plugins are available out of
the box. See `configs/exemple-postgres.config.json`.

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
