package stores

import "github.com/evmi-cloud/go-evm-indexer/internal/database/models"

type EvmIndexerStorage interface {
	Init(config models.Config) error
	InsertLogs(logs []models.EvmLog) error
	InsertTransactions(txs []models.EvmTransaction) error
	UpdateSourceLatestBlock(sourceId string, latestBlock uint64) error
	GetStoreById(storeId string) (models.LogStore, error)
	GetStores() ([]models.LogStore, error)
	GetStoreGlobalLatestBlock(storeId string) (uint64, error)
	GetSources(storeId string) ([]models.LogSource, error)
	GetLogsCount() (uint64, error)
	GetLogsByStoreCount(storeId string) (uint64, error)
	GetLogs(storeId string, fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]models.EvmLog, error)
	GetLatestLogs(storeId string, limit uint64) ([]models.EvmLog, error)
	GetTransactions(storeId string, fromBlock uint64, toBlock uint64) ([]models.EvmTransaction, error)
	GetDatabaseDiskSize() (uint64, error)
}
