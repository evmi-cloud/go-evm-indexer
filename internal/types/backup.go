package types

type EvmIndexerBackupState struct {
	FromBlock uint64
	ToBlock   uint64

	FileList []EvmIndexerBackupFile
}

type EvmIndexerBackupFile struct {
	Identifier string
	FromBlock  uint64
	ToBlock    uint64
}

type EvmIndexerBackupStorage interface {
	Init() error
	GetState() (EvmIndexerBackupState, error)
	UpdateState(state EvmIndexerBackupState) error
	DownloadFile(remotePath string, localPath string) error
	UploadFile(localPath string, remotePath string) error
}
