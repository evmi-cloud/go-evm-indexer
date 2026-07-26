package grpc

import (
	"context"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSourceServer(t *testing.T) *EvmIndexerServer {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "src.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.EvmLogSource{}, &evmi_database.EvmFactoryRule{}, &evmi_database.EvmFactoryRuleCondition{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &EvmIndexerServer{db: &evmi_database.EvmiDatabase{Conn: db}, logger: zerolog.Nop()}
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
