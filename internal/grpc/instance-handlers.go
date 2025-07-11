package grpc

import (
	"context"

	"connectrpc.com/connect"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// GetEvmiInstance implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmiInstance(context.Context, *connect.Request[evm_indexerv1.GetEvmiInstanceRequest]) (*connect.Response[evm_indexerv1.GetEvmiInstanceResponse], error) {
	panic("unimplemented")
}

// ListEvmiInstances implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmiInstances(context.Context, *connect.Request[evm_indexerv1.ListEvmiInstancesRequest]) (*connect.Response[evm_indexerv1.ListEvmiInstancesResponse], error) {
	panic("unimplemented")
}
