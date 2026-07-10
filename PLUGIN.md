# Building an EVMI Exporter Plugin

An **exporter plugin** is a small Go program that EVMI runs against your indexed
data. EVMI calls your plugin's `NewLogEvent` once for every log stored in a
pipeline, **in block order**, and remembers how far it got so it resumes after a
restart. You use it to project on-chain events into whatever you want — a
Postgres table, a cache, a webhook, an ERC-20 balance sheet.

This guide walks through writing, building, and registering one. For the
architecture and the guarantees behind it, see [docs/exporters.md](docs/exporters.md).

---

## 1. The contract

Your plugin implements one interface from the public SDK package
`github.com/evmi-cloud/go-evm-indexer/pkg/exporter`:

```go
type Exporter interface {
    Name() string                       // identifier for logs
    Init(ctx Context) error             // called once, before any event
    NewLogEvent(log LogEvent) error     // called once per stored log, in order
    Close() error                       // called on stop; flush here
}
```

and exports a factory symbol EVMI looks up by name:

```go
func New() exporter.Exporter { return &myExporter{} }
```

### Declaring your config parameters (recommended)

Implement the optional `Configurable` interface to **declare the config your
plugin expects**. EVMI extracts this schema when the plugin is installed, renders
a typed form for it in the exporter UI, and **validates each exporter's config
against it** (required fields present, correct types) when the exporter is
created or updated. Plugins that don't implement it accept any config.

```go
func (e *myExporter) ConfigSchema() []exporter.ConfigField {
    return []exporter.ConfigField{
        {Name: "dsn", Type: exporter.StringField, Required: true, Description: "Postgres DSN"},
        {Name: "token", Type: exporter.StringField, Required: true, Description: "ERC-20 address"},
        {Name: "decimals", Type: exporter.NumberField, Required: false, Default: "18"},
    }
}
```

Types: `StringField` → JSON string, `NumberField` → JSON number, `BoolField` →
JSON boolean. The values arrive in `Context.Config` (the raw JSON you decode in
`Init`).

`Init` receives a `Context`:

```go
type Context struct {
    ExporterName string
    PipelineId   uint64
    ChainId      uint64
    Config       []byte // your PluginConfig JSON, decode it into your own struct
}
```

`NewLogEvent` receives a decoded `LogEvent`:

```go
type LogEvent struct {
    Id               string            // "chainId:blockNumber:logIndex" — stable, unique
    SourceId         uint
    ChainId          uint64
    Address          string            // contract that emitted the log
    Topics           []string
    Data             string            // hex, no 0x
    BlockNumber      uint64
    TransactionHash  string
    TransactionFrom  string
    TransactionIndex uint64
    BlockHash        string
    LogIndex         uint64
    Removed          bool              // true if the log was reorged out

    ContractName string               // decoded (empty for FULL/undecoded sources)
    EventName    string               // e.g. "Transfer"
    Args         map[string]string    // decoded event args, e.g. {"from":..., "to":..., "value":...}
}
```

---

## 2. Two rules you must follow

1. **`package main` + a `main` function.** `-buildmode=plugin` requires it. The
   `main` func is never executed — leave it empty.

2. **Be idempotent.** Delivery is **at-least-once**: after a crash EVMI replays
   the current block. If you see the same `LogEvent.Id` twice you must not
   double-count. Upsert on `Id` (or on `(blockNumber, logIndex)`), don't blind
   `INSERT`, and don't `+=` without a dedupe key.

---

## 3. Minimal example

```go
// package main is required for -buildmode=plugin.
package main

import (
    "fmt"

    exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

type counter struct {
    name  string
    total uint64
}

func (e *counter) Name() string { return "counter" }

func (e *counter) Init(ctx exporter.Context) error {
    e.name = ctx.ExporterName
    fmt.Printf("[%s] starting on chain %d\n", e.name, ctx.ChainId)
    return nil
}

func (e *counter) NewLogEvent(log exporter.LogEvent) error {
    e.total++
    return nil
}

func (e *counter) Close() error {
    fmt.Printf("[%s] saw %d logs\n", e.name, e.total)
    return nil
}

func New() exporter.Exporter { return &counter{} }

func main() {}
```

A runnable version lives at
[`examples/exporters/logcount`](examples/exporters/logcount/main.go).

---

## 4. Realistic example: ERC-20 balances into Postgres

This is the canonical use case: keep a live balance table by processing
`Transfer(address indexed from, address indexed to, uint256 value)`.

```go
package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "math/big"
    "strings"

    _ "github.com/lib/pq"
    exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

// config is decoded from the exporter's PluginConfig JSON, e.g.
// {"dsn":"postgres://...","token":"0xA0b8...","decimals":6}
type config struct {
    DSN   string `json:"dsn"`
    Token string `json:"token"`
}

type erc20Balances struct {
    cfg config
    db  *sql.DB
}

func (e *erc20Balances) Name() string { return "erc20-balances" }

func (e *erc20Balances) Init(ctx exporter.Context) error {
    if err := json.Unmarshal(ctx.Config, &e.cfg); err != nil {
        return fmt.Errorf("bad config: %w", err)
    }
    db, err := sql.Open("postgres", e.cfg.DSN)
    if err != nil {
        return err
    }
    e.db = db

    // Idempotency ledger: one row per processed log id.
    _, err = e.db.Exec(`
        CREATE TABLE IF NOT EXISTS balances (
            holder TEXT PRIMARY KEY,
            balance NUMERIC NOT NULL DEFAULT 0
        );
        CREATE TABLE IF NOT EXISTS processed_logs (id TEXT PRIMARY KEY);
    `)
    return err
}

func (e *erc20Balances) NewLogEvent(log exporter.LogEvent) error {
    // Only this token's Transfer events.
    if log.EventName != "Transfer" || !strings.EqualFold(log.Address, e.cfg.Token) {
        return nil
    }
    // A reorged-out log: skip (or reverse it if you track that).
    if log.Removed {
        return nil
    }

    from := log.Args["from"]
    to := log.Args["to"]
    value, ok := new(big.Int).SetString(log.Args["value"], 10)
    if !ok {
        return fmt.Errorf("bad value in log %s", log.Id)
    }

    tx, err := e.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Idempotency guard: if we've seen this log id, do nothing.
    res, err := tx.Exec(`INSERT INTO processed_logs(id) VALUES($1) ON CONFLICT DO NOTHING`, log.Id)
    if err != nil {
        return err
    }
    if n, _ := res.RowsAffected(); n == 0 {
        return tx.Commit() // already applied — safe replay
    }

    // Apply the transfer. (Skip the zero address for mints/burns as needed.)
    if _, err := tx.Exec(
        `INSERT INTO balances(holder, balance) VALUES($1, -$2)
         ON CONFLICT (holder) DO UPDATE SET balance = balances.balance - $2`,
        from, value.String()); err != nil {
        return err
    }
    if _, err := tx.Exec(
        `INSERT INTO balances(holder, balance) VALUES($1, $2)
         ON CONFLICT (holder) DO UPDATE SET balance = balances.balance + $2`,
        to, value.String()); err != nil {
        return err
    }
    return tx.Commit()
}

func (e *erc20Balances) Close() error {
    if e.db != nil {
        return e.db.Close()
    }
    return nil
}

func New() exporter.Exporter { return &erc20Balances{} }

func main() {}
```

The `processed_logs` guard + a single DB transaction per log is what makes the
at-least-once replay safe: re-seeing a log id is a no-op.

> The decoded `Args` keys (`from`, `to`, `value`) come straight from the contract
> ABI's event parameter names. If a parameter is unnamed in the ABI it will have
> an ABI-generated key — inspect a sample `LogEvent` if unsure.

---

## 5. Build it

Your plugin lives in its own module (or a subdirectory of one). Build a shared
object:

```bash
go build -buildmode=plugin -o erc20-balances.so ./path/to/plugin
```

> **Critical: version matching.** A `.so` only loads if it was built with the
> **same Go toolchain version** and the **same versions of every shared
> dependency** (at minimum `pkg/exporter`, and anything both sides import, like
> `go-ethereum`) as the running EVMI server. If they differ, `plugin.Open` fails.
> In your plugin's `go.mod`, pin:
>
> ```
> require github.com/evmi-cloud/go-evm-indexer v0.0.0  // match the server's version
> ```
>
> and build with the same `go` version. `CGO_ENABLED=1` is required
> (`-buildmode=plugin` needs it), and plugins only work on **Linux and macOS**.

---

## 6. Register it with EVMI

Registration is two steps: **install the plugin**, then **reference it from an
exporter**. In the web UI these are the **Plugins** and **Exporters** tabs.

### a. Install the plugin

A `Plugin` record holds the code source:

| field          | meaning                                                    |
|----------------|------------------------------------------------------------|
| `Name`         | display name (shown in the exporter's plugin picker)       |
| `LocalPath`    | path to your `.so`, **or** a module root for EVMI to build |
| `GitUrl`       | any git repo EVMI clones and builds (when no `.so` is given) |
| `RelativePath` | package to build within the module root                    |

Then **Install** it. EVMI resolves the source in this order and stores the result:

1. `LocalPath` ending in `.so` → used directly (you built it).
2. `GitUrl` set → cloned (any git repository), then built (`RelativePath` is the package).
3. `LocalPath` as a directory → treated as the module root and built.

Plugins can also be **declared in the server config** to be imported and installed
on startup — add a `plugins` array (each entry `{name, description, gitUrl,
relativePath}`); each is created if absent (matched by name) and installed.

Installing sets the plugin's status to `INSTALLED` (or `FAILED` with the build
error). Editing the source resets it to `NOT_INSTALLED` — reinstall to rebuild.

### b. Create an exporter that uses it

An `EvmiExporter` binds an installed plugin to a pipeline:

| field                        | meaning                                                     |
|------------------------------|-------------------------------------------------------------|
| `Name`                       | display name (passed to `Init` as `ExporterName`)           |
| `EvmLogPipelineID`           | the pipeline whose logs you receive                         |
| `PluginID`                   | the installed plugin to run                                 |
| `Enabled`                    | set `true` for EVMI to start it                             |
| `StartBlock`                 | first block to process                                      |
| `SyncBlock` / `SyncLogIndex` | resume cursor (server-managed; the exact last log executed) |
| `PluginConfig`               | JSON handed to your `Init` as `Context.Config`              |

An exporter only starts if its plugin is `INSTALLED`.

Example `PluginConfig` for the ERC-20 exporter above:

```json
{
    "dsn": "postgres://user:pass@localhost:5432/analytics?sslmode=disable",
    "token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
}
```

> There is not yet a gRPC/API endpoint to create exporters — rows are managed
> directly in the metadata database for now (see the roadmap in
> [docs/exporters.md](docs/exporters.md)).

---

## 7. How EVMI drives your plugin

- Logs arrive **in ascending `(block_number, log_index)` order** across all of
  the pipeline's sources.
- EVMI records your progress as a `(SyncBlock, SyncLogIndex)` cursor and persists
  it **after every log** it hands you, so a restart resumes at the exact next
  log — never replaying logs you already accepted.
- If `NewLogEvent` returns an error, the exporter stops and **that same log** is
  redelivered on the next run (everything before it is already committed). Return
  an error to signal "retry this"; return `nil` to accept and move on.
- Your plugin runs **inside the EVMI process**. A `panic` or a long block in your
  code affects the whole server — recover from panics you can anticipate, keep
  `NewLogEvent` fast, and do heavy/blocking work behind batching if needed.

---

## Checklist

- [ ] `package main` with an empty `func main() {}`
- [ ] implements `Name`, `Init`, `NewLogEvent`, `Close`
- [ ] exports `func New() exporter.Exporter`
- [ ] idempotent on `LogEvent.Id` (safe to replay)
- [ ] handles `Removed` logs deliberately
- [ ] built with the server's Go version + pinned dependency versions, `CGO_ENABLED=1`
- [ ] installed as a `Plugin` (status `INSTALLED`), then referenced by an
      `EvmiExporter` (with `PluginConfig`)
