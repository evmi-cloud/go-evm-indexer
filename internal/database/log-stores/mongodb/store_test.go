package mongodb_store

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

// TestMongoStore runs against a real MongoDB. Set MONGODB_URI (e.g.
// mongodb://localhost:27017) to enable it; it is skipped otherwise.
func TestMongoStore(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("set MONGODB_URI to run the MongoDB integration test")
	}
	ctx := context.Background()

	cfg := map[string]string{"uri": uri, "database": "evmi_test", "logsCollection": "logs", "transactionsCollection": "transactions"}
	s, _ := NewMongoStore(zerolog.Nop())
	if err := s.Init(cfg); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Clean slate, then re-init to recreate indexes.
	_ = s.logs.Drop(ctx)
	_ = s.txs.Drop(ctx)
	if err := s.Init(cfg); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	defer s.client.Database("evmi_test").Drop(ctx)

	mk := func(sourceId uint, block, idx uint64) types.EvmLog {
		return types.EvmLog{
			Id: fmt.Sprintf("1:%d:%d", block, idx), SourceId: sourceId, ChainId: 1, Address: "0xabc",
			Topics: []string{"0xt0", "0xt1"}, Data: "beef", BlockNumber: block, LogIndex: idx,
			Metadata: types.EvmMetadata{ContractName: "C", Data: map[string]string{"k": "v"}},
		}
	}
	logs := []types.EvmLog{mk(1, 10, 0), mk(1, 10, 1), mk(1, 12, 0), mk(2, 11, 0)}
	if err := s.InsertLogs(logs); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Upsert dedup: re-insert must keep the count at 4.
	if err := s.InsertLogs(logs); err != nil {
		t.Fatalf("re-insert: %v", err)
	}

	if c, err := s.GetLogsCount(); err != nil || c != 4 {
		t.Fatalf("count = %d, err %v (want 4)", c, err)
	}

	ids := func(got []types.EvmLog) string {
		out := make([]string, len(got))
		for i, l := range got {
			out[i] = l.Id
		}
		return fmt.Sprint(out)
	}

	got, err := s.GetLogs(1, 10, 11)
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != fmt.Sprint([]string{"1:10:0", "1:10:1"}) {
		t.Errorf("GetLogs = %s", ids(got))
	}
	if len(got) > 0 && (len(got[0].Topics) != 2 || got[0].Metadata.Data["k"] != "v") {
		t.Errorf("complex fields not preserved: %+v", got[0])
	}

	after, err := s.GetLogsAfter([]uint64{1, 2}, 10, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	if ids(after) != fmt.Sprint([]string{"1:10:1", "1:11:0", "1:12:0"}) {
		t.Errorf("GetLogsAfter = %s", ids(after))
	}

	latest, err := s.GetLatestLogs(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ids(latest) != fmt.Sprint([]string{"1:12:0", "1:10:1"}) {
		t.Errorf("GetLatestLogs = %s", ids(latest))
	}

	if err := s.InsertTransactions([]types.EvmTransaction{
		{Id: "1:tx", SourceId: 1, BlockNumber: 10, ChainId: 1, From: "0xf", To: "0xt", Value: "5", Hash: "0xh"},
	}); err != nil {
		t.Fatalf("insert txs: %v", err)
	}
	txs, err := s.GetTransactions(1, 0, 100)
	if err != nil || len(txs) != 1 || txs[0].Value != "5" {
		t.Fatalf("GetTransactions = %+v, err %v", txs, err)
	}
}
