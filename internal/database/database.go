package database

import (
	"os"

	"github.com/evmi-cloud/go-evm-indexer/internal/database/stores"
	clickhouse_store "github.com/evmi-cloud/go-evm-indexer/internal/database/stores/clickhouse"
	clover_store "github.com/evmi-cloud/go-evm-indexer/internal/database/stores/clover"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

type IndexerDatabase struct {
	store stores.EvmIndexerStorage
}

func (db *IndexerDatabase) GetStoreDatabase() (stores.EvmIndexerStorage, error) {
	return db.store, nil
}

func LoadDatabase(config types.Config, logger zerolog.Logger) (*IndexerDatabase, error) {

	db := &IndexerDatabase{}

	if config.Storage.Type == "clover" {
		if err := os.MkdirAll(config.Storage.Config["path"], os.ModePerm); err != nil {
			logger.Fatal().Msg(err.Error())
		}

		store, err := clover_store.NewCloverStore(config.Storage.Config["path"], logger)
		if err != nil {
			return nil, err
		}

		db.store = store
		err = db.store.Init(config)
		if err != nil {
			return nil, err
		}
	}

	if config.Storage.Type == "clickhouse" {
		store, err := clickhouse_store.NewClickHouseStore(logger)
		if err != nil {
			return nil, err
		}

		db.store = store
		err = db.store.Init(config)
		if err != nil {
			return nil, err
		}
	}

	return db, nil
}
