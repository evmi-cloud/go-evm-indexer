package grpc

import (
	"context"
	"encoding/json"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
)

// ListEvmLogs implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogs(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmLogsRequest]) (*connect.Response[evm_indexerv1.ListEvmLogsResponse], error) {

	var pipeline evm_indexerv1.EvmLogPipeline
	result := e.db.Conn.First(&pipeline, req.Msg.PipelineId)
	if result.Error != nil {
		return nil, result.Error
	}

	var storeInfo evmi_database.EvmLogStore
	result = e.db.Conn.First(&storeInfo, pipeline.EvmLogStoreId)
	if result.Error != nil {
		return nil, result.Error
	}

	var storeConfig map[string]string
	err := json.Unmarshal(storeInfo.StoreConfig, &storeConfig)
	if err != nil {
		return nil, result.Error
	}

	store, err := log_stores.LoadStore(storeInfo.StoreType, storeConfig, e.logger)
	if err != nil {
		return nil, result.Error
	}

	logs, err := store.GetStorage().GetLogs(req.Msg.FromTimestamp, req.Msg.ToTimestamp, uint64(req.Msg.Pagination.Limit), uint64(req.Msg.Pagination.Offset))
	if err != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmLogsResponse]{
		Msg: &evm_indexerv1.ListEvmLogsResponse{
			Logs: toGrpcLogs(logs),
		},
	}, nil
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
