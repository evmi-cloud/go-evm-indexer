package exporter

import (
	"errors"
	"sync/atomic"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const maxUint64 = ^uint64(0)

// --- pure helpers ----------------------------------------------------------

func TestCursorBound(t *testing.T) {
	cases := []struct {
		completed uint64
		lastIdx   int64
		wantBlock uint64
		wantIdx   uint64
	}{
		{0, -1, 0, maxUint64},   // fresh: everything after block 0
		{10, -1, 10, maxUint64}, // block 10 done: everything after block 10
		{10, 3, 11, 3},          // mid-block 11 at idx 3: strictly after (11,3)
		{10, 0, 11, 0},          // mid-block 11 at idx 0: strictly after (11,0)
	}
	for _, c := range cases {
		gotBlock, gotIdx := cursorBound(c.completed, c.lastIdx)
		if gotBlock != c.wantBlock || gotIdx != c.wantIdx {
			t.Errorf("cursorBound(%d,%d) = (%d,%d), want (%d,%d)",
				c.completed, c.lastIdx, gotBlock, gotIdx, c.wantBlock, c.wantIdx)
		}
	}
}

func TestBlockBefore(t *testing.T) {
	for _, c := range []struct{ in, want uint64 }{{0, 0}, {1, 0}, {5, 4}} {
		if got := blockBefore(c.in); got != c.want {
			t.Errorf("blockBefore(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestToLogEvent(t *testing.T) {
	l := types.EvmLog{
		Id:          "1:10:2",
		SourceId:    7,
		ChainId:     1,
		Address:     "0xabc",
		Topics:      []string{"0xtopic"},
		Data:        "deadbeef",
		BlockNumber: 10,
		LogIndex:    2,
		Metadata: types.EvmMetadata{
			ContractName: "Token",
			EventName:    "Transfer",
			Data:         map[string]string{"from": "0x1", "to": "0x2"},
		},
	}
	e := toLogEvent(l)
	if e.Id != "1:10:2" || e.ChainId != 1 || e.BlockNumber != 10 || e.LogIndex != 2 {
		t.Fatalf("core fields not mapped: %+v", e)
	}
	if e.ContractName != "Token" || e.EventName != "Transfer" || e.Args["from"] != "0x1" {
		t.Fatalf("metadata not mapped: %+v", e)
	}
}

// --- test doubles ----------------------------------------------------------

// fakeStore serves a fixed ordered dataset, applying the same strictly-after
// filter the real store's GetLogsAfter does.
type fakeStore struct {
	logs []types.EvmLog
	err  error
}

func (f *fakeStore) Init(map[string]string) error                    { return nil }
func (f *fakeStore) InsertLogs([]types.EvmLog) error                 { return nil }
func (f *fakeStore) InsertTransactions([]types.EvmTransaction) error { return nil }
func (f *fakeStore) GetLogsCount() (uint64, error)                   { return uint64(len(f.logs)), nil }
func (f *fakeStore) GetLogs(uint64, uint64, uint64) ([]types.EvmLog, error) {
	return nil, nil
}
func (f *fakeStore) GetLogsAfter(sourceIds []uint64, afterBlock uint64, afterLogIndex uint64, toBlock uint64) ([]types.EvmLog, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := []types.EvmLog{}
	for _, l := range f.logs {
		if l.BlockNumber > toBlock {
			continue
		}
		after := l.BlockNumber > afterBlock || (l.BlockNumber == afterBlock && l.LogIndex > afterLogIndex)
		if after {
			out = append(out, l)
		}
	}
	return out, nil
}
func (f *fakeStore) GetLogStream(uint64, uint64, uint64, chan types.EvmLog) error { return nil }
func (f *fakeStore) GetLatestLogs(uint64, uint64) ([]types.EvmLog, error)         { return nil, nil }
func (f *fakeStore) GetTransactions(uint64, uint64, uint64) ([]types.EvmTransaction, error) {
	return nil, nil
}

// recordPlugin records the order of delivered logs and detects any concurrent
// (re-entrant) NewLogEvent call.
type recordPlugin struct {
	received []pluginsdk.LogEvent
	inCall   int32
	maxConc  int32
	failAt   int // 1-based position to fail at; 0 = never
}

func (r *recordPlugin) Name() string                 { return "record" }
func (r *recordPlugin) Init(pluginsdk.Context) error { return nil }
func (r *recordPlugin) Close() error                 { return nil }
func (r *recordPlugin) NewLogEvent(l pluginsdk.LogEvent) error {
	n := atomic.AddInt32(&r.inCall, 1)
	if n > atomic.LoadInt32(&r.maxConc) {
		atomic.StoreInt32(&r.maxConc, n)
	}
	defer atomic.AddInt32(&r.inCall, -1)

	r.received = append(r.received, l)
	if r.failAt != 0 && len(r.received) == r.failAt {
		return errors.New("boom")
	}
	return nil
}

// --- harness ---------------------------------------------------------------

func newTestService(t *testing.T, store log_stores.EvmIndexerStorage, plug pluginsdk.Exporter) *ExporterService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.EvmiExporter{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	exp := evmi_database.EvmiExporter{Name: "test", Enabled: true, SyncLogIndex: -1}
	if err := db.Create(&exp).Error; err != nil {
		t.Fatalf("seed exporter: %v", err)
	}

	return &ExporterService{
		db:       &evmi_database.EvmiDatabase{Conn: db},
		store:    log_stores.NewIndexerStore(store),
		plugin:   plug,
		exporter: exp,
		chain:    evmi_database.EvmBlockchain{ChainId: 1},
		logger:   zerolog.Nop(),
		running:  true,
	}
}

func logAt(block, idx uint64) types.EvmLog {
	return types.EvmLog{
		Id:          idKey(block, idx),
		SourceId:    1,
		ChainId:     1,
		BlockNumber: block,
		LogIndex:    idx,
	}
}

func idKey(block, idx uint64) string {
	return "1:" + itoa(block) + ":" + itoa(idx)
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// --- exportRange behaviour -------------------------------------------------

func TestExportRangeDeliversInOrderOneByOne(t *testing.T) {
	// Deliberately non-contiguous indices and a gap at block 12.
	store := &fakeStore{logs: []types.EvmLog{
		logAt(10, 0), logAt(10, 1), logAt(11, 0), logAt(13, 2), logAt(13, 5),
	}}
	plug := &recordPlugin{}
	svc := newTestService(t, store, plug)

	completed, lastIdx, err := svc.exportRange([]uint64{1}, 9, -1, 13)
	if err != nil {
		t.Fatalf("exportRange: %v", err)
	}

	want := []string{"1:10:0", "1:10:1", "1:11:0", "1:13:2", "1:13:5"}
	if len(plug.received) != len(want) {
		t.Fatalf("got %d logs, want %d", len(plug.received), len(want))
	}
	for i, id := range want {
		if plug.received[i].Id != id {
			t.Errorf("log %d = %s, want %s (order violated)", i, plug.received[i].Id, id)
		}
	}
	if plug.maxConc != 1 {
		t.Errorf("max concurrent NewLogEvent calls = %d, want 1 (must be strictly serial)", plug.maxConc)
	}

	// Range fully scanned: cursor is (toBlock, -1).
	if completed != 13 || lastIdx != -1 {
		t.Errorf("returned cursor = (%d,%d), want (13,-1)", completed, lastIdx)
	}
	assertPersisted(t, svc, 13, -1)
}

func TestExportRangeResumesStrictlyAfterCursor(t *testing.T) {
	store := &fakeStore{logs: []types.EvmLog{
		logAt(10, 0), logAt(10, 1), logAt(11, 0), logAt(13, 2), logAt(13, 5),
	}}
	plug := &recordPlugin{}
	svc := newTestService(t, store, plug)

	// Cursor: last executed log was (11,0) → completed=10, lastIdx=0.
	if _, _, err := svc.exportRange([]uint64{1}, 10, 0, 13); err != nil {
		t.Fatalf("exportRange: %v", err)
	}

	want := []string{"1:13:2", "1:13:5"}
	if len(plug.received) != len(want) {
		t.Fatalf("got %d logs, want %d: %+v", len(plug.received), len(want), plug.received)
	}
	for i, id := range want {
		if plug.received[i].Id != id {
			t.Errorf("log %d = %s, want %s", i, plug.received[i].Id, id)
		}
	}
}

func TestExportRangeStopsOnPluginErrorWithoutAdvancing(t *testing.T) {
	store := &fakeStore{logs: []types.EvmLog{
		logAt(10, 0), logAt(10, 1), logAt(11, 0), logAt(13, 2),
	}}
	plug := &recordPlugin{failAt: 3} // fail on (11,0)
	svc := newTestService(t, store, plug)

	completed, lastIdx, err := svc.exportRange([]uint64{1}, 9, -1, 13)
	if err == nil {
		t.Fatal("expected error from failing plugin, got nil")
	}

	// The failing log was delivered but not committed; later logs were NOT.
	if len(plug.received) != 3 {
		t.Fatalf("delivered %d logs, want 3 (must stop at the failure)", len(plug.received))
	}
	if plug.received[2].Id != "1:11:0" {
		t.Errorf("last delivered = %s, want 1:11:0", plug.received[2].Id)
	}

	// Cursor stays at the last successful log (10,1) → completed=9, lastIdx=1,
	// so a restart replays (11,0). It must not have advanced past the failure.
	if completed != 9 || lastIdx != 1 {
		t.Errorf("returned cursor = (%d,%d), want (9,1)", completed, lastIdx)
	}
	assertPersisted(t, svc, 9, 1)

	// Confirm the resume bound re-includes the failed log.
	ab, ai := cursorBound(completed, lastIdx)
	remaining, _ := store.GetLogsAfter([]uint64{1}, ab, ai, 13)
	if len(remaining) == 0 || remaining[0].Id != "1:11:0" {
		t.Errorf("resume should replay 1:11:0 first, got %+v", remaining)
	}
}

func assertPersisted(t *testing.T, svc *ExporterService, wantBlock uint64, wantIdx int64) {
	t.Helper()
	var got evmi_database.EvmiExporter
	if err := svc.db.Conn.First(&got, svc.exporter.ID).Error; err != nil {
		t.Fatalf("reload exporter: %v", err)
	}
	if got.SyncBlock != wantBlock || got.SyncLogIndex != wantIdx {
		t.Errorf("persisted cursor = (%d,%d), want (%d,%d)", got.SyncBlock, got.SyncLogIndex, wantBlock, wantIdx)
	}
}
