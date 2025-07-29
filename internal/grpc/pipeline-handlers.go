package grpc

import (
	"context"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogPipeline(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogPipelineResponse], error) {
	newPipeline := evmi_database.EvmLogPipeline{
		Name:       req.Msg.Pipeline.Name,
		LogSources: []evmi_database.EvmLogSource{},

		EvmiInstanceID: uint(req.Msg.Pipeline.EvmiInstanceId),
		EvmLogStoreId:  uint(req.Msg.Pipeline.EvmLogStoreId),
	}

	result := e.db.Conn.Create(&newPipeline)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.CreateEvmLogPipelineResponse]{
		Msg: &evm_indexerv1.CreateEvmLogPipelineResponse{
			Id: uint32(newPipeline.ID),
		},
	}, nil
}

// DeleteEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogPipeline(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogPipelineResponse], error) {
	//TODO: verify there is dependent entities

	result := e.db.Conn.Delete(&evmi_database.EvmLogPipeline{}, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.DeleteEvmLogPipelineResponse]{
		Msg: &evm_indexerv1.DeleteEvmLogPipelineResponse{},
	}, nil
}

// GetEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogPipeline(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.GetEvmLogPipelineResponse], error) {
	var pipeline evmi_database.EvmLogPipeline

	result := e.db.Conn.First(&pipeline, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.GetEvmLogPipelineResponse]{
		Msg: &evm_indexerv1.GetEvmLogPipelineResponse{
			Pipeline: toGrpcPipeline(pipeline),
		},
	}, nil
}

// ListEvmLogPipelines implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogPipelines(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmLogPipelinesRequest]) (*connect.Response[evm_indexerv1.ListEvmLogPipelinesResponse], error) {
	var pipelines []evmi_database.EvmLogPipeline

	result := e.db.Conn.Model(&evmi_database.EvmLogPipeline{}).Find(&pipelines).Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmLogPipelinesResponse]{
		Msg: &evm_indexerv1.ListEvmLogPipelinesResponse{
			Pipelines: toGrpcPipelines(pipelines),
		},
	}, nil
}

// UpdateEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogPipeline(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogPipelineResponse], error) {
	var blockchain evmi_database.EvmLogPipeline

	result := e.db.Conn.First(&blockchain, req.Msg.Pipeline.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	blockchain.Name = req.Msg.Pipeline.Name
	blockchain.EvmiInstanceID = uint(req.Msg.Pipeline.EvmiInstanceId)
	blockchain.EvmLogStoreId = uint(req.Msg.Pipeline.EvmLogStoreId)

	result = e.db.Conn.Save(&blockchain)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.UpdateEvmLogPipelineResponse]{
		Msg: &evm_indexerv1.UpdateEvmLogPipelineResponse{},
	}, nil
}

func toGrpcPipeline(pipeline evmi_database.EvmLogPipeline) *evm_indexerv1.EvmLogPipeline {
	id := uint32(pipeline.ID)
	createdAt := uint32(pipeline.CreatedAt.Unix())
	updatedAt := uint32(pipeline.UpdatedAt.Unix())
	deletedAt := uint32(pipeline.DeletedAt.Time.Unix())
	return &evm_indexerv1.EvmLogPipeline{
		Id:             &id,
		Name:           pipeline.Name,
		EvmiInstanceId: uint32(pipeline.EvmiInstanceID),
		EvmLogStoreId:  uint32(pipeline.EvmLogStoreId),
		CreatedAt:      &createdAt,
		UpdatedAt:      &updatedAt,
		DeletedAt:      &deletedAt,
	}
}

func toGrpcPipelines(pipelines []evmi_database.EvmLogPipeline) []*evm_indexerv1.EvmLogPipeline {
	var result []*evm_indexerv1.EvmLogPipeline

	for _, pipeline := range pipelines {
		result = append(result, toGrpcPipeline(pipeline))
	}

	return result
}
