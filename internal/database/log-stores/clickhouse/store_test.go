package clickhouse_store

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

// TestClickHouseStore runs against a real ClickHouse. Set CLICKHOUSE_ADDR
// (e.g. localhost:9000, the compose stack works) to enable it; it is skipped
// otherwise.
func TestClickHouseStore(t *testing.T) {
	addr := os.Getenv("CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set CLICKHOUSE_ADDR to run the ClickHouse integration test")
	}
	ctx := context.Background()

	cfg := map[string]string{
		"addr":                  addr,
		"database":              orEnv("CLICKHOUSE_DATABASE", "default"),
		"username":              orEnv("CLICKHOUSE_USERNAME", "default"),
		"password":              os.Getenv("CLICKHOUSE_PASSWORD"),
		"logsTableName":         "evmi_test_logs",
		"transactionsTableName": "evmi_test_transactions",
	}
	s, _ := NewClickHouseStore(zerolog.Nop())
	if err := s.Init(cfg); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer func() {
		_ = s.store.Exec(ctx, "DROP TABLE IF EXISTS evmi_test_logs")
		_ = s.store.Exec(ctx, "DROP TABLE IF EXISTS evmi_test_transactions")
	}()

	mk := func(sourceId uint, block, idx uint64) types.EvmLog {
		return types.EvmLog{
			Id: fmt.Sprintf("1:%d:%d", block, idx), SourceId: sourceId, ChainId: 1, Address: "0xabc",
			Topics: []string{"0xt0", "0xt1"}, Data: "beef", BlockNumber: block, LogIndex: idx,
			TransactionFrom: "0xfrom", TransactionHash: "0xhash", BlockHash: "0xbh",
			Metadata: types.EvmMetadata{ContractName: "C", EventName: "E", Data: map[string]string{"k": "v"}},
		}
	}

	batch := []types.EvmLog{mk(1, 10, 0), mk(1, 10, 1), mk(1, 12, 0), mk(2, 11, 0)}
	if err := s.InsertLogs(batch); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Replayed range: identical rows must be collapsed, not duplicated.
	if err := s.InsertLogs(batch); err != nil {
		t.Fatalf("replay insert: %v", err)
	}

	if c, err := s.GetLogsCount(); err != nil || c != 4 {
		t.Fatalf("count = %d, err %v (want 4)", c, err)
	}

	got, err := s.GetLogs(1, 10, 11)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("GetLogs = %d logs, want 2", len(got))
	}
	l := got[0]
	if l.Id != "1:10:0" || l.ChainId != 1 || l.TransactionFrom != "0xfrom" ||
		len(l.Topics) != 2 || l.Metadata.Data["k"] != "v" {
		t.Errorf("fields not preserved: %+v", l)
	}

	after, err := s.GetLogsAfter([]uint64{1, 2}, 10, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 3 || after[0].Id != "1:10:1" {
		t.Errorf("GetLogsAfter = %+v (want 1:10:1 first, 3 logs)", after)
	}

	latest, err := s.GetLatestLogs(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 || latest[0].BlockNumber != 12 {
		t.Errorf("GetLatestLogs = %+v", latest)
	}

	stream := make(chan types.EvmLog, 16)
	if err := s.GetLogStream(1, 10, 12, stream); err != nil {
		t.Fatalf("stream: %v", err)
	}
	var streamed int
	for range stream {
		streamed++
	}
	if streamed != 3 {
		t.Errorf("streamed %d logs, want 3", streamed)
	}

	tx := types.EvmTransaction{
		Id: "1:0xh", SourceId: 1, BlockNumber: 10, ChainId: 1, From: "0xf", To: "0xt",
		Value: "5", Hash: "0xh", Metadata: types.EvmMetadata{ContractName: "C"},
	}
	if err := s.InsertTransactions([]types.EvmTransaction{tx}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTransactions([]types.EvmTransaction{tx}); err != nil {
		t.Fatal(err)
	}
	txs, err := s.GetTransactions(1, 0, 100)
	if err != nil || len(txs) != 1 || txs[0].Value != "5" {
		t.Fatalf("GetTransactions = %+v, err %v (want 1 tx)", txs, err)
	}
}

func orEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
