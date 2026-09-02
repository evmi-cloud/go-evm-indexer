package indexer

import (
	"context"
	"math/big"
	"sync/atomic"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/rs/zerolog"
)

// HeadWatcher polls the chain head once per PullInterval on behalf of every
// source of a blockchain. Without it each source polled eth_blockNumber on
// its own: N sources meant N identical requests per interval for one answer.
//
// It is a suture service owned by the IndexerService, which shares one
// watcher per blockchain among the sources of that chain and retires it when
// the last source is removed.
type HeadWatcher struct {
	chain    evmi_database.EvmBlockchain
	interval time.Duration
	metrics  *metrics.MetricService
	logger   zerolog.Logger

	// head is the latest block number; 0 until the first successful read.
	head atomic.Uint64
	// refs counts the sources relying on this watcher.
	refs atomic.Int32
}

func NewHeadWatcher(chain evmi_database.EvmBlockchain, m *metrics.MetricService, logger zerolog.Logger) *HeadWatcher {
	interval := time.Duration(chain.PullInterval) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	return &HeadWatcher{chain: chain, interval: interval, metrics: m, logger: logger}
}

// Head returns the latest known block number and whether one is known yet.
func (h *HeadWatcher) Head() (uint64, bool) {
	n := h.head.Load()
	return n, n > 0
}

// Serve polls eth_blockNumber every interval until ctx is canceled. A failed
// read keeps the last known head: sources simply see no progress until the
// next successful read.
func (h *HeadWatcher) Serve(ctx context.Context) error {
	client, err := w3.Dial(h.chain.RpcUrl)
	if err != nil {
		return err
	}
	defer client.Close()

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}

		var block *big.Int
		start := time.Now()
		err := client.Call(eth.BlockNumber().Returns(&block))
		if h.metrics != nil {
			h.metrics.RecordRPC(h.chain.ChainId, "eth_blockNumber", time.Since(start), err)
		}
		if err != nil {
			h.logger.Warn().Err(err).Msg("head watcher: eth_blockNumber failed")
		} else {
			h.head.Store(block.Uint64())
			if h.metrics != nil {
				h.metrics.SetChainHead(h.chain.ChainId, block.Uint64())
			}
		}
		timer.Reset(h.interval)
	}
}
