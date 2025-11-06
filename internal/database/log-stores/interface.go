package log_stores

import "github.com/evmi-cloud/go-evm-indexer/internal/types"

type EvmIndexerStorage interface {
	Init(config map[string]string) error
	InsertLogs(logs []types.EvmLog) error
	InsertTransactions(txs []types.EvmTransaction) error
	GetLogsCount() (uint64, error)
	GetLogs(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmLog, error)
	GetLogStream(sourceId uint64, fromBlock uint64, toBlock uint64, stream chan types.EvmLog) error
	GetLatestLogs(sourceId uint64, limit uint64) ([]types.EvmLog, error)
	GetTransactions(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error)
}
