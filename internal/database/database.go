package database

import (
	"os"

	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
	"github.com/evmi-cloud/go-evm-indexer/internal/database/stores"
	"github.com/evmi-cloud/go-evm-indexer/internal/database/stores/clover"
	"github.com/rs/zerolog"
)

type IndexerDatabase struct {
	store stores.EvmIndexerStorage
}

func (db *IndexerDatabase) GetStoreDatabase() (stores.EvmIndexerStorage, error) {
	return db.store, nil
}

func LoadDatabase(config models.Config, logger zerolog.Logger) (*IndexerDatabase, error) {

	db := &IndexerDatabase{}

	if config.Storage.Type == "clover" {
		if err := os.MkdirAll(config.Storage.Config["path"], os.ModePerm); err != nil {
			logger.Fatal().Msg(err.Error())
		}

		store, err := clover.NewCloverStore(config.Storage.Config["path"], logger)
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
