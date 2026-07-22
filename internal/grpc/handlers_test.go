package grpc

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newServerForTest(t *testing.T) *EvmIndexerServer {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.EvmBlockchain{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &EvmIndexerServer{db: &evmi_database.EvmiDatabase{Conn: db}, logger: zerolog.Nop()}
}

func TestListEvmBlockchainsPagination(t *testing.T) {
	e := newServerForTest(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := e.db.Conn.Create(&evmi_database.EvmBlockchain{Name: fmt.Sprintf("chain-%d", i), ChainId: uint64(i)}).Error; err != nil {
			t.Fatal(err)
		}
	}

	// No pagination message: must not panic and returns everything.
	res, err := e.ListEvmBlockchains(ctx, connect.NewRequest(&evm_indexerv1.ListEvmBlockchainsRequest{}))
	if err != nil {
		t.Fatalf("list without pagination: %v", err)
	}
	if len(res.Msg.Blockchains) != 5 {
		t.Fatalf("got %d blockchains, want 5", len(res.Msg.Blockchains))
	}

	// Offset/limit are actually applied.
	res, err = e.ListEvmBlockchains(ctx, connect.NewRequest(&evm_indexerv1.ListEvmBlockchainsRequest{
		Pagination: &evm_indexerv1.Pagination{Offset: 1, Limit: 2},
	}))
	if err != nil {
		t.Fatalf("list with pagination: %v", err)
	}
	if len(res.Msg.Blockchains) != 2 {
		t.Fatalf("got %d blockchains, want 2 (offset 1, limit 2)", len(res.Msg.Blockchains))
	}
	if res.Msg.Blockchains[0].Name != "chain-1" {
		t.Errorf("first page item = %s, want chain-1", res.Msg.Blockchains[0].Name)
	}
}

func TestGetEvmBlockchainNotFound(t *testing.T) {
	e := newServerForTest(t)

	_, err := e.GetEvmBlockchain(context.Background(), connect.NewRequest(&evm_indexerv1.GetEvmBlockchainRequest{Id: 999}))
	if err == nil {
		t.Fatal("expected an error for a missing id")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}
