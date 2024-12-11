package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricService struct {
	enabled bool
	path    string
	port    uint64

	logsScrapedMetrics *prometheus.CounterVec
	evmRpcCallMetrics  *prometheus.CounterVec

	logsCountMetrics          *prometheus.GaugeVec
	latestBlockIndexedMetrics *prometheus.GaugeVec
	latestChainBlockMetrics   *prometheus.GaugeVec
	storeDiskSizeMetrics      prometheus.Gauge
}

func (h *MetricService) Start() {
	if h.enabled {
		http.Handle(h.path, promhttp.Handler())
		go http.ListenAndServe(":"+fmt.Sprint(h.port), nil)
	}
}

func (h *MetricService) LogsScrapedMetricsInc(storeIdentifier string, chainId uint64, value uint64) {
	if h.enabled {
		h.logsScrapedMetrics.WithLabelValues(storeIdentifier, fmt.Sprint(chainId)).Add(float64(value))
	}
}

func (h *MetricService) EvmRpcCallMetricsInc(storeIdentifier string, chainId uint64, callType string, value uint64) {
	if h.enabled {
		h.evmRpcCallMetrics.WithLabelValues(callType, storeIdentifier, fmt.Sprint(chainId)).Add(float64(value))
	}
}

func (h *MetricService) LogsCountMetricsSet(storeIdentifier string, value uint64) {
	if h.enabled {
		h.logsCountMetrics.WithLabelValues(storeIdentifier).Set(float64(value))
	}
}

func (h *MetricService) LogsCountMetricsAdd(storeIdentifier string, value uint64) {
	if h.enabled {
		h.logsCountMetrics.WithLabelValues(storeIdentifier).Add(float64(value))
	}
}

func (h *MetricService) LatestBlockIndexedMetricsSet(storeIdentifier string, sourceName string, chainId uint64, value uint64) {
	if h.enabled {
		h.latestBlockIndexedMetrics.WithLabelValues(storeIdentifier, sourceName, fmt.Sprint(chainId)).Set(float64(value))
	}
}

func (h *MetricService) LatestChainBlockMetricsSet(chainId uint64, value uint64) {
	if h.enabled {
		h.latestChainBlockMetrics.WithLabelValues(fmt.Sprint(chainId)).Set(float64(value))
	}
}

func (h *MetricService) StoreDiskSizeMetricsSet(value uint64) {
	if h.enabled {
		h.storeDiskSizeMetrics.Set(float64(value))
	}
}

func NewMetricService(enabled bool, path string, port uint64) *MetricService {

	service := &MetricService{
		enabled: enabled,
		path:    path,
		port:    port,

		logsCountMetrics:          logsCountMetrics,
		logsScrapedMetrics:        logsScrapedMetrics,
		latestBlockIndexedMetrics: latestBlockIndexedMetrics,
		latestChainBlockMetrics:   latestChainBlockMetrics,
		evmRpcCallMetrics:         evmRpcCallMetrics,
		storeDiskSizeMetrics:      storeDiskSizeMetrics,
	}

	return service
}
