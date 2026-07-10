package log_stores

import (
	"errors"

	clickhouse_store "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores/clickhouse"
	elasticsearch_store "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores/elasticsearch"
	mongodb_store "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores/mongodb"
	parquet_store "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores/parquet"
	sql_store "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores/sql"
	"github.com/rs/zerolog"
)

type IndexerStore struct {
	storage EvmIndexerStorage
}

func (store *IndexerStore) GetStorage() EvmIndexerStorage {
	return store.storage
}

// NewIndexerStore wraps an existing storage backend. Useful for tests that inject
// a fake EvmIndexerStorage without going through LoadStore.
func NewIndexerStore(storage EvmIndexerStorage) *IndexerStore {
	return &IndexerStore{storage: storage}
}

func LoadStore(storeType string, config map[string]string, logger zerolog.Logger) (*IndexerStore, error) {
	var storage EvmIndexerStorage
	var err error

	switch storeType {
	case "clickhouse":
		storage, err = clickhouse_store.NewClickHouseStore(logger)
	case "parquet":
		storage, err = parquet_store.NewParquetStore(logger)
	case "elasticsearch":
		storage, err = elasticsearch_store.NewElasticsearchStore(logger)
	case "mysql", "postgres":
		storage, err = sql_store.NewSQLStore(storeType, logger)
	case "mongodb":
		storage, err = mongodb_store.NewMongoStore(logger)
	default:
		return nil, errors.New("unknown store type: " + storeType)
	}
	if err != nil {
		return nil, err
	}

	if err := storage.Init(config); err != nil {
		return nil, err
	}
	return &IndexerStore{storage: storage}, nil
}
