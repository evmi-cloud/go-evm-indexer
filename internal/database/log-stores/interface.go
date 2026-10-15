package log_stores

import "github.com/evmi-cloud/go-evm-indexer/internal/types"

type EvmIndexerStorage interface {
	Init(config map[string]string) error
	InsertLogs(logs []types.EvmLog) error
	InsertTransactions(txs []types.EvmTransaction) error
	GetLogsCount() (uint64, error)
	// DeleteSourceData removes all stored logs and transactions for the given
	// source. Used when a source (or a factory-spawned child) is deleted. Deleting
	// data for a source with nothing stored is a no-op (not an error).
	DeleteSourceData(sourceId uint64) error
	GetLogs(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmLog, error)
	// GetLogsAfter returns logs for the given sources up to and including toBlock,
	// strictly after the (afterBlock, afterLogIndex) cursor, ordered by
	// (block_number, log_index). A log qualifies when block > afterBlock, or
	// block == afterBlock and log_index > afterLogIndex.
	GetLogsAfter(sourceIds []uint64, afterBlock uint64, afterLogIndex uint64, toBlock uint64) ([]types.EvmLog, error)
	GetLogStream(sourceId uint64, fromBlock uint64, toBlock uint64, stream chan types.EvmLog) error
	GetLatestLogs(sourceId uint64, limit uint64) ([]types.EvmLog, error)
	GetTransactions(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error)
	// Close releases the store's connections. A store is opened by the
	// service that uses it (one per indexer source, one per exporter) and
	// must be closed when that service stops: a supervisor that restarts a
	// failing service would otherwise open a fresh pool at every attempt and
	// leave the previous ones idling until a managed database's connection
	// cap is hit. Closing twice, or a store that never connected, is a no-op.
	Close() error
}
