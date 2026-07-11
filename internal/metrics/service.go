package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// SourceLabels identifies a source across all per-source metrics. Sources have no
// name, so a source is pinned by its DB id + type (factory children share a type
// but have distinct ids).
type SourceLabels struct {
	ChainID    uint64
	Pipeline   string
	Store      string
	SourceID   uint
	SourceType string
}

func (l SourceLabels) values() []string {
	return []string{fmt.Sprint(l.ChainID), l.Pipeline, l.Store, fmt.Sprint(l.SourceID), l.SourceType}
}

// ExporterLabels identifies an exporter across all per-exporter metrics.
type ExporterLabels struct {
	ChainID  uint64
	Pipeline string
	Exporter string
}

func (l ExporterLabels) values() []string {
	return []string{fmt.Sprint(l.ChainID), l.Pipeline, l.Exporter}
}

type MetricService struct {
	enabled bool
	path    string
	port    uint64
	logger  zerolog.Logger
}

// Start serves the Prometheus registry on its own mux so it never collides with
// the gRPC/web mux, logging (rather than swallowing) a listener failure.
func (h *MetricService) Start() {
	if h == nil || !h.enabled {
		return
	}
	mux := http.NewServeMux()
	mux.Handle(h.path, promhttp.Handler())
	addr := ":" + fmt.Sprint(h.port)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			h.logger.Error().Msg("metrics server stopped: " + err.Error())
		}
	}()
	h.logger.Info().Msg("metrics listening on " + addr + h.path)
}

// --- chain ---

func (h *MetricService) SetChainHead(chainID, block uint64) {
	if h == nil || !h.enabled {
		return
	}
	chainHeadBlockMetrics.WithLabelValues(fmt.Sprint(chainID)).Set(float64(block))
}

// --- source ---

// SetSourceProgress records a source's cursor and its lag behind the chain head
// in one call (lag is clamped at 0 in case the head reading is momentarily stale).
func (h *MetricService) SetSourceProgress(l SourceLabels, head, synced uint64) {
	if h == nil || !h.enabled {
		return
	}
	v := l.values()
	sourceSyncedBlockMetrics.WithLabelValues(v...).Set(float64(synced))
	sourceLagBlocksMetrics.WithLabelValues(v...).Set(float64(lag(head, synced)))
}

func (h *MetricService) SetSourceUp(l SourceLabels, up bool) {
	if h == nil || !h.enabled {
		return
	}
	sourceUpMetrics.WithLabelValues(l.values()...).Set(boolToFloat(up))
}

// AddLogsIndexed bumps both the per-source counter and the per-store gauge.
func (h *MetricService) AddLogsIndexed(l SourceLabels, count uint64) {
	if h == nil || !h.enabled || count == 0 {
		return
	}
	logsIndexedMetrics.WithLabelValues(l.values()...).Add(float64(count))
	logsStoredMetrics.WithLabelValues(l.Store).Add(float64(count))
}

func (h *MetricService) AddTransactionsIndexed(l SourceLabels, count uint64) {
	if h == nil || !h.enabled || count == 0 {
		return
	}
	transactionsIndexedMetrics.WithLabelValues(l.values()...).Add(float64(count))
}

func (h *MetricService) ObserveBatchDuration(l SourceLabels, d time.Duration) {
	if h == nil || !h.enabled {
		return
	}
	batchDurationMetrics.WithLabelValues(l.values()...).Observe(d.Seconds())
}

// --- store ---

// ObserveStoreWrite records the duration of a store write and, on error, bumps the
// per-operation error counter. operation is "logs" or "transactions".
func (h *MetricService) ObserveStoreWrite(store, operation string, d time.Duration, err error) {
	if h == nil || !h.enabled {
		return
	}
	storeWriteDurationMetrics.WithLabelValues(store, operation).Observe(d.Seconds())
	if err != nil {
		storeWriteErrorsMetrics.WithLabelValues(store, operation).Inc()
	}
}

func (h *MetricService) SetStoreDiskBytes(store string, bytes uint64) {
	if h == nil || !h.enabled {
		return
	}
	storeDiskBytesMetrics.WithLabelValues(store).Set(float64(bytes))
}

// --- rpc ---

// RecordRPC records one JSON-RPC call's latency and outcome. status is derived
// from err (ok / error).
func (h *MetricService) RecordRPC(chainID uint64, method string, d time.Duration, err error) {
	if h == nil || !h.enabled {
		return
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	chain := fmt.Sprint(chainID)
	rpcRequestsMetrics.WithLabelValues(chain, method, status).Inc()
	rpcDurationMetrics.WithLabelValues(chain, method).Observe(d.Seconds())
}

// --- exporter ---

func (h *MetricService) SetExporterProgress(l ExporterLabels, head, synced uint64) {
	if h == nil || !h.enabled {
		return
	}
	v := l.values()
	exporterSyncedBlockMetrics.WithLabelValues(v...).Set(float64(synced))
	exporterLagBlocksMetrics.WithLabelValues(v...).Set(float64(lag(head, synced)))
}

func (h *MetricService) SetExporterUp(l ExporterLabels, up bool) {
	if h == nil || !h.enabled {
		return
	}
	exporterUpMetrics.WithLabelValues(l.values()...).Set(boolToFloat(up))
}

func (h *MetricService) AddExporterEvents(l ExporterLabels, count uint64) {
	if h == nil || !h.enabled || count == 0 {
		return
	}
	exporterEventsMetrics.WithLabelValues(l.values()...).Add(float64(count))
}

func (h *MetricService) IncExporterErrors(l ExporterLabels) {
	if h == nil || !h.enabled {
		return
	}
	exporterErrorsMetrics.WithLabelValues(l.values()...).Inc()
}

func (h *MetricService) ObserveExporterProcess(l ExporterLabels, d time.Duration) {
	if h == nil || !h.enabled {
		return
	}
	exporterProcessDurationMetrics.WithLabelValues(l.values()...).Observe(d.Seconds())
}

func NewMetricService(enabled bool, path string, port uint64, logger zerolog.Logger) *MetricService {
	return &MetricService{
		enabled: enabled,
		path:    path,
		port:    port,
		logger:  logger,
	}
}

func lag(head, synced uint64) uint64 {
	if head <= synced {
		return 0
	}
	return head - synced
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
