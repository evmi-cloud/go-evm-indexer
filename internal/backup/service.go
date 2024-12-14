package backup

import (
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

type BackupService struct {
	logger zerolog.Logger
	config types.Config

	store    types.EvmIndexerBackupStorage
	exporter types.EvmIndexerBackupExporter
}

func (h *BackupService) Start() {

	h.logger.Info().Msg("Backup service started")
}

func NewBackupService(config types.Config, logger zerolog.Logger) (*BackupService, error) {

	service := &BackupService{
		logger: logger,
		config: config,
	}

	return service, nil
}
