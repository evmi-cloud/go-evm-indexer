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

and hands it to `exporter.Serve` from `main`:

```go
func main() { exporter.Serve(&myExporter{}) }
```

EVMI launches your plugin as a **subprocess** and calls it over gRPC
([hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)). Practically,
that means your plugin is an ordinary Go program: build it with a plain
`go build`, use whatever dependency versions you like, and a panic in your code
takes down only your process — not the indexer.

Your module needs one dependency for the SDK:

```bash
go get github.com/evmi-cloud/go-evm-indexer
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

## 2. Three rules you must follow

1. **`package main` whose `main` calls `exporter.Serve`.** `Serve` blocks until
   EVMI disconnects, then returns; do your setup in `Init`, not in `main`.

2. **Never print to stdout.** Stdout is the handshake and gRPC channel between
   EVMI and your plugin — writing to it corrupts the connection. Log to
   **stderr** instead (the standard library `log` package already does); EVMI
   captures it and forwards it to its own log, tagged with your plugin's name.

3. **Be idempotent.** Delivery is **at-least-once**: after a crash EVMI replays
   the current block. If you see the same `LogEvent.Id` twice you must not
   double-count. Upsert on `Id` (or on `(blockNumber, logIndex)`), don't blind
   `INSERT`, and don't `+=` without a dedupe key.

---

## 3. Minimal example

```go
package main

import (
    "log"

    exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

type counter struct {
    name  string
    total uint64
}

func (e *counter) Name() string { return "counter" }

func (e *counter) Init(ctx exporter.Context) error {
    e.name = ctx.ExporterName
    log.Printf("[%s] starting on chain %d", e.name, ctx.ChainId) // stderr, not stdout
    return nil
}

func (e *counter) NewLogEvent(l exporter.LogEvent) error {
    e.total++
    return nil
}

func (e *counter) Close() error {
    log.Printf("[%s] saw %d logs", e.name, e.total)
    return nil
}

func main() { exporter.Serve(&counter{}) }
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

func main() { exporter.Serve(&erc20Balances{}) }
```

The `processed_logs` guard + a single DB transaction per log is what makes the
at-least-once replay safe: re-seeing a log id is a no-op.

> The decoded `Args` keys (`from`, `to`, `value`) come straight from the contract
> ABI's event parameter names. If a parameter is unnamed in the ABI it will have
> an ABI-generated key — inspect a sample `LogEvent` if unsure.

---

## 5. Build it

Your plugin is its own Go module with `main` at the **repo root** (that is the
path EVMI builds). It is an ordinary program:

```bash
go build -o erc20-balances .
```

Any Go toolchain, any dependency versions — the plugin is a normal program.

The one compatibility rule: EVMI rejects a plugin built against an **incompatible
SDK protocol version** at startup, with a handshake error naming the mismatch. If
that happens, update the SDK dependency and rebuild.

In practice you rarely build by hand at all — push the plugin to a git repository
and let EVMI clone and build it (next section).

---

## 6. Register it with EVMI

Registration is two steps: **install the plugin**, then **reference it from an
exporter**. In the web UI these are the **Plugins** and **Exporters** tabs.

### a. Install the plugin

A `Plugin` record holds the code source:

| field     | meaning                                                       |
|-----------|---------------------------------------------------------------|
| `Name`    | display name (shown in the exporter's plugin picker)          |
| `GitUrl`  | git repo EVMI clones and builds — **the only source**         |
| `GitRef`  | optional branch or tag (empty = the repo's default branch)    |

Then **Install** it: EVMI clones `GitUrl` at `GitRef` and runs `go build` on the
**repo root** (the plugin's `main` package must live there — there is no package
path to set), storing the resulting executable. Git is the only supported source,
and the build happens on the instance, so it needs network access to fetch your
module's dependencies.

Plugins can also be **declared in the server config** to be imported and installed
on startup — add a `plugins` array (each entry `{name, description, gitUrl,
gitRef}`); each is created if absent (matched by name) and installed.

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

> Exporters are managed over the Connect API (`CreateEvmiExporter`,
> `UpdateEvmiExporter`, `StartExporter`, `StopExporter`, …) and from the web UI's
> **Exporters** tab.

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
- Your plugin runs **in its own process**, started when the exporter starts and
  killed when it stops. A panic kills only your plugin: the exporter is restarted
  by the supervisor and replays from the last committed cursor. Nothing else on
  the server is affected.
- Calls are **strictly sequential** — one `NewLogEvent` at a time — so your
  plugin does not need to be goroutine-safe.
- Each delivered log costs one local gRPC round trip, so keep `NewLogEvent`
  cheap: buffer or batch expensive work inside the plugin and flush it in
  `Close`.

---

## Checklist

- [ ] `package main` whose `main` calls `exporter.Serve(&myExporter{})`
- [ ] implements `Name`, `Init`, `NewLogEvent`, `Close`
- [ ] logs to stderr only — nothing written to stdout
- [ ] idempotent on `LogEvent.Id` (safe to replay)
- [ ] handles `Removed` logs deliberately
- [ ] builds with a plain `go build` from the repo root
- [ ] installed as a `Plugin` (status `INSTALLED`), then referenced by an
      `EvmiExporter` (with `PluginConfig`)
