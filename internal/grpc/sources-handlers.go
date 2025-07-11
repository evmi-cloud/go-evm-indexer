package grpc

import (
	"context"

	"connectrpc.com/connect"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogSource(context.Context, *connect.Request[evm_indexerv1.CreateEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogSourceResponse], error) {
	panic("unimplemented")
}

// DeleteEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogSource(context.Context, *connect.Request[evm_indexerv1.DeleteEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogSourceResponse], error) {
	panic("unimplemented")
}

// GetEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogSource(context.Context, *connect.Request[evm_indexerv1.GetEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.GetEvmLogSourceResponse], error) {
	panic("unimplemented")
}

// ListEvmLogSources implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogSources(context.Context, *connect.Request[evm_indexerv1.ListEvmLogSourcesRequest]) (*connect.Response[evm_indexerv1.ListEvmLogSourcesResponse], error) {
	panic("unimplemented")
}

// UpdateEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogSource(context.Context, *connect.Request[evm_indexerv1.UpdateEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogSourceResponse], error) {
	panic("unimplemented")
}
