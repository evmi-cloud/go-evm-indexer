package indexer

import (
	"database/sql"
	"path/filepath"
	"testing"

	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSourceIndexerForTest(t *testing.T, factory evmi_database.EvmLogSource) *SourceIndexerService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "idx.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.EvmLogSource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&factory).Error; err != nil {
		t.Fatalf("create factory: %v", err)
	}
	s := NewSourceIndexerService(&evmi_database.EvmiDatabase{Conn: db}, internal_bus.InitializeBus(), nil, factory)
	// The method reads p.pipeline.ID / p.source; set them from the persisted factory.
	s.source = factory
	s.pipeline = evmi_database.EvmLogPipeline{}
	s.pipeline.ID = factory.EvmLogPipelineID
	s.logger = zerolog.Nop()
	return s
}

func TestRegisterFactoryChildCreatesAndDedupes(t *testing.T) {
	factory := evmi_database.EvmLogSource{
		Enabled:                true,
		Type:                   string(evmi_database.FactoryLogSourceType),
		EvmLogPipelineID:       3,
		EvmBlockchainID:        2,
		FactoryChildEvmJsonABI: sql.NullInt32{Int32: 7, Valid: true},
	}
	s := newSourceIndexerForTest(t, factory)

	if err := s.registerFactoryChild("0xChildContract", 1200); err != nil {
		t.Fatalf("register: %v", err)
	}

	var children []evmi_database.EvmLogSource
	if err := s.db.Conn.Where("parent_source_id = ?", s.source.ID).Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}

	got := children[0]
	if got.Type != string(evmi_database.ContractLogSourceType) {
		t.Errorf("type = %q, want CONTRACT", got.Type)
	}
	if !got.Enabled {
		t.Error("factory child should be enabled")
	}
	if got.Address.String != "0xChildContract" || got.StartBlock != 1200 ||
		got.EvmLogPipelineID != 3 || got.EvmJsonAbiID != 7 || got.EvmBlockchainID != 2 ||
		got.ParentSourceID != s.source.ID {
		t.Errorf("fields not carried over: %+v", got)
	}

	// Re-seeing the same (factory, address) deployment must not duplicate.
	if err := s.registerFactoryChild("0xChildContract", 1200); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	var count int64
	s.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("parent_source_id = ?", s.source.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 child after re-register, got %d", count)
	}
}

func TestRegisterFactoryChildUniquePerFactory(t *testing.T) {
	factoryA := evmi_database.EvmLogSource{
		Type:             string(evmi_database.FactoryLogSourceType),
		EvmLogPipelineID: 3,
		EvmBlockchainID:  2,
	}
	sA := newSourceIndexerForTest(t, factoryA)

	// A second factory persisted in the same DB, sharing the child address.
	factoryB := evmi_database.EvmLogSource{
		Type:             string(evmi_database.FactoryLogSourceType),
		EvmLogPipelineID: 4,
		EvmBlockchainID:  2,
	}
	if err := sA.db.Conn.Create(&factoryB).Error; err != nil {
		t.Fatalf("create factory B: %v", err)
	}
	sB := NewSourceIndexerService(sA.db, internal_bus.InitializeBus(), nil, factoryB)
	sB.source = factoryB
	sB.pipeline.ID = factoryB.EvmLogPipelineID
	sB.logger = zerolog.Nop()

	if err := sA.registerFactoryChild("0xSharedChild", 100); err != nil {
		t.Fatalf("register A: %v", err)
	}
	// Same address from a different factory must still create (uniqueness is per factory).
	if err := sB.registerFactoryChild("0xSharedChild", 100); err != nil {
		t.Fatalf("register B: %v", err)
	}

	var count int64
	sA.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("address = ?", "0xSharedChild").Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 children (one per factory), got %d", count)
	}
}
