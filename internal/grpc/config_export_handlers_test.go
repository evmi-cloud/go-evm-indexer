package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/evmi-cloud/go-evm-indexer/internal/auth"
	"github.com/evmi-cloud/go-evm-indexer/internal/autoloader"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportConfiguration(t *testing.T) {
	e := newSourceServerWithStore(t)
	// database/metrics come from the loaded config; the export re-emits them verbatim.
	e.config.Database.Type = "SQLITE"
	e.config.Database.Config = map[string]string{"filename": "evmi.db"}
	e.config.Metrics.Enabled = true
	e.config.Metrics.Port = 9999
	if err := e.db.Conn.AutoMigrate(&evmi_database.EvmBlockchain{}, &evmi_database.EvmJsonAbi{},
		&evmi_database.EvmiExporter{}, &evmi_database.Plugin{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := e.db.Conn

	chain := evmi_database.EvmBlockchain{Name: "ethereum", ChainId: 1, RpcUrl: "http://rpc", BlockRange: 500}
	db.Create(&chain)
	abi := evmi_database.EvmJsonAbi{ContractName: "ERC20", Content: "[]"}
	db.Create(&abi)
	factoryAbi := evmi_database.EvmJsonAbi{ContractName: "UniswapV2Factory", Content: "[]"}
	db.Create(&factoryAbi)
	store := evmi_database.EvmLogStore{Identifier: "ch", Description: "d", StoreType: "clickhouse", StoreConfig: datatypes.JSON([]byte(`{"addr":"x"}`))}
	db.Create(&store)
	pipeline := evmi_database.EvmLogPipeline{Name: "main", EvmBlockchainID: chain.ID, EvmLogStoreId: store.ID}
	db.Create(&pipeline)

	contractSrc := evmi_database.EvmLogSource{Type: "CONTRACT", Enabled: true, StartBlock: 100,
		Address: sql.NullString{String: "0xabc", Valid: true}, EvmLogPipelineID: pipeline.ID, EvmBlockchainID: chain.ID, EvmJsonAbiID: abi.ID}
	db.Create(&contractSrc)

	factorySrc := evmi_database.EvmLogSource{Type: "FACTORY", Enabled: true, StartBlock: 50,
		Address: sql.NullString{String: "0xfac", Valid: true}, EvmLogPipelineID: pipeline.ID, EvmBlockchainID: chain.ID, EvmJsonAbiID: factoryAbi.ID}
	db.Create(&factorySrc)
	fid := factorySrc.ID
	rule := evmi_database.EvmFactoryRule{EvmLogSourceID: &fid, CreationFunctionName: "PairCreated", CreationAddressLogArg: "pair",
		ChildType: "CONTRACT", EvmJsonAbiID: abi.ID,
		Conditions: []evmi_database.EvmFactoryRuleCondition{{Arg: "x", Operator: "gte", Value: "1"}}}
	db.Create(&rule)

	// Factory-created child: must be excluded from the export.
	child := evmi_database.EvmLogSource{Type: "CONTRACT", Address: sql.NullString{String: "0xchild", Valid: true},
		ParentSourceID: factorySrc.ID, EvmLogPipelineID: pipeline.ID, EvmBlockchainID: chain.ID, EvmJsonAbiID: abi.ID}
	db.Create(&child)

	plugin := evmi_database.Plugin{Name: "myplugin", Description: "p", GitUrl: "http://git", GitRef: "main"}
	db.Create(&plugin)
	exporter := evmi_database.EvmiExporter{Name: "exp", EvmLogPipelineID: pipeline.ID, PluginID: plugin.ID, Enabled: true, StartBlock: 10}
	db.Create(&exporter)

	adminCtx := auth.WithUser(context.Background(), &evmi_database.User{Role: string(evmi_database.RoleAdmin)})
	resp, err := e.ExportConfiguration(adminCtx, connect.NewRequest(&evm_indexerv1.ExportConfigurationRequest{}))
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var cfg types.Config
	if err := json.Unmarshal([]byte(resp.Msg.ConfigJson), &cfg); err != nil {
		t.Fatalf("unmarshal exported json: %v", err)
	}

	// The non-DB config-file entries are re-emitted from the loaded config.
	if cfg.Database.Type != "SQLITE" || cfg.Database.Config["filename"] != "evmi.db" {
		t.Errorf("database config not exported: %+v", cfg.Database)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.Port != 9999 {
		t.Errorf("metrics config not exported: %+v", cfg.Metrics)
	}

	if len(cfg.Resources.Blockchains) != 1 || cfg.Resources.Blockchains[0].Name != "ethereum" || cfg.Resources.Blockchains[0].ChainId != 1 {
		t.Errorf("blockchains: %+v", cfg.Resources.Blockchains)
	}
	if len(cfg.Resources.Abis) != 2 {
		t.Errorf("expected 2 abis, got %d", len(cfg.Resources.Abis))
	}
	if len(cfg.Resources.Stores) != 1 || cfg.Resources.Stores[0].Identifier != "ch" || cfg.Resources.Stores[0].StoreType != "clickhouse" {
		t.Errorf("stores: %+v", cfg.Resources.Stores)
	}
	if len(cfg.Resources.Pipelines) != 1 || cfg.Resources.Pipelines[0].Blockchain != "ethereum" || cfg.Resources.Pipelines[0].Store != "ch" {
		t.Errorf("pipelines: %+v", cfg.Resources.Pipelines)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Name != "myplugin" || cfg.Plugins[0].GitRef != "main" {
		t.Errorf("plugins: %+v", cfg.Plugins)
	}
	if len(cfg.Resources.Exporters) != 1 || cfg.Resources.Exporters[0].Pipeline != "main" || cfg.Resources.Exporters[0].Plugin != "myplugin" {
		t.Errorf("exporters: %+v", cfg.Resources.Exporters)
	}

	// The factory-created child is excluded; the two declared sources remain.
	if len(cfg.Resources.Sources) != 2 {
		t.Fatalf("expected 2 sources (factory child excluded), got %d: %+v", len(cfg.Resources.Sources), cfg.Resources.Sources)
	}
	var fac *types.ConfigSource
	for i := range cfg.Resources.Sources {
		s := &cfg.Resources.Sources[i]
		if s.Pipeline != "main" || s.Blockchain != "ethereum" {
			t.Errorf("source cross-refs unresolved: %+v", s)
		}
		if s.Address == "0xchild" {
			t.Errorf("factory child leaked into export: %+v", s)
		}
		if s.Type == "FACTORY" {
			fac = s
		}
	}
	if fac == nil {
		t.Fatal("factory source missing from export")
	}
	if len(fac.FactoryRules) != 1 {
		t.Fatalf("expected 1 factory rule, got %d", len(fac.FactoryRules))
	}
	fr := fac.FactoryRules[0]
	if fr.CreationFunctionName != "PairCreated" || fr.ChildAbi != "ERC20" || fr.ChildType != "CONTRACT" {
		t.Errorf("factory rule wrong: %+v", fr)
	}
	if len(fr.Conditions) != 1 || fr.Conditions[0].Arg != "x" || fr.Conditions[0].Operator != "gte" {
		t.Errorf("factory rule conditions wrong: %+v", fr.Conditions)
	}

	// Round-trip: the exported resources reload through the autoloader into a fresh
	// DB, recreating every resource (plus the factory rule tree), and NOT the
	// runtime-only factory child.
	fresh, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "reload.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open fresh db: %v", err)
	}
	if err := fresh.AutoMigrate(&evmi_database.EvmBlockchain{}, &evmi_database.EvmJsonAbi{}, &evmi_database.EvmLogStore{},
		&evmi_database.EvmLogPipeline{}, &evmi_database.EvmLogSource{}, &evmi_database.EvmFactoryRule{},
		&evmi_database.EvmFactoryRuleCondition{}, &evmi_database.EvmiExporter{}, &evmi_database.Plugin{}); err != nil {
		t.Fatalf("migrate fresh: %v", err)
	}
	freshDB := &evmi_database.EvmiDatabase{Conn: fresh}
	// Plugins are imported before the autoloader runs (ImportConfigPlugins); mirror
	// that so the exporter's plugin reference resolves.
	fresh.Create(&evmi_database.Plugin{Name: "myplugin"})

	autoloader.Load(freshDB, 1, cfg.Resources, zerolog.Nop())

	var count int64
	fresh.Model(&evmi_database.EvmBlockchain{}).Count(&count)
	if count != 1 {
		t.Errorf("reload blockchains = %d, want 1", count)
	}
	fresh.Model(&evmi_database.EvmJsonAbi{}).Count(&count)
	if count != 2 {
		t.Errorf("reload abis = %d, want 2", count)
	}
	fresh.Model(&evmi_database.EvmLogPipeline{}).Count(&count)
	if count != 1 {
		t.Errorf("reload pipelines = %d, want 1", count)
	}
	// Only the two declared sources are recreated (factory child is not declared).
	fresh.Model(&evmi_database.EvmLogSource{}).Count(&count)
	if count != 2 {
		t.Errorf("reload sources = %d, want 2 (factory child must not reload)", count)
	}
	fresh.Model(&evmi_database.EvmiExporter{}).Count(&count)
	if count != 1 {
		t.Errorf("reload exporters = %d, want 1", count)
	}
	// The factory source's rule tree was recreated.
	var reloadedFactory evmi_database.EvmLogSource
	if err := fresh.Where("type = ?", "FACTORY").First(&reloadedFactory).Error; err != nil {
		t.Fatalf("reloaded factory source missing: %v", err)
	}
	fresh.Model(&evmi_database.EvmFactoryRule{}).Where("evm_log_source_id = ?", reloadedFactory.ID).Count(&count)
	if count != 1 {
		t.Errorf("reload factory rules = %d, want 1", count)
	}
}

// ExportConfiguration is admin-only (the output carries store/database secrets).
func TestExportConfigurationRequiresAdmin(t *testing.T) {
	e := newSourceServerWithStore(t)

	// No user in context → unauthenticated.
	if _, err := e.ExportConfiguration(context.Background(), connect.NewRequest(&evm_indexerv1.ExportConfigurationRequest{})); connect.CodeOf(err) == 0 {
		t.Fatalf("expected error without a user, got nil")
	}

	// Non-admin user → permission denied.
	userCtx := auth.WithUser(context.Background(), &evmi_database.User{Role: string(evmi_database.RoleUser)})
	_, err := e.ExportConfiguration(userCtx, connect.NewRequest(&evm_indexerv1.ExportConfigurationRequest{}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("non-admin export: want PermissionDenied, got %v", err)
	}
}
