package grpc

import (
	"context"

	"connectrpc.com/connect"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogPipeline(context.Context, *connect.Request[evm_indexerv1.CreateEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogPipelineResponse], error) {
	panic("unimplemented")
}

// DeleteEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogPipeline(context.Context, *connect.Request[evm_indexerv1.DeleteEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogPipelineResponse], error) {
	panic("unimplemented")
}

// GetEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogPipeline(context.Context, *connect.Request[evm_indexerv1.GetEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.GetEvmLogPipelineResponse], error) {
	panic("unimplemented")
}

// ListEvmLogPipelines implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogPipelines(context.Context, *connect.Request[evm_indexerv1.ListEvmLogPipelinesRequest]) (*connect.Response[evm_indexerv1.ListEvmLogPipelinesResponse], error) {
	panic("unimplemented")
}

// StartPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) StartPipeline(context.Context, *connect.Request[evm_indexerv1.StartPipelineRequest]) (*connect.Response[evm_indexerv1.StartPipelineResponse], error) {
	panic("unimplemented")
}

// StopPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) StopPipeline(context.Context, *connect.Request[evm_indexerv1.StopPipelineRequest]) (*connect.Response[evm_indexerv1.StopPipelineResponse], error) {
	panic("unimplemented")
}

// UpdateEvmLogPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogPipeline(context.Context, *connect.Request[evm_indexerv1.UpdateEvmLogPipelineRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogPipelineResponse], error) {
	panic("unimplemented")
}
