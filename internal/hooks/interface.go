package hooks

import "github.com/evmi-cloud/go-evm-indexer/internal/database/models"

type EvmIndexerHook interface {
	Init(config models.Config, index uint64) error
	PublishNewLogs(logs []models.EvmLog) error
}
