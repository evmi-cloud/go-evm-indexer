package elasticsearch_store

import (
	"fmt"
	"os"
	"testing"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

// TestElasticsearchStore runs against a real Elasticsearch. Set ELASTICSEARCH_URL
// (e.g. http://localhost:9200) to enable it; it is skipped otherwise.
func TestElasticsearchStore(t *testing.T) {
	url := os.Getenv("ELASTICSEARCH_URL")
	if url == "" {
		t.Skip("set ELASTICSEARCH_URL to run the Elasticsearch integration test")
	}

	cfg := map[string]string{
		"addresses":         url,
		"logsIndex":         "evmi_test_logs",
		"transactionsIndex": "evmi_test_txs",
	}

	s, _ := NewElasticsearchStore(zerolog.Nop())
	if err := s.Init(cfg); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Clean slate, then re-init to recreate the indices with mappings.
	s.client.Indices.Delete([]string{"evmi_test_logs", "evmi_test_txs"})
	if err := s.Init(cfg); err != nil {
		t.Fatalf("re-init: %v", err)
	}

	mk := func(sourceId uint, block, idx uint64) types.EvmLog {
		return types.EvmLog{
			Id: fmt.Sprintf("1:%d:%d", block, idx), SourceId: sourceId, ChainId: 1, Address: "0xabc",
			Topics: []string{"0xt0"}, Data: "beef", BlockNumber: block, LogIndex: idx,
			Metadata: types.EvmMetadata{ContractName: "C", Data: map[string]string{"k": "v"}},
		}
	}
	if err := s.InsertLogs([]types.EvmLog{mk(1, 10, 0), mk(1, 10, 1), mk(1, 12, 0), mk(2, 11, 0)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if c, err := s.GetLogsCount(); err != nil || c != 4 {
		t.Fatalf("count = %d, err %v (want 4)", c, err)
	}

	assertIds := func(name string, got []types.EvmLog, want ...string) {
		ids := make([]string, len(got))
		for i, l := range got {
			ids[i] = l.Id
		}
		if fmt.Sprint(ids) != fmt.Sprint(want) {
			t.Errorf("%s = %v, want %v", name, ids, want)
		}
	}

	logs, err := s.GetLogs(1, 10, 11)
	if err != nil {
		t.Fatal(err)
	}
	assertIds("GetLogs", logs, "1:10:0", "1:10:1")
	if len(logs) > 0 && (logs[0].Metadata.Data["k"] != "v" || len(logs[0].Topics) != 1) {
		t.Errorf("complex fields not preserved: %+v", logs[0])
	}

	after, err := s.GetLogsAfter([]uint64{1, 2}, 10, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	assertIds("GetLogsAfter", after, "1:10:1", "1:11:0", "1:12:0")

	latest, err := s.GetLatestLogs(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertIds("GetLatestLogs", latest, "1:12:0", "1:10:1")

	if err := s.InsertTransactions([]types.EvmTransaction{
		{Id: "1:10:tx", SourceId: 1, BlockNumber: 10, ChainId: 1, From: "0xf", To: "0xt", Value: "5", Hash: "0xh"},
	}); err != nil {
		t.Fatalf("insert txs: %v", err)
	}
	txs, err := s.GetTransactions(1, 0, 100)
	if err != nil || len(txs) != 1 || txs[0].Value != "5" {
		t.Fatalf("GetTransactions = %+v, err %v", txs, err)
	}

	s.client.Indices.Delete([]string{"evmi_test_logs", "evmi_test_txs"})
}
