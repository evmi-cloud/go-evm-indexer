package stores

import "github.com/evmi-cloud/go-evm-indexer/internal/types"

type EvmIndexerStorage interface {
	Init(config types.Config) error
	InsertLogs(logs []types.EvmLog) error
	InsertTransactions(txs []types.EvmTransaction) error
	UpdateSourceLatestBlock(sourceId string, latestBlock uint64) error
	GetStoreById(storeId string) (types.LogStore, error)
	GetStores() ([]types.LogStore, error)
	GetStoreGlobalLatestBlock(storeId string) (uint64, error)
	GetSources(storeId string) ([]types.LogSource, error)
	GetLogsCount() (uint64, error)
	GetLogsByStoreCount(storeId string) (uint64, error)
	GetLogs(storeId string, fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmLog, error)
	GetLatestLogs(storeId string, limit uint64) ([]types.EvmLog, error)
	GetTransactions(storeId string, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error)
	GetDatabaseDiskSize() (uint64, error)
}
