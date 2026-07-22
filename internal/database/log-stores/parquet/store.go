// Package parquet_store implements the EvmIndexerStorage backend as Parquet
// files on disk. Logs and transactions are written as immutable Parquet files
// partitioned per source; queries read the relevant source's files back and
// filter/sort in memory. It is an analytics-friendly archival sink — writes are
// cheap and columnar, reads scan files.
package parquet_store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/parquet-go/parquet-go"
	"github.com/rs/zerolog"
)

type ParquetStore struct {
	logger  zerolog.Logger
	logsDir string
	txDir   string
}

func NewParquetStore(logger zerolog.Logger) (*ParquetStore, error) {
	return &ParquetStore{logger: logger}, nil
}

func (s *ParquetStore) Init(config map[string]string) error {
	base := config["path"]
	if base == "" {
		return fmt.Errorf("parquet store: config \"path\" is required")
	}
	s.logsDir = filepath.Join(base, "logs")
	s.txDir = filepath.Join(base, "transactions")
	if err := os.MkdirAll(s.logsDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(s.txDir, 0o755)
}

// --- parquet row models (complex fields JSON-encoded to keep a flat schema) ---

type parquetLog struct {
	Id               string `parquet:"id"`
	SourceId         uint64 `parquet:"source_id"`
	ChainId          uint64 `parquet:"chain_id"`
	Address          string `parquet:"address"`
	Topics           string `parquet:"topics"`
	Data             string `parquet:"data"`
	BlockNumber      uint64 `parquet:"block_number"`
	TransactionFrom  string `parquet:"transaction_from"`
	TransactionHash  string `parquet:"transaction_hash"`
	TransactionIndex uint64 `parquet:"transaction_index"`
	BlockHash        string `parquet:"block_hash"`
	LogIndex         uint64 `parquet:"log_index"`
	Removed          bool   `parquet:"removed"`
	ContractName     string `parquet:"metadata_contract_name"`
	EventName        string `parquet:"metadata_event_name"`
	FunctionName     string `parquet:"metadata_function_name"`
	MetadataData     string `parquet:"metadata_data"`
}

type parquetTx struct {
	Id               string `parquet:"id"`
	SourceId         uint64 `parquet:"source_id"`
	BlockNumber      uint64 `parquet:"block_number"`
	TransactionIndex uint64 `parquet:"transaction_index"`
	ChainId          uint64 `parquet:"chain_id"`
	From             string `parquet:"from"`
	Data             string `parquet:"data"`
	Value            string `parquet:"value"`
	Nonce            uint64 `parquet:"nonce"`
	To               string `parquet:"to"`
	Hash             string `parquet:"hash"`
	ContractName     string `parquet:"metadata_contract_name"`
	EventName        string `parquet:"metadata_event_name"`
	FunctionName     string `parquet:"metadata_function_name"`
	MetadataData     string `parquet:"metadata_data"`
}

func toParquetLog(l types.EvmLog) parquetLog {
	topics, _ := json.Marshal(l.Topics)
	data, _ := json.Marshal(l.Metadata.Data)
	return parquetLog{
		Id: l.Id, SourceId: uint64(l.SourceId), ChainId: l.ChainId, Address: l.Address,
		Topics: string(topics), Data: l.Data, BlockNumber: l.BlockNumber,
		TransactionFrom: l.TransactionFrom, TransactionHash: l.TransactionHash,
		TransactionIndex: l.TransactionIndex, BlockHash: l.BlockHash, LogIndex: l.LogIndex,
		Removed: l.Removed, ContractName: l.Metadata.ContractName, EventName: l.Metadata.EventName,
		FunctionName: l.Metadata.FunctionName, MetadataData: string(data),
	}
}

func fromParquetLog(p parquetLog) types.EvmLog {
	var topics []string
	_ = json.Unmarshal([]byte(p.Topics), &topics)
	data := map[string]string{}
	_ = json.Unmarshal([]byte(p.MetadataData), &data)
	return types.EvmLog{
		Id: p.Id, SourceId: uint(p.SourceId), ChainId: p.ChainId, Address: p.Address,
		Topics: topics, Data: p.Data, BlockNumber: p.BlockNumber, TransactionFrom: p.TransactionFrom,
		TransactionHash: p.TransactionHash, TransactionIndex: p.TransactionIndex, BlockHash: p.BlockHash,
		LogIndex: p.LogIndex, Removed: p.Removed,
		Metadata: types.EvmMetadata{ContractName: p.ContractName, EventName: p.EventName, FunctionName: p.FunctionName, Data: data},
	}
}

func toParquetTx(t types.EvmTransaction) parquetTx {
	data, _ := json.Marshal(t.Metadata.Data)
	return parquetTx{
		Id: t.Id, SourceId: uint64(t.SourceId), BlockNumber: t.BlockNumber, TransactionIndex: t.TransactionIndex,
		ChainId: t.ChainId, From: t.From, Data: t.Data, Value: t.Value, Nonce: t.Nonce, To: t.To, Hash: t.Hash,
		ContractName: t.Metadata.ContractName, EventName: t.Metadata.EventName, FunctionName: t.Metadata.FunctionName,
		MetadataData: string(data),
	}
}

func fromParquetTx(p parquetTx) types.EvmTransaction {
	data := map[string]string{}
	_ = json.Unmarshal([]byte(p.MetadataData), &data)
	return types.EvmTransaction{
		Id: p.Id, SourceId: uint(p.SourceId), BlockNumber: p.BlockNumber, TransactionIndex: p.TransactionIndex,
		ChainId: p.ChainId, From: p.From, Data: p.Data, Value: p.Value, Nonce: p.Nonce, To: p.To, Hash: p.Hash,
		Metadata: types.EvmMetadata{ContractName: p.ContractName, EventName: p.EventName, FunctionName: p.FunctionName, Data: data},
	}
}

// --- writes ---------------------------------------------------------------

func (s *ParquetStore) InsertLogs(logs []types.EvmLog) error {
	bySource := map[uint]([]parquetLog){}
	for _, l := range logs {
		bySource[l.SourceId] = append(bySource[l.SourceId], toParquetLog(l))
	}
	for sourceId, rows := range bySource {
		var minBlock, maxBlock uint64
		for i, r := range rows {
			if i == 0 || r.BlockNumber < minBlock {
				minBlock = r.BlockNumber
			}
			if r.BlockNumber > maxBlock {
				maxBlock = r.BlockNumber
			}
		}
		if err := writeBatchFile(s.sourceDir(s.logsDir, uint64(sourceId)), minBlock, maxBlock, rows); err != nil {
			return err
		}
	}
	return nil
}

func (s *ParquetStore) InsertTransactions(txs []types.EvmTransaction) error {
	bySource := map[uint]([]parquetTx){}
	for _, t := range txs {
		bySource[t.SourceId] = append(bySource[t.SourceId], toParquetTx(t))
	}
	for sourceId, rows := range bySource {
		var minBlock, maxBlock uint64
		for i, r := range rows {
			if i == 0 || r.BlockNumber < minBlock {
				minBlock = r.BlockNumber
			}
			if r.BlockNumber > maxBlock {
				maxBlock = r.BlockNumber
			}
		}
		if err := writeBatchFile(s.sourceDir(s.txDir, uint64(sourceId)), minBlock, maxBlock, rows); err != nil {
			return err
		}
	}
	return nil
}

// writeBatchFile writes one batch as a parquet file named after its block
// range. Inserts are replayed after a crash (the sync cursor only advances
// once the write succeeded), so the name must be deterministic: replaying the
// same range overwrites the previous file instead of duplicating its rows.
// The write goes through a temp file + rename so readers never see a partial
// file.
func writeBatchFile[T any](dir string, minBlock, maxBlock uint64, rows []T) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	final := filepath.Join(dir, fmt.Sprintf("%020d-%020d.parquet", minBlock, maxBlock))
	tmp := final + ".tmp"
	if err := parquet.WriteFile(tmp, rows); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, final)
}

// --- reads ----------------------------------------------------------------

func (s *ParquetStore) GetLogsCount() (uint64, error) {
	entries, err := os.ReadDir(s.logsDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var count uint64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logs, err := s.readSourceLogs(filepath.Join(s.logsDir, e.Name()))
		if err != nil {
			return 0, err
		}
		count += uint64(len(logs))
	}
	return count, nil
}

func (s *ParquetStore) GetLogs(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmLog, error) {
	logs, err := s.readSourceLogs(s.sourceDir(s.logsDir, sourceId))
	if err != nil {
		return nil, err
	}
	out := []types.EvmLog{}
	for _, l := range logs {
		if l.BlockNumber >= fromBlock && l.BlockNumber <= toBlock {
			out = append(out, l)
		}
	}
	sortLogs(out)
	return out, nil
}

func (s *ParquetStore) GetLogsAfter(sourceIds []uint64, afterBlock uint64, afterLogIndex uint64, toBlock uint64) ([]types.EvmLog, error) {
	out := []types.EvmLog{}
	for _, id := range sourceIds {
		logs, err := s.readSourceLogs(s.sourceDir(s.logsDir, id))
		if err != nil {
			return nil, err
		}
		for _, l := range logs {
			if l.BlockNumber > toBlock {
				continue
			}
			after := l.BlockNumber > afterBlock || (l.BlockNumber == afterBlock && l.LogIndex > afterLogIndex)
			if after {
				out = append(out, l)
			}
		}
	}
	sortLogs(out)
	return out, nil
}

func (s *ParquetStore) GetLogStream(sourceId uint64, fromBlock uint64, toBlock uint64, stream chan types.EvmLog) error {
	logs, err := s.GetLogs(sourceId, fromBlock, toBlock)
	if err != nil {
		close(stream)
		return err
	}
	for _, l := range logs {
		stream <- l
	}
	close(stream)
	return nil
}

func (s *ParquetStore) GetLatestLogs(sourceId uint64, limit uint64) ([]types.EvmLog, error) {
	logs, err := s.readSourceLogs(s.sourceDir(s.logsDir, sourceId))
	if err != nil {
		return nil, err
	}
	// descending by (block, logIndex)
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber > logs[j].BlockNumber
		}
		return logs[i].LogIndex > logs[j].LogIndex
	})
	if uint64(len(logs)) > limit {
		logs = logs[:limit]
	}
	return logs, nil
}

func (s *ParquetStore) GetTransactions(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error) {
	txs, err := s.readSourceTxs(s.sourceDir(s.txDir, sourceId))
	if err != nil {
		return nil, err
	}
	out := []types.EvmTransaction{}
	for _, t := range txs {
		if t.BlockNumber >= fromBlock && t.BlockNumber <= toBlock {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BlockNumber > out[j].BlockNumber })
	return out, nil
}

// --- helpers --------------------------------------------------------------

func (s *ParquetStore) sourceDir(base string, sourceId uint64) string {
	return filepath.Join(base, fmt.Sprintf("source-%d", sourceId))
}

// readSourceLogs reads every file of a source, deduplicating rows by id:
// overlapping batch files (e.g. left behind by older versions or a re-sliced
// range) must not surface a log twice.
func (s *ParquetStore) readSourceLogs(dir string) ([]types.EvmLog, error) {
	files, err := parquetFiles(dir)
	if err != nil {
		return nil, err
	}
	out := []types.EvmLog{}
	seen := map[string]struct{}{}
	for _, f := range files {
		rows, err := parquet.ReadFile[parquetLog](f)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if _, dup := seen[r.Id]; dup {
				continue
			}
			seen[r.Id] = struct{}{}
			out = append(out, fromParquetLog(r))
		}
	}
	return out, nil
}

func (s *ParquetStore) readSourceTxs(dir string) ([]types.EvmTransaction, error) {
	files, err := parquetFiles(dir)
	if err != nil {
		return nil, err
	}
	out := []types.EvmTransaction{}
	seen := map[string]struct{}{}
	for _, f := range files {
		rows, err := parquet.ReadFile[parquetTx](f)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if _, dup := seen[r.Id]; dup {
				continue
			}
			seen[r.Id] = struct{}{}
			out = append(out, fromParquetTx(r))
		}
	}
	return out, nil
}

func parquetFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".parquet" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

func sortLogs(logs []types.EvmLog) {
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		return logs[i].LogIndex < logs[j].LogIndex
	})
}
