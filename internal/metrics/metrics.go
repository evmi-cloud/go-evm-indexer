package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	logsCountMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_logs_count",
		Help: "The total number of logs stored",
	}, []string{
		"storeIdentifier",
	})

	logsScrapedMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_logs_scraped",
		Help: "The total number of logs scraped",
	}, []string{
		"storeIdentifier",
		"chainId",
	})

	latestBlockIndexedMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_latest_block_indexed",
		Help: "The total number of logs scraped",
	}, []string{
		"storeIdentifier",
		"sourceName",
		"chainId",
	})

	latestChainBlockMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evm_indexer_chain_latest_block_number",
		Help: "The total number of logs scraped",
	}, []string{
		"chainId",
	})

	storeDiskSizeMetrics = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evm_indexer_store_disk_size",
		Help: "The total number of logs scraped",
	})

	evmRpcCallMetrics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evm_indexer_evm_rpc_call",
		Help: "The total number of logs scraped",
	}, []string{
		"type",
		"storeIdentifier",
		"chainId",
	})
)
