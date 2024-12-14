package exporter

import (
	"errors"

	jsonexporter "github.com/evmi-cloud/go-evm-indexer/internal/backup/exporter/json"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
)

type BackupExporterService struct {
	Exporter types.EvmIndexerBackupExporter
}

func NewBackupExporterService(exporterType string) (BackupExporterService, error) {
	if exporterType == "json" {
		return BackupExporterService{
			Exporter: jsonexporter.NewEvmJSONBackupExporter(),
		}, nil
	}

	return BackupExporterService{}, errors.New("unknown exporter type: " + exporterType)
}
