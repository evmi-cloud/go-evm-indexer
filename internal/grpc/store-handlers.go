package grpc

import (
	"context"

	"connectrpc.com/connect"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogStore(context.Context, *connect.Request[evm_indexerv1.CreateEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogStoreResponse], error) {
	panic("unimplemented")
}

// DeleteEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogStore(context.Context, *connect.Request[evm_indexerv1.DeleteEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogStoreResponse], error) {
	panic("unimplemented")
}

// GetEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogStore(context.Context, *connect.Request[evm_indexerv1.GetEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.GetEvmLogStoreResponse], error) {
	panic("unimplemented")
}

// ListEvmLogStores implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogStores(context.Context, *connect.Request[evm_indexerv1.ListEvmLogStoresRequest]) (*connect.Response[evm_indexerv1.ListEvmLogStoresResponse], error) {
	panic("unimplemented")
}

// UpdateEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogStore(context.Context, *connect.Request[evm_indexerv1.UpdateEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogStoreResponse], error) {
	panic("unimplemented")
}
