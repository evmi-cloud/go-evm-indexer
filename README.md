<p align="center">
  <img src="public/EVMI_LOGO_WHITE_TRANSPARENT.png" width="300px" alt="EVMI"/>
</p>

# EVM Indexer

EVMI is a powerful and flexible Ethereum Virtual Machine (EVM) log indexing service that helps you capture, store, and process blockchain events efficiently.

## Features

- **Multi-Chain Support**: Index logs from any EVM-compatible blockchain over JSON-RPC
- **ABI Decoding**: Decode events into structured metadata using contract ABIs
- **Multiple Source Types**: Index a whole chain, a single contract, a topic filter, or a
  factory (auto-tracking every contract it deploys)
- **ClickHouse Storage**: Persist decoded logs and transactions to ClickHouse behind a
  pluggable store interface
- **Runtime API**: Manage blockchains, ABIs, stores, pipelines, and sources live over a
  gRPC/Connect API — no restart required
- **Prometheus Metrics**: Indexing progress, RPC call counts, and log counts

## Getting Started

EVMI can be used for multiple use cases:
- Smart contract log storage and indexing into ClickHouse
- Structured, ABI-decoded event data for analytics and downstream processing

### Installation

```bash
# Using Docker
docker pull evmicloud/go-evm-indexer
docker run -p 8080:8080 evmicloud/go-evm-indexer

# From source
go install github.com/evmi-cloud/go-evm-indexer/cmd/evm-indexer@latest
```

### Configuration

The configuration file only sets up the **metadata database** (where indexing topology and
progress are stored) and metrics. The indexing topology itself — blockchains, ABIs, log
stores, pipelines, and sources — is created and managed at runtime through the gRPC/Connect
API, not in this file.

Create a configuration file (e.g., `config.json`):

```json
{
    "database": {
        "type": "SQLITE",
        "config": {
            "filename": "evmi-database.sqlite"
        }
    },
    "metrics": {
        "enabled": true,
        "path": "/metrics",
        "port": 9090
    }
}
```

`database.type` is one of `SQLITE`, `POSTGRES`, or `MYSQL`. SQLite reads `config.filename`;
Postgres and MySQL read a `config.dsn` connection string. See `configs/` for full examples.

Run it with:

```bash
evm-indexer start --config config.json --instance EVMI_INSTANCE_1
```

#### Log stores

A **log store** is where decoded logs and transactions are written. Log stores are created
via the API as `EvmLogStore` records, each with a `storeType` and a JSON `storeConfig`. The
`clickhouse` store type expects:

```json
{
    "addr": "localhost:9000",
    "database": "evmi_cloud",
    "username": "default",
    "password": "secret",
    "logsTableName": "logs",
    "transactionsTableName": "transactions"
}
```

(`addr` may be a comma-separated list for multiple nodes.)

## Components

### Metadata database

Holds the indexing topology (blockchains, ABIs, stores, pipelines, sources) and per-source
sync progress. Managed with GORM; `database.type` selects the backend:
- **SQLite** — single-file, good for local/dev
- **PostgreSQL**
- **MySQL**

### Log stores

Where decoded logs and transactions are written, selected per store via `storeType`:
- **ClickHouse** — the only implemented backend today

Additional backends can be added by implementing the `EvmIndexerStorage` interface in
`internal/database/log-stores`.

### API

A gRPC/Connect API (`EvmIndexerService`) is served on `:8080` (HTTP/2 cleartext, h2c). All
topology — blockchains, ABIs, stores, pipelines, and sources — is created and managed here,
including starting and stopping individual source indexers at runtime.

Every RPC requires a bearer token except the public `Login` and `GetOAuthLoginUrl` RPCs. Call
`Login` to obtain a token; a default `admin`/`admin` user is seeded on first startup (change it
immediately). Users can mint long-lived API keys and admins can configure OAuth2/OIDC login —
see [AUTH.md](AUTH.md).

### Metrics

When enabled, Prometheus metrics are exposed on the configured `metrics.port` and
`metrics.path`. A ready-made Prometheus + Grafana stack is included in `docker-compose.yml`.

### Exporters (custom plugins)

Exporters run user-written Go plugins over indexed data: the server calls a
plugin's `NewLogEvent` for every stored log, in block order, and tracks each
exporter's sync progress so it resumes after a restart — e.g. a plugin that
maintains ERC-20 balances in your own database. Plugins implement the
`github.com/evmi-cloud/go-evm-indexer/pkg/exporter` interface and are compiled
with `-buildmode=plugin`. See [docs/exporters.md](docs/exporters.md) for the
authoring guide and the important native-plugin constraints (toolchain/version
matching, CGO, no process isolation).

### Web UI

A Next.js app in [`webui/`](webui/) provides a login + control panel. It is built as a
static export and served by the Go server at `/` (from the directory in `EVMI_WEBUI_DIR`,
default `public`). The Docker build compiles it automatically; for local development see
[`webui/README.md`](webui/README.md). API and auth routes take precedence over the static
handler, so the SPA never shadows them.

## Roadmap

The following are planned and **not yet implemented**:
- Event streaming to message brokers (Redis PubSub, Kafka, Webhooks)
- Analytics export (JSON / Apache Parquet) to S3, Google Cloud Storage, or IPFS
- A richer web UI (pipeline management, monitoring) beyond the current scaffold
- Additional log-store backends

## Documentation

For detailed documentation, visit our [documentation site](https://docs.evmi.dev) (coming soon).

## License

MIT License - see LICENSE file for details.
