package log_stores

import "github.com/evmi-cloud/go-evm-indexer/internal/types"

type EvmIndexerStorage interface {
	Init(config map[string]string) error
	InsertLogs(logs []types.EvmLog) error
	InsertTransactions(txs []types.EvmTransaction) error
	GetLogsCount() (uint64, error)
	GetLogs(fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmLog, error)
	GetLatestLogs(limit uint64) ([]types.EvmLog, error)
	GetTransactions(fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmTransaction, error)
}
