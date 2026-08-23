package exporter

import (
	"os"
	"path/filepath"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newPluginDB(t *testing.T) *evmi_database.EvmiDatabase {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "p.db")), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.Plugin{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &evmi_database.EvmiDatabase{Conn: db}
}

// Already installed and the binary is on disk → InstallPlugin does nothing (no
// rebuild), leaving the row untouched.
func TestInstallPluginSkipsWhenBinaryPresent(t *testing.T) {
	db := newPluginDB(t)
	so := filepath.Join(t.TempDir(), "p"+exeSuffix())
	if err := os.WriteFile(so, []byte("BIN"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := evmi_database.Plugin{
		Name:       "already-there",
		Status:     string(evmi_database.InstalledPluginStatus),
		BinaryPath: so,
		// A bogus git source that would fail to build — proving no build is attempted.
		GitUrl: "https://example.invalid/nope.git",
	}
	db.Conn.Create(&p)

	if err := InstallPlugin(db, p.ID, zerolog.Nop()); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
	var got evmi_database.Plugin
	db.Conn.First(&got, p.ID)
	if got.Status != string(evmi_database.InstalledPluginStatus) || got.BinaryPath != so {
		t.Errorf("row should be unchanged, got status=%q binary=%q", got.Status, got.BinaryPath)
	}
}

// Marked installed but the binary is gone → InstallPlugin does NOT skip; it
// attempts a build (which fails here for lack of a real source), ending in
// FAILED.
func TestInstallPluginBuildsWhenBinaryMissing(t *testing.T) {
	db := newPluginDB(t)
	p := evmi_database.Plugin{
		Name:       "missing-so",
		Status:     string(evmi_database.InstalledPluginStatus),
		BinaryPath: filepath.Join(t.TempDir(), "gone"), // does not exist
		// No GitUrl → build resolution fails deterministically (git is required).
	}
	db.Conn.Create(&p)

	if err := InstallPlugin(db, p.ID, zerolog.Nop()); err == nil {
		t.Fatal("expected a build attempt to fail (no source), got nil")
	}
	var got evmi_database.Plugin
	db.Conn.First(&got, p.ID)
	if got.Status != string(evmi_database.FailedPluginStatus) {
		t.Errorf("status = %q, want FAILED (build was attempted)", got.Status)
	}
}
