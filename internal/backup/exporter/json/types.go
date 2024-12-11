package jsonexporter

import (
	"github.com/evmi-cloud/go-evm-indexer/internal/backup"
	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
)

type EvmIndexerBackupStateJSON struct {
	FromBlock uint64 `json:"fromBlock"`
	ToBlock   uint64 `json:"toBlock"`

	FileList []EvmIndexerBackupFileJSON `json:"fileList"`
}

type EvmIndexerBackupFileJSON struct {
	Identifier string `json:"identifier"`
	FromBlock  uint64 `json:"fromBlock"`
	ToBlock    uint64 `json:"toBlock"`
}

type EvmMetadataJSON struct {
	ContractName string            `json:"contractName"`
	EventName    string            `json:"eventName"`
	FunctionName string            `json:"functionName"`
	Data         map[string]string `json:"data"`
}

type EvmLogJSON struct {
	Id               string   `json:"id"`
	StoreId          string   `json:"storeId"`
	SourceId         string   `json:"sourceId"`
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      uint64   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex uint64   `json:"transactionIndex"`
	BlockHash        string   `json:"blockHash"`
	LogIndex         uint64   `json:"logIndex"`
	Removed          bool     `json:"removed"`
	MintedAt         uint64   `json:"mintedAt"`

	Metadata EvmMetadataJSON `json:"metadata"`
}

type EvmTransactionJSON struct {
	Id          string `json:"id"`
	StoreId     string `json:"storeId"`
	SourceId    string `json:"sourceId"`
	BlockNumber uint64 `json:"blockNumber"`
	ChainId     uint64 `json:"chainId"`
	From        string `json:"from"`
	Data        string `json:"data"`
	Value       string `json:"value"`
	Nonce       uint64 `json:"nonce"`
	To          string `json:"to"`
	Hash        string `json:"hash"`
	MintedAt    uint64 `json:"mintedAt"`

	Metadata EvmMetadataJSON `json:"metadata"`
}

func fromLogsModels(data []models.EvmLog) []EvmLogJSON {
	var result []EvmLogJSON
	for _, v := range data {
		result = append(result, EvmLogJSON{
			Id:               v.Id,
			StoreId:          v.StoreId,
			SourceId:         v.SourceId,
			Address:          v.Address,
			Topics:           v.Topics,
			Data:             v.Data,
			BlockNumber:      v.BlockNumber,
			TransactionHash:  v.TransactionHash,
			TransactionIndex: v.TransactionIndex,
			BlockHash:        v.BlockHash,
			LogIndex:         v.LogIndex,
			Removed:          v.Removed,
			MintedAt:         v.MintedAt,

			Metadata: EvmMetadataJSON{
				ContractName: v.Metadata.ContractName,
				EventName:    v.Metadata.EventName,
				FunctionName: v.Metadata.FunctionName,
				Data:         v.Metadata.Data,
			},
		})
	}

	return result
}

func toLogsModels(data []EvmLogJSON) []models.EvmLog {
	var result []models.EvmLog
	for _, v := range data {
		result = append(result, models.EvmLog{
			Id:               v.Id,
			StoreId:          v.StoreId,
			SourceId:         v.SourceId,
			Address:          v.Address,
			Topics:           v.Topics,
			Data:             v.Data,
			BlockNumber:      v.BlockNumber,
			TransactionHash:  v.TransactionHash,
			TransactionIndex: v.TransactionIndex,
			BlockHash:        v.BlockHash,
			LogIndex:         v.LogIndex,
			Removed:          v.Removed,
			MintedAt:         v.MintedAt,

			Metadata: models.EvmMetadata{
				ContractName: v.Metadata.ContractName,
				EventName:    v.Metadata.EventName,
				FunctionName: v.Metadata.FunctionName,
				Data:         v.Metadata.Data,
			},
		})
	}

	return result
}

func fromTransactionsModels(data []models.EvmTransaction) []EvmTransactionJSON {
	var result []EvmTransactionJSON
	for _, v := range data {
		result = append(result, EvmTransactionJSON{
			Id:          v.Id,
			StoreId:     v.StoreId,
			SourceId:    v.SourceId,
			BlockNumber: v.BlockNumber,
			ChainId:     v.ChainId,
			From:        v.From,
			Data:        v.Data,
			Value:       v.Value,
			Nonce:       v.Nonce,
			To:          v.To,
			Hash:        v.Hash,
			MintedAt:    v.MintedAt,

			Metadata: EvmMetadataJSON{
				ContractName: v.Metadata.ContractName,
				EventName:    v.Metadata.EventName,
				FunctionName: v.Metadata.FunctionName,
				Data:         v.Metadata.Data,
			},
		})
	}

	return result
}

func toTransactionsModels(data []EvmTransactionJSON) []models.EvmTransaction {
	var result []models.EvmTransaction
	for _, v := range data {
		result = append(result, models.EvmTransaction{
			Id:          v.Id,
			StoreId:     v.StoreId,
			SourceId:    v.SourceId,
			BlockNumber: v.BlockNumber,
			ChainId:     v.ChainId,
			From:        v.From,
			Data:        v.Data,
			Value:       v.Value,
			Nonce:       v.Nonce,
			To:          v.To,
			Hash:        v.Hash,
			MintedAt:    v.MintedAt,

			Metadata: models.EvmMetadata{
				ContractName: v.Metadata.ContractName,
				EventName:    v.Metadata.EventName,
				FunctionName: v.Metadata.FunctionName,
				Data:         v.Metadata.Data,
			},
		})
	}

	return result
}

func fromBackupStateModels(data backup.EvmIndexerBackupState) EvmIndexerBackupStateJSON {
	var files []EvmIndexerBackupFileJSON
	for _, f := range data.FileList {
		files = append(files, EvmIndexerBackupFileJSON{
			Identifier: f.Identifier,
			FromBlock:  f.FromBlock,
			ToBlock:    f.ToBlock,
		})
	}

	result := EvmIndexerBackupStateJSON{
		FileList:  files,
		FromBlock: data.FromBlock,
		ToBlock:   data.ToBlock,
	}

	return result
}

func toBackupStateModels(data EvmIndexerBackupStateJSON) backup.EvmIndexerBackupState {
	var files []backup.EvmIndexerBackupFile
	for _, f := range data.FileList {
		files = append(files, backup.EvmIndexerBackupFile{
			Identifier: f.Identifier,
			FromBlock:  f.FromBlock,
			ToBlock:    f.ToBlock,
		})
	}

	result := backup.EvmIndexerBackupState{
		FileList:  files,
		FromBlock: data.FromBlock,
		ToBlock:   data.ToBlock,
	}

	return result
}
