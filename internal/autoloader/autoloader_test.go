package autoloader

import (
	"path/filepath"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newDBForTest(t *testing.T) *evmi_database.EvmiDatabase {
	t.Helper()
	// Silent: create-if-not-exists intentionally probes with First(), whose misses
	// gorm would otherwise log as errors.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auto.db")), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&evmi_database.EvmiInstance{},
		&evmi_database.EvmBlockchain{},
		&evmi_database.EvmJsonAbi{},
		&evmi_database.EvmLogStore{},
		&evmi_database.EvmLogPipeline{},
		&evmi_database.EvmLogSource{},
		&evmi_database.EvmiExporter{},
		&evmi_database.Plugin{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &evmi_database.EvmiDatabase{Conn: db}
}

func fullConfig() types.AutoloadResources {
	return types.AutoloadResources{
		Blockchains: []types.ConfigBlockchain{
			{Name: "ethereum", ChainId: 1, RpcUrl: "http://rpc", BlockRange: 100},
		},
		Abis: []types.ConfigAbi{
			{ContractName: "ERC20", Content: "[]"},
			{ContractName: "Factory", Content: "[]"},
		},
		Stores: []types.ConfigStore{
			{Identifier: "main", StoreType: "clickhouse", StoreConfig: []byte(`{"addr":"ch:9000"}`)},
		},
		Pipelines: []types.ConfigPipeline{
			{Name: "erc20-pipe", Blockchain: "ethereum", Store: "main"},
		},
		Sources: []types.ConfigSource{
			{Pipeline: "erc20-pipe", Abi: "ERC20", Type: "CONTRACT", Enabled: true, StartBlock: 100, Address: "0xToken"},
			{
				Pipeline: "erc20-pipe", Abi: "Factory", Type: "FACTORY", Enabled: true, Address: "0xFactory",
				FactoryChildAbi: "ERC20", FactoryCreationFunctionName: "PoolCreated", FactoryCreationAddressLogArg: "pool",
			},
		},
	}
}

func TestLoadCreatesResourcesAndResolvesRefs(t *testing.T) {
	db := newDBForTest(t)
	// Pipelines are per-instance; create the instance row.
	instance := evmi_database.EvmiInstance{InstanceId: "inst"}
	if err := db.Conn.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}

	Load(db, instance.ID, fullConfig(), zerolog.Nop())

	// Blockchain / ABI / store created.
	var chain evmi_database.EvmBlockchain
	if err := db.Conn.Where("name = ?", "ethereum").First(&chain).Error; err != nil {
		t.Fatalf("blockchain not created: %v", err)
	}
	var store evmi_database.EvmLogStore
	if err := db.Conn.Where("identifier = ?", "main").First(&store).Error; err != nil {
		t.Fatalf("store not created: %v", err)
	}

	// Pipeline created with references resolved to ids.
	var pipeline evmi_database.EvmLogPipeline
	if err := db.Conn.Where("name = ?", "erc20-pipe").First(&pipeline).Error; err != nil {
		t.Fatalf("pipeline not created: %v", err)
	}
	if pipeline.EvmBlockchainID != chain.ID || pipeline.EvmLogStoreId != store.ID || pipeline.EvmiInstanceID != instance.ID {
		t.Errorf("pipeline refs not resolved: %+v", pipeline)
	}

	// Sources created; blockchain defaulted from the pipeline; factory child ABI resolved.
	var sources []evmi_database.EvmLogSource
	if err := db.Conn.Order("type").Find(&sources).Error; err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	var factory, contract evmi_database.EvmLogSource
	for _, s := range sources {
		if s.Type == string(evmi_database.FactoryLogSourceType) {
			factory = s
		} else {
			contract = s
		}
	}
	if contract.EvmBlockchainID != chain.ID {
		t.Errorf("contract source blockchain not defaulted from pipeline: %+v", contract)
	}
	if contract.Address.String != "0xToken" || !contract.Enabled || contract.SyncBlock != 100 {
		t.Errorf("contract source fields wrong: %+v", contract)
	}
	var erc20 evmi_database.EvmJsonAbi
	db.Conn.Where("contract_name = ?", "ERC20").First(&erc20)
	if !factory.FactoryChildEvmJsonABI.Valid || uint(factory.FactoryChildEvmJsonABI.Int32) != erc20.ID {
		t.Errorf("factory child ABI not resolved: %+v", factory)
	}
	if factory.FactoryCreationFunctionName.String != "PoolCreated" {
		t.Errorf("factory creation event not set: %+v", factory)
	}
}

func TestLoadIsIdempotent(t *testing.T) {
	db := newDBForTest(t)
	instance := evmi_database.EvmiInstance{InstanceId: "inst"}
	db.Conn.Create(&instance)

	Load(db, instance.ID, fullConfig(), zerolog.Nop())
	Load(db, instance.ID, fullConfig(), zerolog.Nop())

	for _, tc := range []struct {
		name  string
		model interface{}
		want  int64
	}{
		{"blockchains", &evmi_database.EvmBlockchain{}, 1},
		{"abis", &evmi_database.EvmJsonAbi{}, 2},
		{"stores", &evmi_database.EvmLogStore{}, 1},
		{"pipelines", &evmi_database.EvmLogPipeline{}, 1},
		{"sources", &evmi_database.EvmLogSource{}, 2},
	} {
		var count int64
		db.Conn.Model(tc.model).Count(&count)
		if count != tc.want {
			t.Errorf("%s: expected %d after two loads, got %d", tc.name, tc.want, count)
		}
	}
}

func TestLoadExporterResolvesPlugin(t *testing.T) {
	db := newDBForTest(t)
	instance := evmi_database.EvmiInstance{InstanceId: "inst"}
	db.Conn.Create(&instance)

	// The plugin must already exist (imported before autoload in main).
	plugin := evmi_database.Plugin{Name: "erc20-balances", Status: string(evmi_database.InstalledPluginStatus)}
	db.Conn.Create(&plugin)

	res := fullConfig()
	res.Exporters = []types.ConfigExporter{
		{Name: "balances", Pipeline: "erc20-pipe", Plugin: "erc20-balances", Enabled: true, StartBlock: 5, PluginConfig: []byte(`{"decimals":18}`)},
	}
	Load(db, instance.ID, res, zerolog.Nop())

	var exp evmi_database.EvmiExporter
	if err := db.Conn.Where("name = ?", "balances").First(&exp).Error; err != nil {
		t.Fatalf("exporter not created: %v", err)
	}
	if exp.PluginID != plugin.ID || exp.SyncBlock != 5 || exp.SyncLogIndex != -1 {
		t.Errorf("exporter fields wrong: %+v", exp)
	}
}

func TestLoadMissingReferenceIsSkipped(t *testing.T) {
	db := newDBForTest(t)
	instance := evmi_database.EvmiInstance{InstanceId: "inst"}
	db.Conn.Create(&instance)

	// Pipeline references a store that doesn't exist → pipeline is skipped, and
	// the whole load must not panic.
	res := types.AutoloadResources{
		Blockchains: []types.ConfigBlockchain{{Name: "ethereum", ChainId: 1}},
		Pipelines:   []types.ConfigPipeline{{Name: "broken", Blockchain: "ethereum", Store: "nope"}},
	}
	Load(db, instance.ID, res, zerolog.Nop())

	var count int64
	db.Conn.Model(&evmi_database.EvmLogPipeline{}).Count(&count)
	if count != 0 {
		t.Errorf("pipeline with missing store ref should not be created, got %d", count)
	}
}
