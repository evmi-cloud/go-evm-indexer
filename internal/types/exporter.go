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

// ExporterCursorUpdate is the payload of the exporter.cursor bus topic: one
// exporter's export position for one of its pipeline's log sources, enriched with
// that source's own state.
//
// Export progress is per source (EvmiExporterSourceCursor rows), so this — not
// the exporter's aggregate SyncBlock — is what actually describes an exporter's
// progress, and it is what the UI's exporter detail view renders live. The source
// fields ride along because the emitter (the export loop) has just read the source
// row anyway, which saves every stream subscriber a lookup per event.
type ExporterCursorUpdate struct {
	ExporterID uint
	SourceID   uint

	// SyncBlock is the last fully-exported block of the source; SyncLogIndex is
	// the last log_index delivered within SyncBlock+1, or -1 when none is.
	SyncBlock    uint64
	SyncLogIndex int64

	// SourceSyncBlock is how far the indexer has stored the source — the block
	// the exporter is catching up to.
	SourceSyncBlock uint64

	SourceType     string
	SourceAddress  string
	SourceTopic0   string
	SourceEnabled  bool
	SourceStatus   string
	ParentSourceID uint
}

// LagBlocks is how far this source's export trails its indexing, clamped at 0
// (the cursor can sit at the head, and a seeded cursor can briefly sit above it).
func (u ExporterCursorUpdate) LagBlocks() uint64 {
	if u.SourceSyncBlock <= u.SyncBlock {
		return 0
	}
	return u.SourceSyncBlock - u.SyncBlock
}
