package clickhouse_store

import (
	"math/big"
	"time"
)

const (
	StoreCollectionName       string = "stores"
	SourceCollectionName      string = "sources"
	LogCollectionName         string = "logs"
	TransactionCollectionName string = "transactions"
)

type ClickHouseLogStore struct {
	Id          string `ch:"id"`
	Identifier  string `ch:"identifier"`
	Description string `ch:"description"`
	Status      string `ch:"status"`
	ChainId     uint64 `ch:"chain_id"`
	Rpc         string `ch:"rpc"`
}

type ClickHouseLogSource struct {
	Id            string                        `ch:"id"`
	LogStoreId    string                        `ch:"store_id"`
	Name          string                        `ch:"name"`
	Type          string                        `ch:"type"`
	Contracts     []ClickHouseLogSourceContract `ch:"contracts"`
	Topic         string                        `ch:"topic"`
	IndexedTopics []string                      `ch:"indexed_topics"`
	StartBlock    uint64                        `ch:"start_block"`
	BlockRange    uint64                        `ch:"block_range"`

	LatestBlockIndexed uint64 `ch:"latest_block_indexed"`
}

type ClickHouseLogSourceContract struct {
	ContractName string `ch:"name"`
	Address      string `ch:"address"`
}

type ClickHouseEvmMetadata struct {
	ContractName string            `ch:"contract_name"`
	EventName    string            `ch:"event_name"`
	FunctionName string            `ch:"function_name"`
	Data         map[string]string `ch:"data"`
}

type ClickHouseEvmLog struct {
	Id               string    `ch:"id"`
	StoreId          uint      `ch:"store_id"`
	SourceId         uint      `ch:"source_id"`
	Address          string    `ch:"address"`
	Topics           []string  `ch:"topics"`
	Data             string    `ch:"data"`
	BlockNumber      uint64    `ch:"block_number"`
	TransactionFrom  string    `ch:"transaction_from"`
	TransactionHash  string    `ch:"transaction_hash"`
	TransactionIndex uint64    `ch:"transaction_index"`
	BlockHash        string    `ch:"block_hash"`
	LogIndex         uint64    `ch:"log_index"`
	Removed          bool      `ch:"removed"`
	MintedAt         time.Time `ch:"minted_at"`

	Metadata ClickHouseEvmMetadata `ch:"metadata"`
}

type ClickHouseEvmTransaction struct {
	Id               string    `ch:"id"`
	StoreId          uint      `ch:"store_id"`
	SourceId         uint      `ch:"source_id"`
	BlockNumber      uint64    `ch:"block_number"`
	TransactionIndex uint64    `ch:"transaction_index"`
	ChainId          uint64    `ch:"chain_id"`
	From             string    `ch:"from"`
	Data             string    `ch:"data"`
	Value            *big.Int  `ch:"value"`
	Nonce            uint64    `ch:"nonce"`
	To               string    `ch:"to"`
	Hash             string    `ch:"hash"`
	MintedAt         time.Time `ch:"minted_at"`

	Metadata ClickHouseEvmMetadata `ch:"metadata"`
}
