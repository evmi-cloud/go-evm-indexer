package types

type EvmTransaction struct {
	Id          string
	StoreId     string
	SourceId    string
	BlockNumber uint64
	ChainId     uint64
	From        string
	Data        string
	Value       string
	Nonce       uint64
	To          string
	Hash        string
	MintedAt    uint64

	Metadata EvmMetadata
}
