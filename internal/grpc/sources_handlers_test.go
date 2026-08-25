package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSourceServer(t *testing.T) *EvmIndexerServer {
	t.Helper()
	// Enforce foreign keys (off by default in SQLite) so the round-trip test exercises
	// the same referential-integrity rules as Postgres/MySQL (factory-rule owner
	// columns must be NULL, not 0).
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "src.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.EvmLogSource{}, &evmi_database.EvmFactoryRule{}, &evmi_database.EvmFactoryRuleCondition{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &EvmIndexerServer{db: &evmi_database.EvmiDatabase{Conn: db}, logger: zerolog.Nop()}
}

// newSourceServerWithStore is like newSourceServer but also migrates the pipeline
// and store tables and does NOT enforce foreign keys, so the cascade-delete test
// can wire sources to a real store without providing every referenced row.
func newSourceServerWithStore(t *testing.T) *EvmIndexerServer {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "src.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.EvmLogSource{}, &evmi_database.EvmFactoryRule{}, &evmi_database.EvmFactoryRuleCondition{},
		&evmi_database.EvmLogPipeline{}, &evmi_database.EvmLogStore{},
		&evmi_database.EvmiExporterSourceCursor{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &EvmIndexerServer{db: &evmi_database.EvmiDatabase{Conn: db}, bus: internal_bus.InitializeBus(), logger: zerolog.Nop()}
}

// A FACTORY source's recursive rule tree round-trips through create → get, and an
// update replaces it wholesale.
func TestFactoryRulesRoundTrip(t *testing.T) {
	e := newSourceServer(t)
	ctx := context.Background()

	// Two top-level rules; the second spawns a nested factory with its own rule.
	rules := []*evm_indexerv1.FactoryRule{
		{CreationFunctionName: "TokenCreated", CreationAddressLogArg: "token", ChildType: "CONTRACT", EvmJsonAbiId: 1,
			Conditions: []*evm_indexerv1.FactoryRuleCondition{
				{Arg: "decimals", Operator: "gte", Value: "6"},
				{Arg: "kind", Operator: "eq", Value: "erc20"},
			}},
		{
			CreationFunctionName: "SubFactoryCreated", CreationAddressLogArg: "subFactory", ChildType: "FACTORY", EvmJsonAbiId: 2,
			ChildRules: []*evm_indexerv1.FactoryRule{
				{CreationFunctionName: "PoolCreated", CreationAddressLogArg: "pool", ChildType: "CONTRACT", EvmJsonAbiId: 3},
			},
		},
	}

	created, err := e.CreateEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.CreateEvmLogSourceRequest{
		Source: &evm_indexerv1.EvmLogSource{Type: "FACTORY", EvmJsonAbiId: 9, FactoryRules: rules},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Msg.Id

	got, err := e.GetEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.GetEvmLogSourceRequest{Id: id}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	gr := got.Msg.Source.FactoryRules
	if len(gr) != 2 {
		t.Fatalf("expected 2 top-level rules, got %d", len(gr))
	}
	if gr[0].CreationFunctionName != "TokenCreated" || gr[0].ChildType != "CONTRACT" || gr[0].EvmJsonAbiId != 1 {
		t.Errorf("rule 0 wrong: %+v", gr[0])
	}
	if len(gr[0].Conditions) != 2 {
		t.Fatalf("rule 0 conditions not round-tripped: %+v", gr[0].Conditions)
	}
	if gr[0].Conditions[0].Arg != "decimals" || gr[0].Conditions[0].Operator != "gte" || gr[0].Conditions[0].Value != "6" {
		t.Errorf("condition 0 wrong: %+v", gr[0].Conditions[0])
	}
	if gr[1].ChildType != "FACTORY" || len(gr[1].ChildRules) != 1 {
		t.Fatalf("rule 1 nested tree missing: %+v", gr[1])
	}
	if gr[1].ChildRules[0].CreationFunctionName != "PoolCreated" || gr[1].ChildRules[0].EvmJsonAbiId != 3 {
		t.Errorf("nested rule wrong: %+v", gr[1].ChildRules[0])
	}

	// Update to a single rule; the old tree (incl. nested) must be gone.
	_, err = e.UpdateEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.UpdateEvmLogSourceRequest{
		Source: &evm_indexerv1.EvmLogSource{Id: &id, Type: "FACTORY", EvmJsonAbiId: 9, FactoryRules: []*evm_indexerv1.FactoryRule{
			{CreationFunctionName: "OnlyOne", CreationAddressLogArg: "addr", ChildType: "CONTRACT", EvmJsonAbiId: 4},
		}},
	}))
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got2, _ := e.GetEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.GetEvmLogSourceRequest{Id: id}))
	if len(got2.Msg.Source.FactoryRules) != 1 || got2.Msg.Source.FactoryRules[0].CreationFunctionName != "OnlyOne" {
		t.Fatalf("update did not replace rules: %+v", got2.Msg.Source.FactoryRules)
	}
	// No orphaned rule rows remain for this source.
	var total int64
	e.db.Conn.Model(&evmi_database.EvmFactoryRule{}).Count(&total)
	if total != 1 {
		t.Errorf("expected exactly 1 rule row after replace, got %d (orphans not cleaned)", total)
	}
	// The replaced rule's conditions must be gone too (no orphaned condition rows).
	var totalConds int64
	e.db.Conn.Model(&evmi_database.EvmFactoryRuleCondition{}).Count(&totalConds)
	if totalConds != 0 {
		t.Errorf("expected 0 condition rows after replace, got %d (orphans not cleaned)", totalConds)
	}
}

// A factory-created child source (ParentSourceID != 0) is read-only: Update and
// Delete are rejected with FailedPrecondition; only start/stop are allowed.
func TestFactoryChildSourceReadOnly(t *testing.T) {
	e := newSourceServer(t)
	ctx := context.Background()

	child := evmi_database.EvmLogSource{Type: "CONTRACT", ParentSourceID: 42}
	if err := e.db.Conn.Create(&child).Error; err != nil {
		t.Fatalf("seed child: %v", err)
	}
	id := uint32(child.ID)

	_, err := e.UpdateEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.UpdateEvmLogSourceRequest{
		Source: &evm_indexerv1.EvmLogSource{Id: &id, Type: "CONTRACT"},
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("update child: want FailedPrecondition, got %v", err)
	}

	_, err = e.DeleteEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.DeleteEvmLogSourceRequest{Id: id}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("delete child: want FailedPrecondition, got %v", err)
	}

	// The child row must still exist (delete was refused).
	var count int64
	e.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("id = ?", child.ID).Count(&count)
	if count != 1 {
		t.Fatalf("child source should still exist after refused delete, count=%d", count)
	}
}

// Deleting a FACTORY source cascades: every descendant source (child + grandchild)
// is removed and each source's stored logs/transactions are deleted from the store.
func TestDeleteSourceCascadesSubtreeAndStoreData(t *testing.T) {
	e := newSourceServerWithStore(t)
	ctx := context.Background()

	// A parquet store rooted at a temp dir, wired to a pipeline.
	dir := t.TempDir()
	cfg, _ := json.Marshal(map[string]string{"path": dir})
	store := evmi_database.EvmLogStore{StoreType: "parquet", StoreConfig: datatypes.JSON(cfg)}
	if err := e.db.Conn.Create(&store).Error; err != nil {
		t.Fatalf("create store: %v", err)
	}
	pipeline := evmi_database.EvmLogPipeline{EvmLogStoreId: store.ID}
	if err := e.db.Conn.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	// factory -> child -> grandchild, all on the pipeline.
	factory := evmi_database.EvmLogSource{Type: "FACTORY", EvmLogPipelineID: pipeline.ID}
	e.db.Conn.Create(&factory)
	child := evmi_database.EvmLogSource{Type: "CONTRACT", EvmLogPipelineID: pipeline.ID, ParentSourceID: factory.ID}
	e.db.Conn.Create(&child)
	grand := evmi_database.EvmLogSource{Type: "CONTRACT", EvmLogPipelineID: pipeline.ID, ParentSourceID: child.ID}
	e.db.Conn.Create(&grand)
	allIDs := []uint{factory.ID, child.ID, grand.ID}

	// Each source also carries an exporter cursor, which the cascade must clear.
	for _, id := range allIDs {
		e.db.Conn.Create(&evmi_database.EvmiExporterSourceCursor{
			EvmiExporterID: 1, EvmLogSourceID: id, SyncBlock: 5, SyncLogIndex: -1,
		})
	}

	// Seed stored logs for each source through the same parquet store.
	ps, err := log_stores.LoadStore("parquet", map[string]string{"path": dir}, zerolog.Nop())
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	storage := ps.GetStorage()
	for _, id := range allIDs {
		if err := storage.InsertLogs([]types.EvmLog{{Id: fmt.Sprintf("log-%d", id), SourceId: id, ChainId: 1, BlockNumber: 1}}); err != nil {
			t.Fatalf("seed logs for %d: %v", id, err)
		}
	}
	if logs, _ := storage.GetLogs(uint64(child.ID), 0, 100); len(logs) != 1 {
		t.Fatalf("seed sanity: child should have 1 log")
	}

	// Delete the factory → cascade to the whole subtree.
	if _, err := e.DeleteEvmLogSource(ctx, connect.NewRequest(&evm_indexerv1.DeleteEvmLogSourceRequest{Id: uint32(factory.ID)})); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Every source row is gone.
	var count int64
	e.db.Conn.Model(&evmi_database.EvmLogSource{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 sources after cascade, got %d", count)
	}

	// No export cursor is left pointing at a source that no longer exists.
	e.db.Conn.Model(&evmi_database.EvmiExporterSourceCursor{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 exporter source cursors after cascade, got %d", count)
	}

	// Stored data is gone for every source in the subtree.
	for _, id := range allIDs {
		logs, err := storage.GetLogs(uint64(id), 0, 100)
		if err != nil {
			t.Fatalf("get logs %d: %v", id, err)
		}
		if len(logs) != 0 {
			t.Errorf("source %d store data not deleted: %d logs remain", id, len(logs))
		}
	}
}
