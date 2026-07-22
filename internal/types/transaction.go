package types

type EvmTransaction struct {
	Id               string
	SourceId         uint
	BlockNumber      uint64
	BlockTimestamp   uint64 // unix seconds, from the block header
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
