package grpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
	"github.com/rs/zerolog"
)

// End-to-end: a source.update emitted on the bus reaches a streaming client, and
// the pipeline filter is honored.
func TestStreamEvmLogSourceUpdates(t *testing.T) {
	b := internal_bus.InitializeBus()
	server := &EvmIndexerServer{bus: b, logger: zerolog.Nop()}

	mux := http.NewServeMux()
	path, handler := evm_indexerv1connect.NewEvmIndexerServiceHandler(server)
	mux.Handle(path, handler)
	ts := httptest.NewUnstartedServer(mux)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	client := evm_indexerv1connect.NewEvmIndexerServiceClient(ts.Client(), ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Emit continuously (started before the stream opens) to avoid racing the
	// server-side handler registration: one update for the wrong pipeline (must be
	// filtered out) and one for pipeline 2.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				b.Emit(context.Background(), internal_bus.SourceUpdateTopic,
					evmi_database.EvmLogSource{EvmLogPipelineID: 1, SyncBlock: 99})
				b.Emit(context.Background(), internal_bus.SourceUpdateTopic,
					evmi_database.EvmLogSource{EvmLogPipelineID: 2, SyncBlock: 7, Type: "TOPIC"})
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	defer close(done)

	// Only stream updates for pipeline 2.
	stream, err := client.StreamEvmLogSourceUpdates(ctx, connect.NewRequest(&evm_indexerv1.StreamEvmLogSourceUpdatesRequest{PipelineId: 2}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	if !stream.Receive() {
		t.Fatalf("stream closed without a message: %v", stream.Err())
	}
	got := stream.Msg()
	cancel() // end the server-side handler promptly so teardown is fast

	if got.EvmLogPipelineId != 2 || got.SyncBlock != 7 {
		t.Errorf("got pipeline=%d syncBlock=%d, want pipeline=2 syncBlock=7 (filter/delivery wrong)", got.EvmLogPipelineId, got.SyncBlock)
	}
}

// End-to-end: an exporter.update emitted on the bus reaches a streaming client,
// honoring the pipeline filter.
func TestStreamEvmiExporterUpdates(t *testing.T) {
	b := internal_bus.InitializeBus()
	server := &EvmIndexerServer{bus: b, logger: zerolog.Nop()}

	mux := http.NewServeMux()
	path, handler := evm_indexerv1connect.NewEvmIndexerServiceHandler(server)
	mux.Handle(path, handler)
	ts := httptest.NewUnstartedServer(mux)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	client := evm_indexerv1connect.NewEvmIndexerServiceClient(ts.Client(), ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				b.Emit(context.Background(), internal_bus.ExporterUpdateTopic,
					evmi_database.EvmiExporter{EvmLogPipelineID: 1, SyncBlock: 99})
				b.Emit(context.Background(), internal_bus.ExporterUpdateTopic,
					evmi_database.EvmiExporter{EvmLogPipelineID: 2, SyncBlock: 5, Name: "balances"})
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	defer close(done)

	stream, err := client.StreamEvmiExporterUpdates(ctx, connect.NewRequest(&evm_indexerv1.StreamEvmiExporterUpdatesRequest{PipelineId: 2}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	if !stream.Receive() {
		t.Fatalf("stream closed without a message: %v", stream.Err())
	}
	got := stream.Msg()
	cancel()

	if got.EvmLogPipelineId != 2 || got.SyncBlock != 5 || got.Name != "balances" {
		t.Errorf("got pipeline=%d syncBlock=%d name=%q, want pipeline=2 syncBlock=5 name=balances", got.EvmLogPipelineId, got.SyncBlock, got.Name)
	}
}
