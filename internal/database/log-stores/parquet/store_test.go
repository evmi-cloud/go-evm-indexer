package parquet_store

import (
	"fmt"
	"testing"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

func mkLog(sourceId uint, block, idx uint64) types.EvmLog {
	return types.EvmLog{
		Id:               fmt.Sprintf("1:%d:%d", block, idx),
		SourceId:         sourceId,
		ChainId:          1,
		Address:          "0xabc",
		Topics:           []string{"0xt0", "0xt1"},
		Data:             "deadbeef",
		BlockNumber:      block,
		LogIndex:         idx,
		TransactionHash:  "0xhash",
		TransactionFrom:  "0xfrom",
		BlockHash:        "0xbh",
		Metadata:         types.EvmMetadata{ContractName: "C", EventName: "E", Data: map[string]string{"k": "v"}},
	}
}

func newStore(t *testing.T) *ParquetStore {
	t.Helper()
	s, _ := NewParquetStore(zerolog.Nop())
	if err := s.Init(map[string]string{"path": t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return s
}

func ids(logs []types.EvmLog) []string {
	out := make([]string, len(logs))
	for i, l := range logs {
		out[i] = l.Id
	}
	return out
}

func TestParquetLogsRoundTrip(t *testing.T) {
	s := newStore(t)

	if err := s.InsertLogs([]types.EvmLog{
		mkLog(1, 10, 0), mkLog(1, 10, 1), mkLog(1, 12, 0), mkLog(2, 11, 0),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Count across sources.
	if c, err := s.GetLogsCount(); err != nil || c != 4 {
		t.Fatalf("count = %d, err %v (want 4)", c, err)
	}

	// GetLogs filters by source + block range, ascending.
	got, err := s.GetLogs(1, 10, 11)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(got)) != fmt.Sprint([]string{"1:10:0", "1:10:1"}) {
		t.Errorf("GetLogs = %v", ids(got))
	}

	// Complex fields survive the round trip.
	if len(got) > 0 {
		l := got[0]
		if len(l.Topics) != 2 || l.Topics[0] != "0xt0" || l.Metadata.Data["k"] != "v" || l.Metadata.ContractName != "C" {
			t.Errorf("complex fields not preserved: %+v", l)
		}
	}

	// GetLogsAfter: strictly after (10,0), up to block 12, across both sources.
	after, err := s.GetLogsAfter([]uint64{1, 2}, 10, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(after)) != fmt.Sprint([]string{"1:10:1", "1:11:0", "1:12:0"}) {
		t.Errorf("GetLogsAfter = %v (want 1:10:1,1:11:0,1:12:0)", ids(after))
	}

	// GetLatestLogs: descending, limited.
	latest, err := s.GetLatestLogs(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(latest)) != fmt.Sprint([]string{"1:12:0", "1:10:1"}) {
		t.Errorf("GetLatestLogs = %v", ids(latest))
	}
}

func TestParquetTransactionsRoundTrip(t *testing.T) {
	s := newStore(t)
	if err := s.InsertTransactions([]types.EvmTransaction{
		{Id: "1:10:tx", SourceId: 1, BlockNumber: 10, ChainId: 1, From: "0xf", To: "0xt", Value: "5", Hash: "0xh"},
	}); err != nil {
		t.Fatalf("insert txs: %v", err)
	}
	txs, err := s.GetTransactions(1, 0, 100)
	if err != nil || len(txs) != 1 || txs[0].Value != "5" || txs[0].From != "0xf" {
		t.Fatalf("GetTransactions = %+v, err %v", txs, err)
	}
}

func TestParquetInsertReplayDoesNotDuplicate(t *testing.T) {
	s := newStore(t)
	batch := []types.EvmLog{mkLog(1, 10, 0), mkLog(1, 10, 1), mkLog(1, 12, 0)}

	// A crash between the insert and the cursor save replays the same range.
	if err := s.InsertLogs(batch); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.InsertLogs(batch); err != nil {
		t.Fatalf("replay insert: %v", err)
	}

	if c, err := s.GetLogsCount(); err != nil || c != 3 {
		t.Fatalf("count = %d, err %v (want 3 after replay)", c, err)
	}
	got, err := s.GetLogs(1, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(got)) != fmt.Sprint([]string{"1:10:0", "1:10:1", "1:12:0"}) {
		t.Errorf("GetLogs after replay = %v", ids(got))
	}

	// Overlapping batches (distinct files sharing a log) dedupe on read too.
	if err := s.InsertLogs([]types.EvmLog{mkLog(1, 12, 0), mkLog(1, 14, 0)}); err != nil {
		t.Fatalf("overlapping insert: %v", err)
	}
	if c, err := s.GetLogsCount(); err != nil || c != 4 {
		t.Fatalf("count = %d, err %v (want 4 with overlap)", c, err)
	}

	tx := types.EvmTransaction{Id: "1:10:tx", SourceId: 1, BlockNumber: 10, ChainId: 1, From: "0xf", To: "0xt", Value: "5", Hash: "0xh"}
	if err := s.InsertTransactions([]types.EvmTransaction{tx}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTransactions([]types.EvmTransaction{tx}); err != nil {
		t.Fatal(err)
	}
	txs, err := s.GetTransactions(1, 0, 100)
	if err != nil || len(txs) != 1 {
		t.Fatalf("GetTransactions after replay = %+v, err %v (want 1)", txs, err)
	}
}
