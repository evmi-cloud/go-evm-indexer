package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

func TestLag(t *testing.T) {
	cases := []struct {
		head, synced, want uint64
	}{
		{100, 90, 10},
		{100, 100, 0},
		{100, 120, 0}, // head momentarily behind synced → clamp at 0
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := lag(c.head, c.synced); got != c.want {
			t.Errorf("lag(%d,%d) = %d, want %d", c.head, c.synced, got, c.want)
		}
	}
}

// A disabled service must be a no-op and never touch the metrics or panic.
func TestDisabledServiceIsNoOp(t *testing.T) {
	m := NewMetricService(false, "/metrics", 0, zerolog.Nop())
	sl := SourceLabels{ChainID: 1, Pipeline: "p", Store: "s", SourceID: 7, SourceType: "CONTRACT"}
	el := ExporterLabels{ChainID: 1, Pipeline: "p", Exporter: "e"}

	// None of these should panic or record anything.
	m.SetChainHead(1, 10)
	m.SetSourceProgress(sl, 10, 5)
	m.SetSourceUp(sl, true)
	m.AddLogsIndexed(sl, 3)
	m.AddTransactionsIndexed(sl, 2)
	m.ObserveBatchDuration(sl, time.Second)
	m.ObserveStoreWrite("s", "logs", time.Second, nil)
	m.RecordRPC(1, "eth_getLogs", time.Second, nil)
	m.SetExporterProgress(el, 10, 5)
	m.AddExporterEvents(el, 4)
	m.IncExporterErrors(el)

	if v := testutil.ToFloat64(logsIndexedMetrics.WithLabelValues(sl.values()...)); v != 0 {
		t.Errorf("disabled service recorded logs: %v", v)
	}
}

func TestEnabledServiceRecords(t *testing.T) {
	m := NewMetricService(true, "/metrics", 0, zerolog.Nop())
	sl := SourceLabels{ChainID: 42, Pipeline: "pipe", Store: "store", SourceID: 1, SourceType: "TOPIC"}

	m.SetSourceProgress(sl, 100, 90)
	if v := testutil.ToFloat64(sourceLagBlocksMetrics.WithLabelValues(sl.values()...)); v != 10 {
		t.Errorf("source_lag_blocks = %v, want 10", v)
	}
	if v := testutil.ToFloat64(sourceSyncedBlockMetrics.WithLabelValues(sl.values()...)); v != 90 {
		t.Errorf("source_synced_block = %v, want 90", v)
	}

	m.AddLogsIndexed(sl, 5)
	m.AddLogsIndexed(sl, 3)
	if v := testutil.ToFloat64(logsIndexedMetrics.WithLabelValues(sl.values()...)); v != 8 {
		t.Errorf("logs_indexed_total = %v, want 8", v)
	}
	// AddLogsIndexed also feeds the per-store gauge.
	if v := testutil.ToFloat64(logsStoredMetrics.WithLabelValues("store")); v != 8 {
		t.Errorf("logs_stored = %v, want 8", v)
	}

	// A failed write bumps the error counter; a successful one does not.
	m.ObserveStoreWrite("store", "logs", time.Millisecond, errors.New("boom"))
	m.ObserveStoreWrite("store", "logs", time.Millisecond, nil)
	if v := testutil.ToFloat64(storeWriteErrorsMetrics.WithLabelValues("store", "logs")); v != 1 {
		t.Errorf("store_write_errors_total = %v, want 1", v)
	}

	// RPC status label is derived from the error.
	m.RecordRPC(42, "eth_getLogs", time.Millisecond, nil)
	m.RecordRPC(42, "eth_getLogs", time.Millisecond, errors.New("x"))
	if v := testutil.ToFloat64(rpcRequestsMetrics.WithLabelValues("42", "eth_getLogs", "ok")); v != 1 {
		t.Errorf("rpc ok = %v, want 1", v)
	}
	if v := testutil.ToFloat64(rpcRequestsMetrics.WithLabelValues("42", "eth_getLogs", "error")); v != 1 {
		t.Errorf("rpc error = %v, want 1", v)
	}
}

// Guard against label/value drift: the declared label count must match values().
func TestLabelArity(t *testing.T) {
	if len(sourceLabelNames) != len(SourceLabels{}.values()) {
		t.Fatalf("source labels arity mismatch: %d names vs %d values", len(sourceLabelNames), len(SourceLabels{}.values()))
	}
	if len(exporterLabelNames) != len(ExporterLabels{}.values()) {
		t.Fatalf("exporter labels arity mismatch: %d names vs %d values", len(exporterLabelNames), len(ExporterLabels{}.values()))
	}
}
