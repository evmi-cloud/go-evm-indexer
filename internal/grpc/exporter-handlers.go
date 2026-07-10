package grpc

import (
	"context"
	"time"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"gorm.io/datatypes"
)

// CreateEvmiExporter implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmiExporter(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmiExporterRequest]) (*connect.Response[evm_indexerv1.CreateEvmiExporterResponse], error) {
	config := req.Msg.Exporter.PluginConfigJson
	if config == "" {
		config = "{}"
	}

	newExporter := evmi_database.EvmiExporter{
		Name:             req.Msg.Exporter.Name,
		EvmLogPipelineID: uint(req.Msg.Exporter.EvmLogPipelineId),
		Enabled:          req.Msg.Exporter.Enabled,
		Status:           string(evmi_database.StoppedExporterStatus),
		StartBlock:       req.Msg.Exporter.StartBlock,
		SyncLogIndex:     -1, // fresh cursor: nothing processed yet
		PluginID:         uint(req.Msg.Exporter.PluginId),
		PluginConfig:     datatypes.JSON([]byte(config)),
	}

	result := e.db.Conn.Create(&newExporter)
	if result.Error != nil {
		return nil, result.Error
	}

	return connect.NewResponse(&evm_indexerv1.CreateEvmiExporterResponse{
		Id: uint32(newExporter.ID),
	}), nil
}

// GetEvmiExporter implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmiExporter(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmiExporterRequest]) (*connect.Response[evm_indexerv1.GetEvmiExporterResponse], error) {
	var exporter evmi_database.EvmiExporter
	if result := e.db.Conn.First(&exporter, req.Msg.Id); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.GetEvmiExporterResponse{
		Exporter: toGrpcExporter(exporter),
	}), nil
}

// UpdateEvmiExporter implements evm_indexerv1connect.EvmIndexerServiceHandler.
// The sync cursor (sync_block, sync_log_index) and status are server-managed and
// deliberately not overwritten from the request.
func (e *EvmIndexerServer) UpdateEvmiExporter(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmiExporterRequest]) (*connect.Response[evm_indexerv1.UpdateEvmiExporterResponse], error) {
	var exporter evmi_database.EvmiExporter
	if result := e.db.Conn.First(&exporter, req.Msg.Exporter.Id); result.Error != nil {
		return nil, result.Error
	}

	config := req.Msg.Exporter.PluginConfigJson
	if config == "" {
		config = "{}"
	}

	exporter.Name = req.Msg.Exporter.Name
	exporter.EvmLogPipelineID = uint(req.Msg.Exporter.EvmLogPipelineId)
	exporter.Enabled = req.Msg.Exporter.Enabled
	exporter.StartBlock = req.Msg.Exporter.StartBlock
	exporter.PluginID = uint(req.Msg.Exporter.PluginId)
	exporter.PluginConfig = datatypes.JSON([]byte(config))

	if result := e.db.Conn.Save(&exporter); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.UpdateEvmiExporterResponse{}), nil
}

// ListEvmiExporters implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmiExporters(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmiExportersRequest]) (*connect.Response[evm_indexerv1.ListEvmiExportersResponse], error) {
	var exporters []evmi_database.EvmiExporter

	query := e.db.Conn.Model(&evmi_database.EvmiExporter{})
	if req.Msg.PipelineId > 0 {
		query = query.Where("evm_log_pipeline_id = ?", req.Msg.PipelineId)
	}
	if req.Msg.Pagination != nil && req.Msg.Pagination.Limit > 0 {
		query = query.Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	}
	if result := query.Find(&exporters); result.Error != nil {
		return nil, result.Error
	}

	return connect.NewResponse(&evm_indexerv1.ListEvmiExportersResponse{
		Exporters: toGrpcExporters(exporters),
	}), nil
}

// DeleteEvmiExporter implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmiExporter(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmiExporterRequest]) (*connect.Response[evm_indexerv1.DeleteEvmiExporterResponse], error) {
	if result := e.db.Conn.Delete(&evmi_database.EvmiExporter{}, req.Msg.Id); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.DeleteEvmiExporterResponse{}), nil
}

// StartExporter enables an exporter and waits briefly for it to report running.
func (e *EvmIndexerServer) StartExporter(ctx context.Context, req *connect.Request[evm_indexerv1.StartExporterRequest]) (*connect.Response[evm_indexerv1.StartExporterResponse], error) {
	exporterId := uint(req.Msg.Id)
	var exporter evmi_database.EvmiExporter
	if result := e.db.Conn.First(&exporter, exporterId); result.Error != nil {
		return connect.NewResponse(&evm_indexerv1.StartExporterResponse{Success: false, Error: result.Error.Error()}), nil
	}

	e.bus.Emit(context.Background(), internal_bus.EnableExporterTopic, exporterId)

	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		if result := e.db.Conn.First(&exporter, exporterId); result.Error != nil {
			return connect.NewResponse(&evm_indexerv1.StartExporterResponse{Success: false, Error: result.Error.Error()}), nil
		}
		if exporter.Status == string(evmi_database.RunningExporterStatus) {
			return connect.NewResponse(&evm_indexerv1.StartExporterResponse{Success: true}), nil
		}
	}
	return connect.NewResponse(&evm_indexerv1.StartExporterResponse{}), nil
}

// StopExporter disables an exporter and waits briefly for it to report stopped.
func (e *EvmIndexerServer) StopExporter(ctx context.Context, req *connect.Request[evm_indexerv1.StopExporterRequest]) (*connect.Response[evm_indexerv1.StopExporterResponse], error) {
	exporterId := uint(req.Msg.Id)
	var exporter evmi_database.EvmiExporter
	if result := e.db.Conn.First(&exporter, exporterId); result.Error != nil {
		return connect.NewResponse(&evm_indexerv1.StopExporterResponse{Success: false, Error: result.Error.Error()}), nil
	}

	e.bus.Emit(context.Background(), internal_bus.DisableExporterTopic, exporterId)

	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		if result := e.db.Conn.First(&exporter, exporterId); result.Error != nil {
			return connect.NewResponse(&evm_indexerv1.StopExporterResponse{Success: false, Error: result.Error.Error()}), nil
		}
		if exporter.Status == string(evmi_database.StoppedExporterStatus) {
			return connect.NewResponse(&evm_indexerv1.StopExporterResponse{Success: true}), nil
		}
	}
	return connect.NewResponse(&evm_indexerv1.StopExporterResponse{}), nil
}

func toGrpcExporter(exp evmi_database.EvmiExporter) *evm_indexerv1.EvmiExporter {
	id := uint32(exp.ID)
	createdAt := uint32(exp.CreatedAt.Unix())
	updatedAt := uint32(exp.UpdatedAt.Unix())
	deletedAt := uint32(exp.DeletedAt.Time.Unix())

	return &evm_indexerv1.EvmiExporter{
		Id:               &id,
		Name:             exp.Name,
		EvmLogPipelineId: uint32(exp.EvmLogPipelineID),
		Enabled:          exp.Enabled,
		Status:           exp.Status,
		StartBlock:       exp.StartBlock,
		SyncBlock:        exp.SyncBlock,
		SyncLogIndex:     exp.SyncLogIndex,
		PluginConfigJson: string(exp.PluginConfig),
		PluginId:         uint32(exp.PluginID),
		CreatedAt:        &createdAt,
		UpdatedAt:        &updatedAt,
		DeletedAt:        &deletedAt,
	}
}

func toGrpcExporters(exporters []evmi_database.EvmiExporter) []*evm_indexerv1.EvmiExporter {
	var result []*evm_indexerv1.EvmiExporter
	for _, exp := range exporters {
		result = append(result, toGrpcExporter(exp))
	}
	return result
}
