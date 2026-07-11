package gateway

import (
	"path/filepath"
	"testing"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newResolverDB(t *testing.T) *evmi_database.EvmiDatabase {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gw.db")), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&evmi_database.EvmiInstance{},
		&evmi_database.EvmLogPipeline{},
		&evmi_database.EvmLogSource{},
		&evmi_database.EvmiExporter{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &evmi_database.EvmiDatabase{Conn: db}
}

// seed builds: instance(10.0.0.1:9000) → pipeline → {source, exporter}.
func seed(t *testing.T, db *evmi_database.EvmiDatabase) (inst evmi_database.EvmiInstance, pipe evmi_database.EvmLogPipeline, src evmi_database.EvmLogSource, exp evmi_database.EvmiExporter) {
	t.Helper()
	inst = evmi_database.EvmiInstance{InstanceId: "i1", IpV4: "10.0.0.1", Port: 9000, Status: "RUNNING"}
	if err := db.Conn.Create(&inst).Error; err != nil {
		t.Fatal(err)
	}
	pipe = evmi_database.EvmLogPipeline{Name: "p1", EvmiInstanceID: inst.ID}
	if err := db.Conn.Create(&pipe).Error; err != nil {
		t.Fatal(err)
	}
	src = evmi_database.EvmLogSource{Type: "CONTRACT", EvmLogPipelineID: pipe.ID}
	if err := db.Conn.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	exp = evmi_database.EvmiExporter{Name: "e1", EvmLogPipelineID: pipe.ID}
	if err := db.Conn.Create(&exp).Error; err != nil {
		t.Fatal(err)
	}
	return
}

func TestResolveOwningInstanceAddress(t *testing.T) {
	db := newResolverDB(t)
	_, pipe, src, exp := seed(t, db)
	r := NewResolver(db, time.Minute)

	if addr, err := r.AddrForInstance(1); err != nil || addr != "10.0.0.1:9000" {
		t.Fatalf("AddrForInstance = %q, %v", addr, err)
	}
	if addr, err := r.AddrForPipeline(pipe.ID); err != nil || addr != "10.0.0.1:9000" {
		t.Fatalf("AddrForPipeline = %q, %v", addr, err)
	}
	if addr, err := r.AddrForSource(src.ID); err != nil || addr != "10.0.0.1:9000" {
		t.Fatalf("AddrForSource = %q, %v", addr, err)
	}
	if addr, err := r.AddrForExporter(exp.ID); err != nil || addr != "10.0.0.1:9000" {
		t.Fatalf("AddrForExporter = %q, %v", addr, err)
	}
}

func TestCacheServesAfterRowDeleted(t *testing.T) {
	db := newResolverDB(t)
	_, _, src, _ := seed(t, db)
	r := NewResolver(db, time.Minute)

	// Warm the cache.
	if _, err := r.AddrForSource(src.ID); err != nil {
		t.Fatal(err)
	}
	// Hard-delete the whole chain; a cached lookup must still resolve (proves the
	// DB isn't hit again within the TTL).
	db.Conn.Unscoped().Where("1 = 1").Delete(&evmi_database.EvmLogSource{})
	db.Conn.Unscoped().Where("1 = 1").Delete(&evmi_database.EvmLogPipeline{})
	db.Conn.Unscoped().Where("1 = 1").Delete(&evmi_database.EvmiInstance{})
	if addr, err := r.AddrForSource(src.ID); err != nil || addr != "10.0.0.1:9000" {
		t.Fatalf("cached AddrForSource = %q, %v (should be served from cache)", addr, err)
	}

	// After invalidation the lookup goes back to the (now empty) DB and fails.
	r.sourceToPipeline.invalidate(src.ID)
	if _, err := r.AddrForSource(src.ID); err == nil {
		t.Fatal("expected error after cache invalidation with deleted rows")
	}
}

func TestDefaultPortFallback(t *testing.T) {
	db := newResolverDB(t)
	inst := evmi_database.EvmiInstance{InstanceId: "old", IpV4: "10.0.0.9", Port: 0, Status: "RUNNING"}
	db.Conn.Create(&inst)
	r := NewResolver(db, time.Minute)
	if addr, err := r.AddrForInstance(inst.ID); err != nil || addr != "10.0.0.9:8080" {
		t.Fatalf("port-0 row should fall back to :8080, got %q, %v", addr, err)
	}
}

func TestAnyAddrRoundRobinsRunning(t *testing.T) {
	db := newResolverDB(t)
	db.Conn.Create(&evmi_database.EvmiInstance{InstanceId: "a", IpV4: "10.0.0.1", Port: 9000, Status: "RUNNING"})
	db.Conn.Create(&evmi_database.EvmiInstance{InstanceId: "b", IpV4: "10.0.0.2", Port: 9000, Status: "RUNNING"})
	db.Conn.Create(&evmi_database.EvmiInstance{InstanceId: "c", IpV4: "10.0.0.3", Port: 9000, Status: "STOPPED"})
	r := NewResolver(db, time.Minute)

	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		addr, err := r.AnyAddr()
		if err != nil {
			t.Fatal(err)
		}
		seen[addr]++
	}
	if seen["10.0.0.3:9000"] != 0 {
		t.Errorf("STOPPED instance should not be selected: %v", seen)
	}
	if len(seen) != 2 {
		t.Errorf("expected round-robin across the 2 running instances, got %v", seen)
	}
}

func TestAnyAddrFallsBackWhenNoneRunning(t *testing.T) {
	db := newResolverDB(t)
	db.Conn.Create(&evmi_database.EvmiInstance{InstanceId: "a", IpV4: "10.0.0.1", Port: 9000, Status: "STOPPED"})
	r := NewResolver(db, time.Minute)
	if addr, err := r.AnyAddr(); err != nil || addr != "10.0.0.1:9000" {
		t.Fatalf("should fall back to any instance when none RUNNING, got %q, %v", addr, err)
	}
}

func TestAnyAddrErrorsWhenNoInstances(t *testing.T) {
	db := newResolverDB(t)
	r := NewResolver(db, time.Minute)
	if _, err := r.AnyAddr(); err == nil {
		t.Fatal("expected error when no instances are registered")
	}
}
