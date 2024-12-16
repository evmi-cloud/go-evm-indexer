package types

type EvmIndexerBackupState struct {
	FromBlock uint64
	ToBlock   uint64

	FileList []EvmIndexerBackupFile
}

type EvmIndexerBackupFile struct {
	Content    string
	Identifier string
	FromBlock  uint64
	ToBlock    uint64
}

type EvmIndexerBackupStorage interface {
	Init() error
	LoadFile(path string) ([]byte, bool, error)
	DownloadFile(remotePath string, localPath string, overwrite bool) error
	UploadFile(localPath string, remotePath string, overwrite bool) error
}
