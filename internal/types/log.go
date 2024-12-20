package types

type EvmMetadata struct {
	ContractName string
	EventName    string
	FunctionName string
	Data         map[string]string
}

type EvmLog struct {
	Id               string
	StoreId          string
	SourceId         string
	Address          string
	Topics           []string
	Data             string
	BlockNumber      uint64
	TransactionHash  string
	TransactionIndex uint64
	BlockHash        string
	LogIndex         uint64
	Removed          bool
	MintedAt         uint64

	Metadata EvmMetadata
}
