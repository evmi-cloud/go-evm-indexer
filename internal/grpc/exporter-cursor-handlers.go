package grpc

import (
	"context"
	"sort"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/google/uuid"
	"github.com/mustafaturan/bus/v3"
)

// ListEvmiExporterSourceCursors returns one row per source of the exporter's
// pipeline: how far the exporter has exported it, and how far the indexer has
// stored it. This is the real progress picture — EvmiExporter.SyncBlock is only
// the minimum across these.
//
// It lists the pipeline's sources rather than the cursor rows, so a source the
// exporter has not reached yet (no row created because the exporter never ran, or
// it was attached while stopped) still shows up, at its own StartBlock.
func (e *EvmIndexerServer) ListEvmiExporterSourceCursors(
	ctx context.Context,
	req *connect.Request[evm_indexerv1.ListEvmiExporterSourceCursorsRequest],
) (*connect.Response[evm_indexerv1.ListEvmiExporterSourceCursorsResponse], error) {
	var exporter evmi_database.EvmiExporter
	if err := e.db.Conn.First(&exporter, req.Msg.ExporterId).Error; err != nil {
		return nil, dbError(err)
	}

	var sources []evmi_database.EvmLogSource
	if err := e.db.Conn.
		Where("evm_log_pipeline_id = ?", exporter.EvmLogPipelineID).
		Find(&sources).Error; err != nil {
		return nil, dbError(err)
	}

	var rows []evmi_database.EvmiExporterSourceCursor
	if err := e.db.Conn.
		Where("evmi_exporter_id = ?", exporter.ID).
		Find(&rows).Error; err != nil {
		return nil, dbError(err)
	}
	bySource := make(map[uint]evmi_database.EvmiExporterSourceCursor, len(rows))
	for _, r := range rows {
		bySource[r.EvmLogSourceID] = r
	}

	cursors := make([]*evm_indexerv1.EvmiExporterSourceCursor, 0, len(sources))
	for _, s := range sources {
		update := types.ExporterCursorUpdate{
			ExporterID:      exporter.ID,
			SourceID:        s.ID,
			SourceSyncBlock: s.SyncBlock,
			SourceType:      s.Type,
			SourceAddress:   s.Address.String,
			SourceTopic0:    s.Topic0.String,
			SourceEnabled:   s.Enabled,
			SourceStatus:    s.Status,
			ParentSourceID:  s.ParentSourceID,
		}
		if row, ok := bySource[s.ID]; ok {
			update.SyncBlock = row.SyncBlock
			update.SyncLogIndex = row.SyncLogIndex
		} else {
			// Not tracked yet: the exporter would seed it here on its next pass.
			update.SyncBlock = s.StartBlock
			update.SyncLogIndex = -1
		}
		cursors = append(cursors, toGrpcExporterCursor(update))
	}

	// Stable order so the detail table doesn't shuffle between refreshes.
	sort.Slice(cursors, func(i, j int) bool { return cursors[i].SourceId < cursors[j].SourceId })

	return connect.NewResponse(&evm_indexerv1.ListEvmiExporterSourceCursorsResponse{
		Cursors: cursors,
	}), nil
}

// StreamEvmiExporterSourceCursors streams the exporter's per-source cursors as it
// advances them, mirroring the other server streams: it subscribes to the
// exporter.cursor bus topic for the lifetime of the stream and forwards each
// event for the requested exporter.
func (e *EvmIndexerServer) StreamEvmiExporterSourceCursors(
	ctx context.Context,
	req *connect.Request[evm_indexerv1.StreamEvmiExporterSourceCursorsRequest],
	stream *connect.ServerStream[evm_indexerv1.EvmiExporterSourceCursor],
) error {
	// Buffered so a slow client can't block the export loop emitting on the bus.
	// Sized for a pipeline with many factory-spawned sources, which emit one event
	// each per batch.
	updates := make(chan *evm_indexerv1.EvmiExporterSourceCursor, 512)
	exporterFilter := req.Msg.ExporterId

	key := uuid.NewString()
	e.bus.RegisterHandler(key, bus.Handler{
		Matcher: internal_bus.ExporterCursorTopic,
		Handle: func(_ context.Context, event bus.Event) {
			update, ok := event.Data.(types.ExporterCursorUpdate)
			if !ok {
				return
			}
			if exporterFilter != 0 && uint32(update.ExporterID) != exporterFilter {
				return
			}
			select {
			case updates <- toGrpcExporterCursor(update):
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
		case cursor := <-updates:
			if err := stream.Send(cursor); err != nil {
				return err
			}
		}
	}
}

func toGrpcExporterCursor(u types.ExporterCursorUpdate) *evm_indexerv1.EvmiExporterSourceCursor {
	return &evm_indexerv1.EvmiExporterSourceCursor{
		ExporterId:      uint32(u.ExporterID),
		SourceId:        uint32(u.SourceID),
		SyncBlock:       u.SyncBlock,
		SyncLogIndex:    u.SyncLogIndex,
		SourceSyncBlock: u.SourceSyncBlock,
		LagBlocks:       u.LagBlocks(),
		SourceType:      u.SourceType,
		SourceAddress:   u.SourceAddress,
		SourceTopic0:    u.SourceTopic0,
		SourceEnabled:   u.SourceEnabled,
		SourceStatus:    u.SourceStatus,
		ParentSourceId:  uint32(u.ParentSourceID),
	}
}
