package log_stores

import (
	"errors"

	clickhouse_store "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores/clickhouse"
	"github.com/rs/zerolog"
)

type IndexerStore struct {
	storage EvmIndexerStorage
}

func (store *IndexerStore) GetStorage() EvmIndexerStorage {
	return store.storage
}

func LoadStore(storeType string, config map[string]string, logger zerolog.Logger) (*IndexerStore, error) {
	if storeType == "clickhouse" {

		storage, err := clickhouse_store.NewClickHouseStore(logger)
		if err != nil {
			return nil, err
		}

		err = storage.Init(config)
		if err != nil {
			return nil, err
		}

		return &IndexerStore{
			storage: storage,
		}, nil

	}

	return nil, errors.New("unknown store type")
}
