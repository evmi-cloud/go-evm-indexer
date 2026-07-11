package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Label sets, kept as constants so the metric declarations and the value slices
// in service.go stay in sync (same names, same order).
var (
	// Per-source metrics. source_id + source_type identify a source (sources have
	// no name; factory children are distinguished by their DB id).
	sourceLabelNames = []string{"chain_id", "pipeline", "store", "source_id", "source_type"}
	// Per-exporter metrics.
	exporterLabelNames = []string{"chain_id", "pipeline", "exporter"}
)

// durationBuckets covers sub-millisecond RPC/store calls up to slow (10s+) ones.
var durationBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

var (
	// --- chain ---

	chainHeadBlockMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_chain_head_block",
		Help: "Latest block number observed on the chain (eth_blockNumber).",
	}, []string{"chain_id"})

	// --- per source: progress ---

	sourceSyncedBlockMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_source_synced_block",
		Help: "Last block fully indexed by a source (its persisted cursor).",
	}, sourceLabelNames)

	sourceLagBlocksMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_source_lag_blocks",
		Help: "How many blocks a source is behind the chain head (head - synced, clamped at 0).",
	}, sourceLabelNames)

	sourceUpMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_source_up",
		Help: "1 when a source's indexer loop is running, 0 when stopped.",
	}, sourceLabelNames)

	// --- per source: throughput ---

	logsIndexedMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_logs_indexed_total",
		Help: "Total decoded logs written to the store by a source.",
	}, sourceLabelNames)

	transactionsIndexedMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_transactions_indexed_total",
		Help: "Total transactions written to the store by a source.",
	}, sourceLabelNames)

	batchDurationMetrics = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evm_indexer_batch_duration_seconds",
		Help:    "Wall-clock time to fetch, decode and store one block range for a source.",
		Buckets: durationBuckets,
	}, sourceLabelNames)

	// --- store ---

	logsStoredMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_logs_stored",
		Help: "Running count of logs written to a store since process start.",
	}, []string{"store"})

	storeWriteDurationMetrics = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evm_indexer_store_write_duration_seconds",
		Help:    "Duration of a store write, by operation (logs / transactions).",
		Buckets: durationBuckets,
	}, []string{"store", "operation"})

	storeWriteErrorsMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_store_write_errors_total",
		Help: "Total failed store writes, by operation.",
	}, []string{"store", "operation"})

	storeDiskBytesMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_store_disk_bytes",
		Help: "On-disk size of a store's data, when the backend can report it (e.g. parquet).",
	}, []string{"store"})

	// --- rpc ---

	rpcRequestsMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_rpc_requests_total",
		Help: "Total JSON-RPC requests, by method and outcome (status = ok | error).",
	}, []string{"chain_id", "method", "status"})

	rpcDurationMetrics = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evm_indexer_rpc_request_duration_seconds",
		Help:    "JSON-RPC request latency, by method.",
		Buckets: durationBuckets,
	}, []string{"chain_id", "method"})

	// --- exporter ---

	exporterSyncedBlockMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_exporter_synced_block",
		Help: "Last block an exporter has fully delivered to its plugin.",
	}, exporterLabelNames)

	exporterLagBlocksMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_exporter_lag_blocks",
		Help: "How many blocks an exporter is behind the export head (head - synced, clamped at 0).",
	}, exporterLabelNames)

	exporterUpMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_exporter_up",
		Help: "1 when an exporter's loop is running, 0 when stopped.",
	}, exporterLabelNames)

	exporterEventsMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_exporter_events_total",
		Help: "Total log events delivered to an exporter's plugin.",
	}, exporterLabelNames)

	exporterErrorsMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_exporter_errors_total",
		Help: "Total exporter failures (plugin or store errors that restart the loop).",
	}, exporterLabelNames)

	exporterProcessDurationMetrics = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evm_indexer_exporter_process_duration_seconds",
		Help:    "Wall-clock time to export one block range (fetch + plugin delivery).",
		Buckets: durationBuckets,
	}, exporterLabelNames)
)
