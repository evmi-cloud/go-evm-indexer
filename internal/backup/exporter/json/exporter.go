package jsonexporter

import (
	"encoding/json"
	"os"

	"github.com/evmi-cloud/go-evm-indexer/internal/backup"
	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
)

type EvmJSONBackupExporter struct{}

func (e EvmJSONBackupExporter) ExportLogsToFile(localPath string, data []models.EvmLog) error {

	file, err := os.OpenFile(localPath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(fromLogsModels(data))
	if err != nil {
		return err
	}

	return nil
}

func (e EvmJSONBackupExporter) ImportLogsFromFile(localPath string) ([]models.EvmLog, error) {

	file, err := os.OpenFile(localPath, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	decoder := json.NewDecoder(file)

	// Read the array open bracket
	_, err = decoder.Token()
	if err != nil {
		return nil, err
	}

	var result []EvmLogJSON

	tmpData := EvmLogJSON{}
	for decoder.More() {
		decoder.Decode(&tmpData)
		result = append(result, tmpData)
	}

	return toLogsModels(result), nil
}

func (e EvmJSONBackupExporter) ExportTransactionsToFile(localPath string, data []models.EvmTransaction) error {

	file, err := os.OpenFile(localPath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(fromTransactionsModels(data))
	if err != nil {
		return err
	}

	return nil
}
func (e EvmJSONBackupExporter) ImportTransactionsFromFile(localPath string) ([]models.EvmTransaction, error) {

	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	decoder := json.NewDecoder(file)

	// Read the array open bracket
	_, err = decoder.Token()
	if err != nil {
		return nil, err
	}

	var result []EvmTransactionJSON

	tmpData := EvmTransactionJSON{}
	for decoder.More() {
		decoder.Decode(&tmpData)
		result = append(result, tmpData)
	}

	return toTransactionsModels(result), nil
}

func (e EvmJSONBackupExporter) ExportStateToFile(localPath string, data backup.EvmIndexerBackupState) error {

	jsonString, err := json.Marshal(fromBackupStateModels(data))
	if err != nil {
		return err
	}

	err = os.WriteFile(localPath, jsonString, os.ModePerm)
	if err != nil {
		return err
	}

	return nil
}

func (e EvmJSONBackupExporter) ImportStateFromFile(localPath string) (backup.EvmIndexerBackupState, error) {

	data := EvmIndexerBackupStateJSON{}

	file, err := os.ReadFile(localPath)
	if err != nil {
		return backup.EvmIndexerBackupState{}, err
	}

	err = json.Unmarshal(file, &data)
	if err != nil {
		return backup.EvmIndexerBackupState{}, err
	}

	return toBackupStateModels(data), nil
}

func NewEvmJSONBackupExporter() EvmJSONBackupExporter {
	return EvmJSONBackupExporter{}
}
