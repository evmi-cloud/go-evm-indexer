package backup

type EvmIndexerBackupStore interface {
	Init() error
	Backup(path string, snapshotId string) error
	Restore(path string, snapshotId string) error
}
