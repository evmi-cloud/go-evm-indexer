package clickhouse_store

import (
	"math/big"
)

const (
	LogCollectionName         string = "logs"
	TransactionCollectionName string = "transactions"
)

type ClickHouseEvmMetadata struct {
	ContractName string            `ch:"contract_name"`
	EventName    string            `ch:"event_name"`
	FunctionName string            `ch:"function_name"`
	Data         map[string]string `ch:"data"`
}

// Field widths must match the DDL in templates.go exactly: the driver refuses
// to scan a UInt32 column into a wider Go integer.
type ClickHouseEvmLog struct {
	Id               string   `ch:"id"`
	ChainID          uint32   `ch:"chain_id"`
	SourceId         uint32   `ch:"source_id"`
	Address          string   `ch:"address"`
	Topics           []string `ch:"topics"`
	Data             string   `ch:"data"`
	BlockNumber      uint64   `ch:"block_number"`
	TransactionFrom  string   `ch:"transaction_from"`
	TransactionHash  string   `ch:"transaction_hash"`
	TransactionIndex uint32   `ch:"transaction_index"`
	BlockHash        string   `ch:"block_hash"`
	LogIndex         uint32   `ch:"log_index"`
	Removed          bool     `ch:"removed"`

	Metadata ClickHouseEvmMetadata `ch:"metadata"`
}

type ClickHouseEvmTransaction struct {
	Id               string   `ch:"id"`
	SourceId         uint32   `ch:"source_id"`
	BlockNumber      uint64   `ch:"block_number"`
	TransactionIndex uint64   `ch:"transaction_index"`
	ChainId          uint32   `ch:"chain_id"`
	From             string   `ch:"from"`
	Data             string   `ch:"data"`
	Value            *big.Int `ch:"value"`
	Nonce            uint64   `ch:"nonce"`
	To               string   `ch:"to"`
	Hash             string   `ch:"hash"`

	Metadata ClickHouseEvmMetadata `ch:"metadata"`
}
