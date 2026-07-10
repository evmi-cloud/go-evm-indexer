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
   sources, ordered by         (per exporter)          (user .so)
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
- The plugin is a native Go plugin (`.so`) loaded in-process by
  `internal/exporter/loader.go`.

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
- Reorgs are **not** handled in v1: a reorged log is delivered with
  `Removed = true` and the cursor is not rewound. Plugins that care must inspect
  `Removed`. (A future confirmation-depth option can lag the safe head by N
  blocks to reduce reorg exposure.)

## Writing a plugin

Import the public SDK and implement `exporter.Exporter` in a `package main`:

```go
package main

import exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"

type myExporter struct{ /* ... */ }

func (e *myExporter) Name() string                          { return "my-exporter" }
func (e *myExporter) Init(ctx exporter.Context) error       { /* open DB, parse ctx.Config */ return nil }
func (e *myExporter) NewLogEvent(l exporter.LogEvent) error { /* upsert by l.Id */ return nil }
func (e *myExporter) Close() error                          { /* flush */ return nil }

// New is the symbol the server looks up.
func New() exporter.Exporter { return &myExporter{} }

func main() {} // required for -buildmode=plugin
```

Build it:

```bash
go build -buildmode=plugin -o my-exporter.so ./path/to/plugin
```

See `examples/exporters/logcount` for a working template.

### Plugins are a separate entity

The plugin **code** is a first-class `Plugin` row, installed independently of any
exporter. An exporter just references an already-installed plugin by `PluginID`.

A `Plugin` row (managed via the API / the web UI's **Plugins** tab):

| field          | meaning                                                        |
|----------------|----------------------------------------------------------------|
| `Name`         | display name (shown in the exporter's plugin picker)           |
| `LocalPath`    | prebuilt `.so`, **or** a module root to build from             |
| `GitUrl`    | any git repo to clone and build (used when no `.so` is given)          |
| `RelativePath` | package to build within the module root                        |
| `SoPath`       | the resolved/compiled `.so` (set on install)                   |
| `Status`       | `NOT_INSTALLED` → `INSTALLING` → `INSTALLED` / `FAILED`         |

**Install** (`InstallPlugin` RPC → `exporter.InstallPlugin`) resolves the source
to a `.so` and records the result: a `LocalPath` ending in `.so` is used directly;
otherwise the server builds from `GitUrl` (cloned) or `LocalPath` (module
root), compiling the `RelativePath` package. Editing a plugin's source resets it
to `NOT_INSTALLED`.

**Config schema.** If the plugin implements the optional `Configurable`
interface, install also extracts its declared config schema (a JSON array of
`{name,type,required,description,default}`) into `Plugin.ConfigSchema`. When an
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

An exporter only loads a plugin whose `Status` is `INSTALLED`; otherwise it fails
to start with "plugin is not installed".

**Startup verification.** The build cache (`$TMPDIR/evmi-plugins`) is typically
wiped across restarts / container recreations, so on every boot
`exporter.VerifyPlugins` checks that each `INSTALLED` plugin's `SoPath` still
exists on disk. If it is missing: a plugin with a `GitUrl` is **rebuilt**
automatically; a plugin without one (a prebuilt `.so` or local dir that is gone)
is set to **`FAILED`**, since it cannot be rebuilt from a remote source.

**Config-declared plugins.** The server config may include a `plugins` array,
each entry `{name, description, gitUrl, relativePath}`. On startup
`exporter.ImportConfigPlugins` creates a `Plugin` row for any that don't exist yet
(matched by name) and installs them — so git-hosted plugins are available out of
the box. See `configs/exemple-postgres.config.json`.

## Operational caveats (native plugins)

Go's `-buildmode=plugin` is powerful but strict. These constraints are inherent
to the native-plugin approach:

- **No isolation.** The plugin runs in the server process. A panic or a blocking
  call in the plugin affects the whole server. Keep plugins fast and defensive.
- **Exact-match builds.** A `.so` only loads if built with the **same Go
  toolchain version** and the **same versions of all shared dependencies**
  (notably `pkg/exporter` and anything it transitively pulls in) as the running
  server. A mismatch fails at `plugin.Open`. Pin the plugin's `go.mod` to the
  server's module version.
- **Linux/macOS only.** Windows is unsupported by `-buildmode=plugin`.
- **CGO required.** Plugins need `CGO_ENABLED=1`. The `Dockerfile` builds the
  server with CGO enabled (Debian/glibc) into a `distroless/cc` image, so a
  **prebuilt** `.so` compiled on a matching glibc base (e.g. `golang:1.23-bookworm`)
  can be mounted and loaded. **Building a plugin from source at runtime** still
  needs the Go toolchain + git + gcc, which the distroless image does not include —
  use a fuller base image (or a sidecar/init step) for that.
- **No unload.** A loaded plugin cannot be unloaded; disabling an exporter stops
  its loop but the code stays resident until the process exits.

## Managing exporters

`EvmiExporter` rows are managed over the Connect API — `CreateEvmiExporter`,
`GetEvmiExporter`, `UpdateEvmiExporter`, `ListEvmiExporters`, `DeleteEvmiExporter`,
plus `StartExporter` / `StopExporter` (which emit the enable/disable bus events).
The web UI exposes all of this under its **Exporters** tab. `UpdateEvmiExporter`
deliberately does not touch the server-managed cursor (`sync_block`,
`sync_log_index`) or `status`.

## Not yet implemented (Phase 2)

- Git ref/commit pinning for `GitUrl` (v1 shallow-clones the default
  branch and reuses the cached checkout).
- Confirmation-depth lag and reorg-aware rollback.
- Prometheus metrics dedicated to exporter progress (v1 reuses the
  latest-block-indexed gauge with the exporter name).
