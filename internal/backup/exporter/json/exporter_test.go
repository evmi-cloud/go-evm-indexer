package jsonexporter_test

import (
	"errors"
	"os"
	"testing"

	"github.com/evmi-cloud/go-evm-indexer/internal/backup"
	jsonexporter "github.com/evmi-cloud/go-evm-indexer/internal/backup/exporter/json"
	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
)

var testLogs []models.EvmLog = []models.EvmLog{
	{
		Id:               "id",
		StoreId:          "storeId",
		SourceId:         "sourceId",
		Address:          "address",
		Topics:           []string{"topics"},
		Data:             "data",
		BlockNumber:      5,
		TransactionHash:  "transactionHash",
		TransactionIndex: 0,
		BlockHash:        "blockHash",
		LogIndex:         0,
		Removed:          false,
		MintedAt:         5,

		Metadata: models.EvmMetadata{
			ContractName: "TestContract",
			EventName:    "TestEvent",
			FunctionName: "Test Function",
			Data:         map[string]string{},
		},
	},
}

var testTransaction []models.EvmTransaction = []models.EvmTransaction{
	{
		Id:          "id",
		StoreId:     "storeId",
		SourceId:    "sourceId",
		BlockNumber: 5,
		ChainId:     137,
		From:        "from",
		Data:        "data",
		Value:       "value",
		Nonce:       1,
		To:          "to",
		Hash:        "hash",
		MintedAt:    5,

		Metadata: models.EvmMetadata{
			ContractName: "TestContract",
			EventName:    "TestEvent",
			FunctionName: "Test Function",
			Data:         map[string]string{},
		},
	},
}

var testState backup.EvmIndexerBackupState = backup.EvmIndexerBackupState{
	FromBlock: 1,
	ToBlock:   10,
	FileList: []backup.EvmIndexerBackupFile{
		{FromBlock: 1, ToBlock: 10, Identifier: "identifier"},
	},
}

const fileFolder string = "/tmp"

func TestExportAndImportLogs(t *testing.T) {
	exporter := jsonexporter.NewEvmJSONBackupExporter()
	err := exporter.ExportLogsToFile(fileFolder+"/logs.json", testLogs)
	if err != nil {
		t.Error(err)
	}

	if stat, err := os.Stat(fileFolder + "/logs.json"); err == nil {
		if stat.Size() == 0 {
			t.Error("file empty")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		t.Error("file not created")
	}

	logs, err := exporter.ImportLogsFromFile(fileFolder + "/logs.json")
	if err != nil {
		t.Error(err)
	}

	if len(logs) != len(testLogs) {
		t.Error("not same length")
	}

	if logs[0].Metadata.ContractName != testLogs[0].Metadata.ContractName {
		t.Error("error in data copy")
	}

	err = os.Remove(fileFolder + "/logs.json")
	if err != nil {
		t.Error(err)
	}
}

func TestExportAndImportTransaction(t *testing.T) {
	exporter := jsonexporter.NewEvmJSONBackupExporter()
	err := exporter.ExportTransactionsToFile(fileFolder+"/transactions.json", testTransaction)
	if err != nil {
		t.Error(err)
	}

	if stat, err := os.Stat(fileFolder + "/transactions.json"); err == nil {
		if stat.Size() == 0 {
			t.Error("file empty")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		t.Error("file not created")
	}

	txs, err := exporter.ImportTransactionsFromFile(fileFolder + "/transactions.json")
	if err != nil {
		t.Error(err)
	}

	if len(txs) != len(testTransaction) {
		t.Error("not same length")
	}

	if txs[0].Metadata.ContractName != testTransaction[0].Metadata.ContractName {
		t.Error("error in data copy")
	}

	err = os.Remove(fileFolder + "/transactions.json")
	if err != nil {
		t.Error(err)
	}
}

func TestExportAndImportState(t *testing.T) {
	exporter := jsonexporter.NewEvmJSONBackupExporter()
	err := exporter.ExportStateToFile(fileFolder+"/state.json", testState)
	if err != nil {
		t.Error(err)
	}

	if stat, err := os.Stat(fileFolder + "/state.json"); err == nil {
		if stat.Size() == 0 {
			t.Error("file empty")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		t.Error("file not created")
	}

	state, err := exporter.ImportStateFromFile(fileFolder + "/state.json")
	if err != nil {
		t.Error(err)
	}

	if state.FromBlock != testState.FromBlock {
		t.Error("not same data")
	}

	if state.ToBlock != testState.ToBlock {
		t.Error("not same data")
	}

	if len(state.FileList) != len(testState.FileList) {
		t.Error("not same data")
	}

	err = os.Remove(fileFolder + "/state.json")
	if err != nil {
		t.Error(err)
	}
}
