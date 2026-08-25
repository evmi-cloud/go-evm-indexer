package evmi_database

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "meta.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&EvmLogSource{}, &EvmiExporter{}, &EvmiExporterSourceCursor{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func cursorsOf(t *testing.T, db *gorm.DB, exporterID uint) map[uint]EvmiExporterSourceCursor {
	t.Helper()
	var rows []EvmiExporterSourceCursor
	if err := db.Where("evmi_exporter_id = ?", exporterID).Find(&rows).Error; err != nil {
		t.Fatalf("load cursors: %v", err)
	}
	out := map[uint]EvmiExporterSourceCursor{}
	for _, r := range rows {
		out[r.EvmLogSourceID] = r
	}
	return out
}

// An exporter that already made progress under the old single-cursor scheme gets
// that position copied onto every source of its pipeline, so upgrading does not
// replay the whole pipeline into the plugin.
func TestBackfillCopiesTheAggregateCursorOntoEverySource(t *testing.T) {
	db := newMigrationDB(t)

	a := EvmLogSource{EvmLogPipelineID: 1, Enabled: true}
	b := EvmLogSource{EvmLogPipelineID: 1, Enabled: false}
	other := EvmLogSource{EvmLogPipelineID: 2, Enabled: true}
	db.Create(&a)
	db.Create(&b)
	db.Create(&other)

	exp := EvmiExporter{Name: "e", EvmLogPipelineID: 1, SyncBlock: 900, SyncLogIndex: 4}
	db.Create(&exp)

	if err := backfillExporterSourceCursors(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	got := cursorsOf(t, db, exp.ID)
	if len(got) != 2 {
		t.Fatalf("backfilled %d cursors, want 2 (both sources of the pipeline)", len(got))
	}
	if _, ok := got[other.ID]; ok {
		t.Error("backfilled a source from another pipeline")
	}
	for _, id := range []uint{a.ID, b.ID} {
		if c := got[id]; c.SyncBlock != 900 || c.SyncLogIndex != 4 {
			t.Errorf("source %d cursor = (%d,%d), want (900,4)", id, c.SyncBlock, c.SyncLogIndex)
		}
	}
}

// The backfill runs once: an exporter that already has cursors is left alone, so a
// source attached after the migration stays new and is exported from its own start.
func TestBackfillSkipsExportersThatAlreadyHaveCursors(t *testing.T) {
	db := newMigrationDB(t)

	existing := EvmLogSource{EvmLogPipelineID: 1}
	added := EvmLogSource{EvmLogPipelineID: 1}
	db.Create(&existing)
	db.Create(&added)

	exp := EvmiExporter{Name: "e", EvmLogPipelineID: 1, SyncBlock: 900, SyncLogIndex: -1}
	db.Create(&exp)
	db.Create(&EvmiExporterSourceCursor{
		EvmiExporterID: exp.ID, EvmLogSourceID: existing.ID, SyncBlock: 900, SyncLogIndex: -1,
	})

	if err := backfillExporterSourceCursors(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	got := cursorsOf(t, db, exp.ID)
	if len(got) != 1 {
		t.Fatalf("cursor count = %d, want 1 (backfill must not run twice)", len(got))
	}
	if _, ok := got[added.ID]; ok {
		t.Error("the source added after the migration must stay uncursored, not inherit the exporter's position")
	}
}

// An exporter that never progressed needs no backfill: seeding from each source's
// StartBlock on first run covers the same range.
func TestBackfillSkipsExportersThatNeverRan(t *testing.T) {
	db := newMigrationDB(t)

	src := EvmLogSource{EvmLogPipelineID: 1}
	db.Create(&src)
	exp := EvmiExporter{Name: "e", EvmLogPipelineID: 1, SyncBlock: 0, SyncLogIndex: -1}
	db.Create(&exp)

	if err := backfillExporterSourceCursors(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cursorsOf(t, db, exp.ID); len(got) != 0 {
		t.Errorf("cursor count = %d, want 0", len(got))
	}
}

// Backfill is idempotent: a second boot must not duplicate rows.
func TestBackfillIsIdempotent(t *testing.T) {
	db := newMigrationDB(t)

	src := EvmLogSource{EvmLogPipelineID: 1}
	db.Create(&src)
	exp := EvmiExporter{Name: "e", EvmLogPipelineID: 1, SyncBlock: 10, SyncLogIndex: -1}
	db.Create(&exp)

	for i := 0; i < 2; i++ {
		if err := backfillExporterSourceCursors(db); err != nil {
			t.Fatalf("backfill %d: %v", i, err)
		}
	}
	if got := cursorsOf(t, db, exp.ID); len(got) != 1 {
		t.Errorf("cursor count = %d, want 1", len(got))
	}
}
