package jsonexporter

import (
	"encoding/json"
	"os"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
)

type EvmJSONBackupExporter struct{}

func (e EvmJSONBackupExporter) ExportLogsToFile(localPath string, data []types.EvmLog) error {

	file, err := os.OpenFile(localPath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(fromLogsTypes(data))
	if err != nil {
		return err
	}

	return nil
}

func (e EvmJSONBackupExporter) ImportLogsFromFile(localPath string) ([]types.EvmLog, error) {

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

	return toLogsTypes(result), nil
}

func (e EvmJSONBackupExporter) ExportTransactionsToFile(localPath string, data []types.EvmTransaction) error {

	file, err := os.OpenFile(localPath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(fromTransactionsTypes(data))
	if err != nil {
		return err
	}

	return nil
}
func (e EvmJSONBackupExporter) ImportTransactionsFromFile(localPath string) ([]types.EvmTransaction, error) {

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

	return toTransactionsTypes(result), nil
}

func (e EvmJSONBackupExporter) ExportStateToFile(localPath string, data types.EvmIndexerBackupState) error {

	jsonString, err := json.Marshal(fromBackupStateTypes(data))
	if err != nil {
		return err
	}

	err = os.WriteFile(localPath, jsonString, os.ModePerm)
	if err != nil {
		return err
	}

	return nil
}

func (e EvmJSONBackupExporter) ImportStateFromFile(localPath string) (types.EvmIndexerBackupState, error) {

	data := EvmIndexerBackupStateJSON{}

	file, err := os.ReadFile(localPath)
	if err != nil {
		return types.EvmIndexerBackupState{}, err
	}

	err = json.Unmarshal(file, &data)
	if err != nil {
		return types.EvmIndexerBackupState{}, err
	}

	return toBackupStateTypes(data), nil
}

func (e EvmJSONBackupExporter) ExportStateToBytes(data types.EvmIndexerBackupState) ([]byte, error) {
	return json.Marshal(data)
}

func (e EvmJSONBackupExporter) ImportStateFromBytes(content []byte) (types.EvmIndexerBackupState, error) {
	var state types.EvmIndexerBackupState
	err := json.Unmarshal(content, &state)
	if err != nil {
		return types.EvmIndexerBackupState{}, err
	}

	return state, nil
}

func NewEvmJSONBackupExporter() EvmJSONBackupExporter {
	return EvmJSONBackupExporter{}
}
