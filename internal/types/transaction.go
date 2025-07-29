package types

type EvmTransaction struct {
	Id               string
	SourceId         uint
	BlockNumber      uint64
	TransactionIndex uint64
	ChainId          uint64
	From             string
	Data             string
	Value            string
	Nonce            uint64
	To               string
	Hash             string

	Metadata EvmMetadata
}
