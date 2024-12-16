package types

type EvmIndexerBackupExporter interface {
	ExportLogsToFile(localPath string, data []EvmLog) error
	ImportLogsFromFile(localPath string) ([]EvmLog, error)
	ExportTransactionsToFile(localPath string, data []EvmTransaction) error
	ImportTransactionsFromFile(localPath string) ([]EvmTransaction, error)
	ExportStateToFile(localPath string, data EvmIndexerBackupState) error
	ImportStateFromFile(localPath string) (EvmIndexerBackupState, error)
	ExportStateToBytes(data EvmIndexerBackupState) ([]byte, error)
	ImportStateFromBytes(content []byte) (EvmIndexerBackupState, error)
}
