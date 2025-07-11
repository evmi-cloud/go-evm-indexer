package types

type EvmTransaction struct {
	Id               string
	StoreId          uint
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
	MintedAt         uint64

	Metadata EvmMetadata
}
