package grpc

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newCursorServer(t *testing.T) *EvmIndexerServer {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cursors.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&evmi_database.EvmLogSource{},
		&evmi_database.EvmiExporter{},
		&evmi_database.EvmiExporterSourceCursor{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &EvmIndexerServer{db: &evmi_database.EvmiDatabase{Conn: db}, logger: zerolog.Nop()}
}

// The list reports every source of the exporter's pipeline: the ones it has a
// cursor for at that cursor, and the ones it has not reached yet at their own
// StartBlock, with lag measured against each source's indexed head.
func TestListEvmiExporterSourceCursors(t *testing.T) {
	e := newCursorServer(t)
	ctx := context.Background()

	exporter := evmi_database.EvmiExporter{Name: "exp", EvmLogPipelineID: 1, SyncBlock: 100}
	e.db.Conn.Create(&exporter)

	tracked := evmi_database.EvmLogSource{
		EvmLogPipelineID: 1, Type: "CONTRACT", Enabled: true, Status: "RUNNING",
		StartBlock: 0, SyncBlock: 150,
		Address: sql.NullString{String: "0xabc", Valid: true},
	}
	e.db.Conn.Create(&tracked)

	// Attached by a factory but not yet picked up by the exporter: no cursor row.
	untracked := evmi_database.EvmLogSource{
		EvmLogPipelineID: 1, Type: "CONTRACT", Enabled: true, Status: "STOPPED",
		StartBlock: 49, SyncBlock: 60, ParentSourceID: tracked.ID,
		Address: sql.NullString{String: "0xdef", Valid: true},
	}
	e.db.Conn.Create(&untracked)

	// A source of another pipeline must not appear.
	other := evmi_database.EvmLogSource{EvmLogPipelineID: 2, Type: "CONTRACT", SyncBlock: 5}
	e.db.Conn.Create(&other)

	e.db.Conn.Create(&evmi_database.EvmiExporterSourceCursor{
		EvmiExporterID: exporter.ID, EvmLogSourceID: tracked.ID, SyncBlock: 100, SyncLogIndex: 3,
	})

	resp, err := e.ListEvmiExporterSourceCursors(ctx, connect.NewRequest(
		&evm_indexerv1.ListEvmiExporterSourceCursorsRequest{ExporterId: uint32(exporter.ID)}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := resp.Msg.Cursors
	if len(got) != 2 {
		t.Fatalf("got %d cursors, want 2 (only this pipeline's sources)", len(got))
	}

	byID := map[uint32]*evm_indexerv1.EvmiExporterSourceCursor{}
	for _, c := range got {
		byID[c.SourceId] = c
	}

	a := byID[uint32(tracked.ID)]
	if a == nil {
		t.Fatal("tracked source missing from the list")
	}
	if a.SyncBlock != 100 || a.SyncLogIndex != 3 {
		t.Errorf("tracked cursor = (%d,%d), want (100,3)", a.SyncBlock, a.SyncLogIndex)
	}
	if a.SourceSyncBlock != 150 || a.LagBlocks != 50 {
		t.Errorf("tracked head/lag = (%d,%d), want (150,50)", a.SourceSyncBlock, a.LagBlocks)
	}
	if a.SourceAddress != "0xabc" || a.SourceType != "CONTRACT" || !a.SourceEnabled {
		t.Errorf("tracked source metadata not carried: %+v", a)
	}

	b := byID[uint32(untracked.ID)]
	if b == nil {
		t.Fatal("untracked source missing from the list")
	}
	// No cursor row yet → reported where the exporter would seed it.
	if b.SyncBlock != 49 || b.SyncLogIndex != -1 {
		t.Errorf("untracked cursor = (%d,%d), want (49,-1)", b.SyncBlock, b.SyncLogIndex)
	}
	if b.LagBlocks != 11 {
		t.Errorf("untracked lag = %d, want 11", b.LagBlocks)
	}
	if b.ParentSourceId != uint32(tracked.ID) {
		t.Errorf("parent source = %d, want %d", b.ParentSourceId, tracked.ID)
	}
}

// A cursor ahead of its source's indexed head reports zero lag rather than
// underflowing (a freshly seeded cursor sits at StartBlock, which can exceed
// SyncBlock before the indexer has stored anything).
func TestListEvmiExporterSourceCursorsClampsLag(t *testing.T) {
	e := newCursorServer(t)

	exporter := evmi_database.EvmiExporter{Name: "exp", EvmLogPipelineID: 1}
	e.db.Conn.Create(&exporter)
	src := evmi_database.EvmLogSource{EvmLogPipelineID: 1, Type: "CONTRACT", StartBlock: 500, SyncBlock: 100}
	e.db.Conn.Create(&src)

	resp, err := e.ListEvmiExporterSourceCursors(context.Background(), connect.NewRequest(
		&evm_indexerv1.ListEvmiExporterSourceCursorsRequest{ExporterId: uint32(exporter.ID)}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := resp.Msg.Cursors[0].LagBlocks; got != 0 {
		t.Errorf("lag = %d, want 0 (must clamp, not underflow)", got)
	}
}

func TestListEvmiExporterSourceCursorsUnknownExporter(t *testing.T) {
	e := newCursorServer(t)
	_, err := e.ListEvmiExporterSourceCursors(context.Background(), connect.NewRequest(
		&evm_indexerv1.ListEvmiExporterSourceCursorsRequest{ExporterId: 999}))
	if err == nil {
		t.Fatal("expected an error for an unknown exporter")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

// End-to-end: an exporter.cursor event emitted on the bus reaches a streaming
// client, and the exporter filter is honored.
func TestStreamEvmiExporterSourceCursors(t *testing.T) {
	b := internal_bus.InitializeBus()
	server := &EvmIndexerServer{bus: b, logger: zerolog.Nop()}

	mux := http.NewServeMux()
	path, handler := evm_indexerv1connect.NewEvmIndexerServiceHandler(server)
	mux.Handle(path, handler)
	ts := httptest.NewUnstartedServer(mux)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	client := evm_indexerv1connect.NewEvmIndexerServiceClient(ts.Client(), ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Emit continuously (started before the stream opens) to avoid racing the
	// server-side handler registration: one event for the wrong exporter (must be
	// filtered out) and one for exporter 2.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				b.Emit(context.Background(), internal_bus.ExporterCursorTopic,
					types.ExporterCursorUpdate{ExporterID: 1, SourceID: 11, SyncBlock: 5})
				b.Emit(context.Background(), internal_bus.ExporterCursorTopic,
					types.ExporterCursorUpdate{
						ExporterID: 2, SourceID: 22, SyncBlock: 90, SyncLogIndex: -1,
						SourceSyncBlock: 100, SourceType: "FACTORY", SourceEnabled: true,
					})
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	defer close(done)

	stream, err := client.StreamEvmiExporterSourceCursors(ctx, connect.NewRequest(
		&evm_indexerv1.StreamEvmiExporterSourceCursorsRequest{ExporterId: 2}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	if !stream.Receive() {
		t.Fatalf("stream closed without a message: %v", stream.Err())
	}
	got := stream.Msg()
	cancel() // end the server-side handler promptly so teardown is fast

	if got.ExporterId != 2 || got.SourceId != 22 {
		t.Fatalf("got cursor for exporter %d source %d, want 2/22 (filter not applied)", got.ExporterId, got.SourceId)
	}
	if got.SyncBlock != 90 || got.SourceSyncBlock != 100 || got.LagBlocks != 10 {
		t.Errorf("cursor = (sync %d, head %d, lag %d), want (90,100,10)", got.SyncBlock, got.SourceSyncBlock, got.LagBlocks)
	}
	if got.SourceType != "FACTORY" || !got.SourceEnabled {
		t.Errorf("source metadata not carried through the stream: %+v", got)
	}
}
