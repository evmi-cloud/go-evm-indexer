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

func TestAggregateCursor(t *testing.T) {
	cases := []struct {
		name      string
		cursors   []*sourceCursor
		wantBlock uint64
		wantIdx   int64
	}{
		{"empty", nil, 0, -1},
		{"single", []*sourceCursor{{block: 10, logIndex: -1}}, 10, -1},
		{"min block wins", []*sourceCursor{
			{block: 50, logIndex: -1}, {block: 10, logIndex: 3}, {block: 30, logIndex: -1},
		}, 10, 3},
		{"same block: min log index wins", []*sourceCursor{
			{block: 10, logIndex: 4}, {block: 10, logIndex: -1},
		}, 10, -1},
	}
	for _, c := range cases {
		block, idx := aggregateCursor(c.cursors)
		if block != c.wantBlock || idx != c.wantIdx {
			t.Errorf("%s: aggregateCursor = (%d,%d), want (%d,%d)", c.name, block, idx, c.wantBlock, c.wantIdx)
		}
	}
}

func TestPipelineHead(t *testing.T) {
	got := pipelineHead([]*sourceCursor{{head: 10}, {head: 99}, {head: 42}})
	if got != 99 {
		t.Errorf("pipelineHead = %d, want 99", got)
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

// fakeStore serves a fixed ordered dataset, applying the same source filter and
// strictly-after filter the real store's GetLogsAfter does.
type fakeStore struct {
	logs []types.EvmLog
	err  error
	// calls records the source id sets of every GetLogsAfter call, so a test can
	// assert how many store queries a batch cost.
	calls [][]uint64
}

func (f *fakeStore) Init(map[string]string) error                    { return nil }
func (f *fakeStore) InsertLogs([]types.EvmLog) error                 { return nil }
func (f *fakeStore) InsertTransactions([]types.EvmTransaction) error { return nil }
func (f *fakeStore) GetLogsCount() (uint64, error)                   { return uint64(len(f.logs)), nil }
func (f *fakeStore) DeleteSourceData(uint64) error                   { return nil }
func (f *fakeStore) GetLogs(uint64, uint64, uint64) ([]types.EvmLog, error) {
	return nil, nil
}
func (f *fakeStore) GetLogsAfter(sourceIds []uint64, afterBlock uint64, afterLogIndex uint64, toBlock uint64) ([]types.EvmLog, error) {
	f.calls = append(f.calls, sourceIds)
	if f.err != nil {
		return nil, f.err
	}
	wanted := map[uint64]bool{}
	for _, id := range sourceIds {
		wanted[id] = true
	}
	out := []types.EvmLog{}
	for _, l := range f.logs {
		if !wanted[uint64(l.SourceId)] || l.BlockNumber > toBlock {
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

func (r *recordPlugin) ids() []string {
	out := make([]string, 0, len(r.received))
	for _, l := range r.received {
		out = append(out, l.Id)
	}
	return out
}

// --- harness ---------------------------------------------------------------

// newTestService builds an ExporterService over a private in-memory database
// holding one pipeline and one exporter. Sources are added per test with
// addSource, which is what drives the per-source cursors.
func newTestService(t *testing.T, store log_stores.EvmIndexerStorage, plug pluginsdk.Exporter) *ExporterService {
	t.Helper()
	// A DSN unique to the test: the cursor rows are shared state, so tests must
	// not see each other's sources.
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&evmi_database.EvmLogPipeline{},
		&evmi_database.EvmLogSource{},
		&evmi_database.EvmiExporter{},
		&evmi_database.EvmiExporterSourceCursor{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pipeline := evmi_database.EvmLogPipeline{Name: "pipeline"}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}

	exp := evmi_database.EvmiExporter{
		Name: "test", Enabled: true, SyncLogIndex: -1, EvmLogPipelineID: pipeline.ID,
	}
	if err := db.Create(&exp).Error; err != nil {
		t.Fatalf("seed exporter: %v", err)
	}

	return &ExporterService{
		db:       &evmi_database.EvmiDatabase{Conn: db},
		store:    log_stores.NewIndexerStore(store),
		plugin:   plug,
		exporter: exp,
		pipeline: pipeline,
		chain:    evmi_database.EvmBlockchain{ChainId: 1},
		cursors:  map[uint64]*sourceCursor{},
		logger:   zerolog.Nop(),
	}
}

// addSource attaches an enabled source to the service's pipeline. startBlock is
// the block *before* the first one indexed (the same convention the indexer and
// the factory/host use); syncBlock is how far the indexer has stored it.
func addSource(t *testing.T, svc *ExporterService, startBlock, syncBlock uint64) uint64 {
	t.Helper()
	src := evmi_database.EvmLogSource{
		Enabled:          true,
		Type:             "CONTRACT",
		StartBlock:       startBlock,
		SyncBlock:        syncBlock,
		EvmLogPipelineID: svc.pipeline.ID,
	}
	if err := svc.db.Conn.Create(&src).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}
	return uint64(src.ID)
}

// setSourceHead moves how far the indexer has stored a source.
func setSourceHead(t *testing.T, svc *ExporterService, sourceID, syncBlock uint64) {
	t.Helper()
	err := svc.db.Conn.Model(&evmi_database.EvmLogSource{}).
		Where("id = ?", sourceID).Update("sync_block", syncBlock).Error
	if err != nil {
		t.Fatalf("update source head: %v", err)
	}
}

// exportOnce runs a single loop iteration: reconcile cursors, then export a batch.
func exportOnce(t *testing.T, svc *ExporterService, batch uint64) ([]*sourceCursor, bool, error) {
	t.Helper()
	cursors, err := svc.loadCursors()
	if err != nil {
		t.Fatalf("loadCursors: %v", err)
	}
	exported, err := svc.exportStep(cursors, batch)
	return cursors, exported, err
}

func logAt(source, block, idx uint64) types.EvmLog {
	return types.EvmLog{
		Id:          idKey(source, block, idx),
		SourceId:    uint(source),
		ChainId:     1,
		BlockNumber: block,
		LogIndex:    idx,
	}
}

func idKey(source, block, idx uint64) string {
	return itoa(source) + ":" + itoa(block) + ":" + itoa(idx)
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

func assertDelivered(t *testing.T, plug *recordPlugin, want ...string) {
	t.Helper()
	got := plug.ids()
	if len(got) != len(want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivered %v, want %v (order violated at %d)", got, want, i)
		}
	}
}

func assertCursor(t *testing.T, svc *ExporterService, sourceID uint64, wantBlock uint64, wantIdx int64) {
	t.Helper()
	var row evmi_database.EvmiExporterSourceCursor
	err := svc.db.Conn.
		Where("evmi_exporter_id = ? AND evm_log_source_id = ?", svc.exporter.ID, sourceID).
		First(&row).Error
	if err != nil {
		t.Fatalf("load cursor for source %d: %v", sourceID, err)
	}
	if row.SyncBlock != wantBlock || row.SyncLogIndex != wantIdx {
		t.Errorf("source %d cursor = (%d,%d), want (%d,%d)",
			sourceID, row.SyncBlock, row.SyncLogIndex, wantBlock, wantIdx)
	}
}

// --- exportStep behaviour --------------------------------------------------

func TestExportStepDeliversInOrderOneByOne(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	src := addSource(t, svc, 9, 13)
	// Deliberately non-contiguous indices and a gap at block 12.
	store.logs = []types.EvmLog{
		logAt(src, 10, 0), logAt(src, 10, 1), logAt(src, 11, 0), logAt(src, 13, 2), logAt(src, 13, 5),
	}

	cursors, exported, err := exportOnce(t, svc, 100)
	if err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	if !exported {
		t.Fatal("exportStep reported nothing to export")
	}

	assertDelivered(t, plug, "1:10:0", "1:10:1", "1:11:0", "1:13:2", "1:13:5")
	if plug.maxConc != 1 {
		t.Errorf("max concurrent NewLogEvent calls = %d, want 1 (must be strictly serial)", plug.maxConc)
	}

	// Range fully scanned up to the source's head: cursor is (head, -1).
	assertCursor(t, svc, src, 13, -1)
	if block, idx := aggregateCursor(cursors); block != 13 || idx != -1 {
		t.Errorf("aggregate cursor = (%d,%d), want (13,-1)", block, idx)
	}

	// Nothing left: a second pass must find no work rather than replaying.
	if _, exported, err := exportOnce(t, svc, 100); err != nil || exported {
		t.Fatalf("second pass exported=%v err=%v, want (false, nil)", exported, err)
	}
	assertDelivered(t, plug, "1:10:0", "1:10:1", "1:11:0", "1:13:2", "1:13:5")
}

func TestExportStepStopsAtTheSourceHead(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	// The store holds a log at block 20, but the indexer has only committed up to
	// block 12 — exporting past the head would leave that block half-exported.
	src := addSource(t, svc, 9, 12)
	store.logs = []types.EvmLog{logAt(src, 10, 0), logAt(src, 20, 0)}

	if _, _, err := exportOnce(t, svc, 1000); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:10:0")
	assertCursor(t, svc, src, 12, -1)
}

func TestExportStepResumesStrictlyAfterCursor(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	src := addSource(t, svc, 9, 13)
	store.logs = []types.EvmLog{
		logAt(src, 10, 0), logAt(src, 10, 1), logAt(src, 11, 0), logAt(src, 13, 2), logAt(src, 13, 5),
	}

	// Cursor: last executed log was (11,0) → completed=10, lastIdx=0.
	cursors, err := svc.loadCursors()
	if err != nil {
		t.Fatalf("loadCursors: %v", err)
	}
	cursors[0].block, cursors[0].logIndex = 10, 0

	if _, err := svc.exportStep(cursors, 100); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:13:2", "1:13:5")
}

func TestExportStepStopsOnPluginErrorWithoutAdvancing(t *testing.T) {
	plug := &recordPlugin{failAt: 3} // fail on (11,0)
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	src := addSource(t, svc, 9, 13)
	store.logs = []types.EvmLog{
		logAt(src, 10, 0), logAt(src, 10, 1), logAt(src, 11, 0), logAt(src, 13, 2),
	}

	if _, _, err := exportOnce(t, svc, 100); err == nil {
		t.Fatal("expected error from failing plugin, got nil")
	}

	// The failing log was delivered but not committed; later logs were NOT.
	assertDelivered(t, plug, "1:10:0", "1:10:1", "1:11:0")

	// Cursor stays at the last successful log (10,1) → completed=9, lastIdx=1,
	// so a restart replays (11,0). It must not have advanced past the failure.
	assertCursor(t, svc, src, 9, 1)

	// Confirm the resume bound re-includes the failed log.
	ab, ai := cursorBound(9, 1)
	remaining, _ := store.GetLogsAfter([]uint64{src}, ab, ai, 13)
	if len(remaining) == 0 || remaining[0].Id != "1:11:0" {
		t.Errorf("resume should replay 1:11:0 first, got %+v", remaining)
	}
}

// --- per-source cursors ----------------------------------------------------

// A source attached long after the exporter started — by a FACTORY rule or by a
// plugin calling Host.CreateLogSource — must be exported from its own first
// stored log. Under a single pipeline-wide cursor its whole backlog below the
// exporter's position was silently dropped.
func TestSourceAddedLateIsExportedFromItsOwnStartBlock(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)

	original := addSource(t, svc, 0, 100)
	store.logs = []types.EvmLog{logAt(original, 10, 0), logAt(original, 100, 0)}

	if _, exported, err := exportOnce(t, svc, 1000); err != nil || !exported {
		t.Fatalf("first pass: exported=%v err=%v", exported, err)
	}
	assertDelivered(t, plug, "1:10:0", "1:100:0")
	assertCursor(t, svc, original, 100, -1)

	// A plugin resolves a deployment at block 50 and registers it. The source is
	// created just before its first block and the indexer backfills it to 100.
	late := addSource(t, svc, 49, 100)
	store.logs = append(store.logs, logAt(late, 50, 0), logAt(late, 60, 3), logAt(late, 100, 1))

	if _, exported, err := exportOnce(t, svc, 1000); err != nil || !exported {
		t.Fatalf("second pass: exported=%v err=%v", exported, err)
	}

	// Every log of the late source is delivered, including the ones stored below
	// the block the original source had already reached.
	assertDelivered(t, plug, "1:10:0", "1:100:0", "2:50:0", "2:60:3", "2:100:1")
	assertCursor(t, svc, late, 100, -1)
	assertCursor(t, svc, original, 100, -1)
}

// A late source must not drag the sources that are already caught up backwards,
// nor stall them: each advances against its own head.
func TestLaggingSourceDoesNotStallTheOthers(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)

	ahead := addSource(t, svc, 0, 100)
	// behind has been created but the indexer has not stored anything for it yet
	// (SyncBlock == StartBlock), so it contributes nothing this pass.
	behind := addSource(t, svc, 49, 49)
	store.logs = []types.EvmLog{logAt(ahead, 100, 0), logAt(behind, 50, 0)}

	if _, exported, err := exportOnce(t, svc, 1000); err != nil || !exported {
		t.Fatalf("first pass: exported=%v err=%v", exported, err)
	}
	assertDelivered(t, plug, "1:100:0")
	assertCursor(t, svc, ahead, 100, -1)
	assertCursor(t, svc, behind, 49, -1)

	// The indexer catches the new source up; its backlog is exported next pass.
	setSourceHead(t, svc, behind, 100)
	if _, exported, err := exportOnce(t, svc, 1000); err != nil || !exported {
		t.Fatalf("second pass: exported=%v err=%v", exported, err)
	}
	assertDelivered(t, plug, "1:100:0", "2:50:0")
	assertCursor(t, svc, behind, 100, -1)
}

// Sources in lockstep are merged into one ordered stream and cost one store query.
func TestExportStepMergesSourcesInBlockOrder(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)

	a := addSource(t, svc, 0, 20)
	b := addSource(t, svc, 0, 20)
	store.logs = []types.EvmLog{
		logAt(a, 10, 0), logAt(b, 10, 1), logAt(b, 11, 0), logAt(a, 12, 4), logAt(b, 20, 0),
	}

	if _, _, err := exportOnce(t, svc, 1000); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:10:0", "2:10:1", "2:11:0", "1:12:4", "2:20:0")

	if len(store.calls) != 1 {
		t.Errorf("store queries = %d, want 1 (sources in lockstep must be fetched together)", len(store.calls))
	}
	assertCursor(t, svc, a, 20, -1)
	assertCursor(t, svc, b, 20, -1)
}

// A restart must resume from the persisted per-source cursors, not replay.
func TestCursorsAreReloadedFromTheDatabase(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	src := addSource(t, svc, 0, 20)
	store.logs = []types.EvmLog{logAt(src, 10, 0), logAt(src, 20, 0)}

	if _, _, err := exportOnce(t, svc, 1000); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:10:0", "1:20:0")

	// Simulate a supervisor restart: fresh in-memory state, same database.
	svc.cursors = map[uint64]*sourceCursor{}
	if _, exported, err := exportOnce(t, svc, 1000); err != nil || exported {
		t.Fatalf("after restart: exported=%v err=%v, want (false, nil)", exported, err)
	}
	assertDelivered(t, plug, "1:10:0", "1:20:0")
}

// The exporter's own StartBlock still gates every source, including a late one.
func TestExporterStartBlockGatesNewSources(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	svc.exporter.StartBlock = 80

	src := addSource(t, svc, 0, 100)
	store.logs = []types.EvmLog{logAt(src, 10, 0), logAt(src, 90, 0)}

	if _, _, err := exportOnce(t, svc, 1000); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:90:0")
}

// A disabled source keeps its cursor: re-enabling it resumes, it does not replay.
func TestDisabledSourceKeepsItsCursor(t *testing.T) {
	plug := &recordPlugin{}
	store := &fakeStore{}
	svc := newTestService(t, store, plug)
	src := addSource(t, svc, 0, 20)
	store.logs = []types.EvmLog{logAt(src, 10, 0), logAt(src, 30, 0)}

	if _, _, err := exportOnce(t, svc, 1000); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:10:0")

	if err := svc.db.Conn.Model(&evmi_database.EvmLogSource{}).
		Where("id = ?", src).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable source: %v", err)
	}
	cursors, exported, err := exportOnce(t, svc, 1000)
	if err != nil || exported || len(cursors) != 0 {
		t.Fatalf("while disabled: cursors=%d exported=%v err=%v", len(cursors), exported, err)
	}

	// Re-enable and let the indexer move on: only the new log is delivered.
	if err := svc.db.Conn.Model(&evmi_database.EvmLogSource{}).
		Where("id = ?", src).Updates(map[string]interface{}{"enabled": true, "sync_block": 30}).Error; err != nil {
		t.Fatalf("re-enable source: %v", err)
	}
	if _, _, err := exportOnce(t, svc, 1000); err != nil {
		t.Fatalf("exportStep: %v", err)
	}
	assertDelivered(t, plug, "1:10:0", "1:30:0")
}
