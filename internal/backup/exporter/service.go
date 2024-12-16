package exporter

import (
	"errors"

	jsonexporter "github.com/evmi-cloud/go-evm-indexer/internal/backup/exporter/json"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
)

type BackupExporterService struct {
	exporter types.EvmIndexerBackupExporter
}

// ExportStateToBytes implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ExportStateToBytes(data types.EvmIndexerBackupState) ([]byte, error) {
	return b.exporter.ExportStateToBytes(data)
}

// ImportStateFromBytes implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ImportStateFromBytes(content []byte) (types.EvmIndexerBackupState, error) {
	return b.exporter.ImportStateFromBytes(content)
}

// ExportLogsToFile implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ExportLogsToFile(localPath string, data []types.EvmLog) error {
	return b.exporter.ExportLogsToFile(localPath, data)
}

// ExportStateToFile implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ExportStateToFile(localPath string, data types.EvmIndexerBackupState) error {
	return b.exporter.ExportStateToFile(localPath, data)
}

// ExportTransactionsToFile implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ExportTransactionsToFile(localPath string, data []types.EvmTransaction) error {
	return b.exporter.ExportTransactionsToFile(localPath, data)
}

// ImportLogsFromFile implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ImportLogsFromFile(localPath string) ([]types.EvmLog, error) {
	return b.exporter.ImportLogsFromFile(localPath)
}

// ImportStateFromFile implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ImportStateFromFile(localPath string) (types.EvmIndexerBackupState, error) {
	return b.exporter.ImportStateFromFile(localPath)
}

// ImportTransactionsFromFile implements types.EvmIndexerBackupExporter.
func (b BackupExporterService) ImportTransactionsFromFile(localPath string) ([]types.EvmTransaction, error) {
	return b.exporter.ImportTransactionsFromFile(localPath)
}

func NewBackupExporterService(exporterType string) (BackupExporterService, error) {
	if exporterType == "json" {
		return BackupExporterService{
			exporter: jsonexporter.NewEvmJSONBackupExporter(),
		}, nil
	}

	return BackupExporterService{}, errors.New("unknown exporter type: " + exporterType)
}
