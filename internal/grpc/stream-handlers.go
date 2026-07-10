package grpc

import (
	"context"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/google/uuid"
	"github.com/mustafaturan/bus/v3"
)

// StreamEvmLogSourceUpdates streams live source updates (indexing progress, status
// changes) to the client. It subscribes to the source.update bus topic for the
// lifetime of the stream and forwards each event, optionally filtered by pipeline.
func (e *EvmIndexerServer) StreamEvmLogSourceUpdates(
	ctx context.Context,
	req *connect.Request[evm_indexerv1.StreamEvmLogSourceUpdatesRequest],
	stream *connect.ServerStream[evm_indexerv1.EvmLogSource],
) error {
	// Buffered so a slow client can't block the indexer emitting on the bus.
	updates := make(chan *evm_indexerv1.EvmLogSource, 128)
	pipelineFilter := req.Msg.PipelineId

	key := uuid.NewString()
	e.bus.RegisterHandler(key, bus.Handler{
		Matcher: internal_bus.SourceUpdateTopic,
		Handle: func(_ context.Context, event bus.Event) {
			source, ok := event.Data.(evmi_database.EvmLogSource)
			if !ok {
				return
			}
			if pipelineFilter != 0 && uint32(source.EvmLogPipelineID) != pipelineFilter {
				return
			}
			select {
			case updates <- toGrpcLogSource(source):
			default:
				// Drop when the client can't keep up; the next update supersedes it.
			}
		},
	})
	defer e.bus.DeregisterHandler(key)

	for {
		select {
		case <-ctx.Done():
			return nil
		case source := <-updates:
			if err := stream.Send(source); err != nil {
				return err
			}
		}
	}
}

// StreamEvmiExporterUpdates streams live exporter updates (sync progress, status
// changes) to the client, mirroring StreamEvmLogSourceUpdates.
func (e *EvmIndexerServer) StreamEvmiExporterUpdates(
	ctx context.Context,
	req *connect.Request[evm_indexerv1.StreamEvmiExporterUpdatesRequest],
	stream *connect.ServerStream[evm_indexerv1.EvmiExporter],
) error {
	updates := make(chan *evm_indexerv1.EvmiExporter, 128)
	pipelineFilter := req.Msg.PipelineId

	key := uuid.NewString()
	e.bus.RegisterHandler(key, bus.Handler{
		Matcher: internal_bus.ExporterUpdateTopic,
		Handle: func(_ context.Context, event bus.Event) {
			exporter, ok := event.Data.(evmi_database.EvmiExporter)
			if !ok {
				return
			}
			if pipelineFilter != 0 && uint32(exporter.EvmLogPipelineID) != pipelineFilter {
				return
			}
			select {
			case updates <- toGrpcExporter(exporter):
			default:
				// Drop when the client can't keep up; the next update supersedes it.
			}
		},
	})
	defer e.bus.DeregisterHandler(key)

	for {
		select {
		case <-ctx.Done():
			return nil
		case exporter := <-updates:
			if err := stream.Send(exporter); err != nil {
				return err
			}
		}
	}
}
