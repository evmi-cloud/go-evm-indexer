package grpc

import (
	"context"

	"connectrpc.com/connect"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmJsonAbi(context.Context, *connect.Request[evm_indexerv1.CreateEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.CreateEvmJsonAbiResponse], error) {
	panic("unimplemented")
}

// DeleteEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmJsonAbi(context.Context, *connect.Request[evm_indexerv1.DeleteEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.DeleteEvmJsonAbiResponse], error) {
	panic("unimplemented")
}

// GetEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmJsonAbi(context.Context, *connect.Request[evm_indexerv1.GetEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.GetEvmJsonAbiResponse], error) {
	panic("unimplemented")
}

// ListEvmJsonAbis implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmJsonAbis(context.Context, *connect.Request[evm_indexerv1.ListEvmJsonAbisRequest]) (*connect.Response[evm_indexerv1.ListEvmJsonAbisResponse], error) {
	panic("unimplemented")
}

// UpdateEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmJsonAbi(context.Context, *connect.Request[evm_indexerv1.UpdateEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.UpdateEvmJsonAbiResponse], error) {
	panic("unimplemented")
}
