package grpc

import (
	"context"

	"connectrpc.com/connect"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
)

// ListEvmLogs implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogs(context.Context, *connect.Request[evm_indexerv1.ListEvmLogsRequest]) (*connect.Response[evm_indexerv1.ListEvmLogsResponse], error) {
	panic("unimplemented")
}

// ListEvmTransactions implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmTransactions(context.Context, *connect.Request[evm_indexerv1.ListEvmTransactionsRequest]) (*connect.Response[evm_indexerv1.ListEvmTransactionsResponse], error) {
	panic("unimplemented")
}

func toGrpcLogs(logs []types.EvmLog) []*evm_indexerv1.EvmLog {
	var result []*evm_indexerv1.EvmLog
	for _, log := range logs {
		result = append(result, &evm_indexerv1.EvmLog{
			Address:          log.Address,
			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,
			MintedAt:         uint32(log.MintedAt),

			Metadata: &evm_indexerv1.EvmMetadata{
				ContractName: &log.Metadata.ContractName,
				EventName:    &log.Metadata.EventName,
				FunctionName: &log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		})
	}

	return result
}
