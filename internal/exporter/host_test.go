package exporter

import (
	"database/sql"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// A minimal but real ABI, so UpsertAbi's validation is genuinely exercised.
const testAbi = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"instance","type":"address"}],"name":"Deployed","type":"event"}]`

func newHostDB(t *testing.T) *evmi_database.EvmiDatabase {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "h.db")), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&evmi_database.EvmLogSource{},
		&evmi_database.EvmJsonAbi{},
		&evmi_database.EvmLogPipeline{},
		&evmi_database.EvmBlockchain{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &evmi_database.EvmiDatabase{Conn: db}
}

// newHost builds the host API bound to pipeline 1, plus a parent source in that
// pipeline for children to hang off.
func newHost(t *testing.T) (*exporterHost, evmi_database.EvmLogSource) {
	t.Helper()
	db := newHostDB(t)

	parent := evmi_database.EvmLogSource{
		Enabled:          true,
		Type:             string(evmi_database.FactoryLogSourceType),
		Address:          sql.NullString{String: "0xfactory", Valid: true},
		EvmLogPipelineID: 1,
		EvmBlockchainID:  7,
	}
	if err := db.Conn.Create(&parent).Error; err != nil {
		t.Fatalf("seed parent: %v", err)
	}

	return &exporterHost{
		db:         db,
		pipelineID: 1,
		chain: evmi_database.EvmBlockchain{
			ChainId: 1, Name: "mainnet", RpcUrl: "https://rpc.example/abc", BlockRange: 500,
		},
		exporterName: "test",
		logger:       zerolog.Nop(),
	}, parent
}

func TestHostBlockchainExposesRpcEndpoint(t *testing.T) {
	h, _ := newHost(t)

	chain, err := h.Blockchain()
	if err != nil {
		t.Fatalf("Blockchain: %v", err)
	}
	if chain.RpcUrl != "https://rpc.example/abc" || chain.ChainId != 1 || chain.BlockRange != 500 {
		t.Fatalf("unexpected chain: %+v", chain)
	}
}

func TestHostUpsertAbiIsIdempotentAndValidates(t *testing.T) {
	h, _ := newHost(t)

	first, err := h.UpsertAbi("Pair", testAbi)
	if err != nil {
		t.Fatalf("UpsertAbi: %v", err)
	}
	if !first.Created || first.Id == 0 {
		t.Fatalf("first upsert should create, got %+v", first)
	}

	// Same name again → existing id, no duplicate row, content untouched.
	second, err := h.UpsertAbi("Pair", `[{"type":"event","name":"Other","inputs":[]}]`)
	if err != nil {
		t.Fatalf("second UpsertAbi: %v", err)
	}
	if second.Created || second.Id != first.Id {
		t.Fatalf("second upsert should reuse %d, got %+v", first.Id, second)
	}
	var count int64
	h.db.Conn.Model(&evmi_database.EvmJsonAbi{}).Where("contract_name = ?", "Pair").Count(&count)
	if count != 1 {
		t.Fatalf("abi rows = %d, want 1", count)
	}
	got, found, err := h.GetAbi("Pair")
	if err != nil || !found {
		t.Fatalf("GetAbi: %v found=%t", err, found)
	}
	if got.Content != testAbi {
		t.Fatal("existing abi content was overwritten")
	}

	// Garbage is rejected rather than stored to fail at decode time later.
	if _, err := h.UpsertAbi("Broken", "not json"); err == nil {
		t.Fatal("expected invalid abi to be rejected")
	}
}

func TestHostAbiLookups(t *testing.T) {
	h, _ := newHost(t)
	ref, err := h.UpsertAbi("Pair", testAbi)
	if err != nil {
		t.Fatalf("UpsertAbi: %v", err)
	}

	byID, found, err := h.GetAbiByID(ref.Id)
	if err != nil || !found || byID.ContractName != "Pair" {
		t.Fatalf("GetAbiByID: %v found=%t abi=%+v", err, found, byID)
	}

	if _, found, _ := h.GetAbi("Missing"); found {
		t.Fatal("missing abi reported as found")
	}

	all, err := h.ListAbis()
	if err != nil || len(all) != 1 {
		t.Fatalf("ListAbis: %v n=%d", err, len(all))
	}
}

func TestHostCreateLogSourceMirrorsFactoryChild(t *testing.T) {
	h, parent := newHost(t)
	abiRef, _ := h.UpsertAbi("Pair", testAbi)

	ref, err := h.CreateLogSource(pluginsdk.NewLogSource{
		Parent:     uint64(parent.ID),
		Address:    "0xAbCdEf0000000000000000000000000000000001",
		AbiId:      abiRef.Id,
		StartBlock: 1200,
	})
	if err != nil {
		t.Fatalf("CreateLogSource: %v", err)
	}
	if !ref.Created {
		t.Fatal("expected a new source")
	}

	var child evmi_database.EvmLogSource
	if err := h.db.Conn.First(&child, uint(ref.Id)).Error; err != nil {
		t.Fatalf("load child: %v", err)
	}
	if child.ParentSourceID != parent.ID {
		t.Errorf("parent = %d, want %d", child.ParentSourceID, parent.ID)
	}
	if !child.Enabled {
		t.Error("child should be created enabled, like a factory child")
	}
	if child.Type != string(evmi_database.ContractLogSourceType) {
		t.Errorf("type = %q, want CONTRACT (the default)", child.Type)
	}
	// Inherited from the parent, not the caller.
	if child.EvmLogPipelineID != parent.EvmLogPipelineID || child.EvmBlockchainID != parent.EvmBlockchainID {
		t.Errorf("child should inherit pipeline/chain, got %d/%d", child.EvmLogPipelineID, child.EvmBlockchainID)
	}
	// Cursor sits just before the deployment block so nothing is missed/replayed.
	if child.StartBlock != 1199 || child.SyncBlock != 1199 {
		t.Errorf("cursor = (%d,%d), want 1199", child.StartBlock, child.SyncBlock)
	}
	if child.Address.String != "0xabcdef0000000000000000000000000000000001" {
		t.Errorf("address not normalised: %q", child.Address.String)
	}
	if child.EvmJsonAbiID != uint(abiRef.Id) {
		t.Errorf("abi = %d, want %d", child.EvmJsonAbiID, abiRef.Id)
	}
}

// At-least-once delivery means the same deployment log can arrive twice; the
// second registration must return the first source, not create a duplicate.
func TestHostCreateLogSourceIsIdempotent(t *testing.T) {
	h, parent := newHost(t)

	first, err := h.CreateLogSource(pluginsdk.NewLogSource{
		Parent: uint64(parent.ID), Address: "0xAAA", StartBlock: 10,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Different casing on purpose — the same contract.
	second, err := h.CreateLogSource(pluginsdk.NewLogSource{
		Parent: uint64(parent.ID), Address: "0xaaa", StartBlock: 10,
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Created || second.Id != first.Id {
		t.Fatalf("re-register should reuse %d, got %+v", first.Id, second)
	}

	var count int64
	h.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("parent_source_id = ?", parent.ID).Count(&count)
	if count != 1 {
		t.Fatalf("child rows = %d, want 1", count)
	}
}

func TestHostCreateLogSourceRejectsBadInput(t *testing.T) {
	h, parent := newHost(t)

	// A parent in another pipeline is the scoping boundary: a plugin must not be
	// able to graft sources onto a pipeline it does not belong to.
	foreign := evmi_database.EvmLogSource{Type: "CONTRACT", EvmLogPipelineID: 99}
	h.db.Conn.Create(&foreign)

	cases := []struct {
		name string
		src  pluginsdk.NewLogSource
	}{
		{"foreign pipeline", pluginsdk.NewLogSource{Parent: uint64(foreign.ID), Address: "0x1"}},
		{"unknown parent", pluginsdk.NewLogSource{Parent: 4242, Address: "0x1"}},
		{"no address", pluginsdk.NewLogSource{Parent: uint64(parent.ID)}},
		{"no parent", pluginsdk.NewLogSource{Address: "0x1"}},
		{"bad type", pluginsdk.NewLogSource{Parent: uint64(parent.ID), Address: "0x1", Type: "TOPIC"}},
		{"unknown abi", pluginsdk.NewLogSource{Parent: uint64(parent.ID), Address: "0x1", AbiId: 999}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.CreateLogSource(tc.src); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// Block 0 must not underflow the cursor to max-uint64, which would park the
// source permanently ahead of the chain head.
func TestHostCreateLogSourceHandlesBlockZero(t *testing.T) {
	h, parent := newHost(t)

	ref, err := h.CreateLogSource(pluginsdk.NewLogSource{
		Parent: uint64(parent.ID), Address: "0xgenesis", StartBlock: 0,
	})
	if err != nil {
		t.Fatalf("CreateLogSource: %v", err)
	}
	var child evmi_database.EvmLogSource
	h.db.Conn.First(&child, uint(ref.Id))
	if child.SyncBlock != 0 {
		t.Fatalf("sync block = %d, want 0 (no underflow)", child.SyncBlock)
	}
}

// --- end to end, through a real plugin subprocess ---------------------------

// The whole feature in one test: a real plugin binary, launched as a subprocess,
// dialling back over the go-plugin broker and driving the real host API against a
// real database. This is what proves the broker wiring works — the unit tests
// above call exporterHost directly and never cross a process boundary.
func TestHostAPIEndToEndThroughPluginSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a plugin binary; skipped with -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	bin := filepath.Join(t.TempDir(), "hostapi"+exeSuffix())
	build := exec.Command("go", "build", "-o", bin, "./examples/exporters/hostapi")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hostapi example: %v: %s", err, out)
	}

	process, err := startPlugin(bin, "hostapi", zerolog.Nop())
	if err != nil {
		t.Fatalf("startPlugin: %v", err)
	}
	defer process.Kill()

	host, parent := newHost(t)
	process.SetHost(host)

	cfg, _ := json.Marshal(map[string]string{
		"contractName": "Pair",
		"abi":          testAbi,
		"createOn":     "Deployed",
		"addressArg":   "instance",
	})

	plug := process.Exporter()
	if err := plug.Init(pluginsdk.Context{
		ExporterName: "e2e", PipelineId: 1, ChainId: 1, Config: cfg,
	}); err != nil {
		t.Fatalf("Init (plugin calls UpsertAbi + Blockchain from here): %v", err)
	}

	// Init should have registered the ABI through the broker.
	abiRow, found, err := host.GetAbi("Pair")
	if err != nil || !found {
		t.Fatalf("plugin did not register the abi: %v found=%t", err, found)
	}

	// A deployment log → the plugin registers a source for the new contract.
	deployed := pluginsdk.LogEvent{
		Id: "1:900:0", SourceId: parent.ID, ChainId: 1, BlockNumber: 900,
		EventName: "Deployed",
		Args:      map[string]string{"instance": "0xDEADBEEF00000000000000000000000000000001"},
	}
	if err := plug.NewLogEvent(deployed); err != nil {
		t.Fatalf("NewLogEvent: %v", err)
	}

	var child evmi_database.EvmLogSource
	if err := host.db.Conn.Where("parent_source_id = ?", parent.ID).First(&child).Error; err != nil {
		t.Fatalf("no source created by the plugin: %v", err)
	}
	if child.Address.String != "0xdeadbeef00000000000000000000000000000001" {
		t.Errorf("address = %q", child.Address.String)
	}
	if child.EvmJsonAbiID != uint(abiRow.Id) {
		t.Errorf("source abi = %d, want the one Init registered (%d)", child.EvmJsonAbiID, abiRow.Id)
	}
	if child.SyncBlock != 899 {
		t.Errorf("cursor = %d, want 899", child.SyncBlock)
	}

	// Redelivery of the same log (at-least-once) must not duplicate the source.
	if err := plug.NewLogEvent(deployed); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	var count int64
	host.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("parent_source_id = ?", parent.ID).Count(&count)
	if count != 1 {
		t.Fatalf("child rows after redelivery = %d, want 1", count)
	}

	if err := plug.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Logf("plugin subprocess registered abi %d and source %d over the broker", abiRow.Id, child.ID)
}
