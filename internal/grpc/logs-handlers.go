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

	var source evmi_database.EvmLogSource
	result := e.db.Conn.First(&source, req.Msg.SourceId)
	if result.Error != nil {
		return nil, result.Error
	}

	var pipeline evmi_database.EvmLogPipeline
	result = e.db.Conn.First(&pipeline, source.EvmLogPipelineID)
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

	logs, err := store.GetStorage().GetLogs(uint64(req.Msg.SourceId), req.Msg.FromBlock, req.Msg.ToBlock)
	if err != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmLogsResponse]{
		Msg: &evm_indexerv1.ListEvmLogsResponse{
			Logs: toGrpcLogs(logs),
		},
	}, nil
}

// ListLatestEvmLogs implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListLatestEvmLogs(ctx context.Context, req *connect.Request[evm_indexerv1.ListLatestEvmLogsRequest]) (*connect.Response[evm_indexerv1.ListLatestEvmLogsResponse], error) {
	var source evmi_database.EvmLogSource
	result := e.db.Conn.First(&source, req.Msg.SourceId)
	if result.Error != nil {
		return nil, result.Error
	}

	var pipeline evmi_database.EvmLogPipeline
	result = e.db.Conn.First(&pipeline, source.EvmLogPipelineID)
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

	logs, err := store.GetStorage().GetLatestLogs(uint64(req.Msg.SourceId), req.Msg.Limit)
	if err != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListLatestEvmLogsResponse]{
		Msg: &evm_indexerv1.ListLatestEvmLogsResponse{
			Logs: toGrpcLogs(logs),
		},
	}, nil
}

// ListEvmTransactions implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmTransactions(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmTransactionsRequest]) (*connect.Response[evm_indexerv1.ListEvmTransactionsResponse], error) {
	var source evmi_database.EvmLogSource
	result := e.db.Conn.First(&source, req.Msg.SourceId)
	if result.Error != nil {
		return nil, result.Error
	}

	var pipeline evmi_database.EvmLogPipeline
	result = e.db.Conn.First(&pipeline, source.EvmLogPipelineID)
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

	txs, err := store.GetStorage().GetTransactions(uint64(req.Msg.SourceId), req.Msg.FromBlock, req.Msg.ToBlock)
	if err != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmTransactionsResponse]{
		Msg: &evm_indexerv1.ListEvmTransactionsResponse{
			Transactions: toGrpcTransactions(txs),
		},
	}, nil
}

func toGrpcLogs(logs []types.EvmLog) []*evm_indexerv1.EvmLog {
	var result []*evm_indexerv1.EvmLog
	for _, log := range logs {
		result = append(result, &evm_indexerv1.EvmLog{
			Id:               log.Id,
			SourceId:         uint32(log.SourceId),
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

func toGrpcTransactions(txs []types.EvmTransaction) []*evm_indexerv1.EvmTransaction {
	var result []*evm_indexerv1.EvmTransaction
	for _, tx := range txs {
		result = append(result, &evm_indexerv1.EvmTransaction{
			Id:               tx.Id,
			SourceId:         uint32(tx.SourceId),
			BlockNumber:      tx.BlockNumber,
			TransactionIndex: tx.TransactionIndex,
			ChainId:          tx.ChainId,
			From:             tx.From,
			Data:             tx.Data,
			Value:            tx.Value,
			Nonce:            tx.Nonce,
			To:               tx.To,
			Hash:             tx.Hash,

			Metadata: &evm_indexerv1.EvmMetadata{
				ContractName: &tx.Metadata.ContractName,
				EventName:    &tx.Metadata.EventName,
				FunctionName: &tx.Metadata.FunctionName,
				Data:         tx.Metadata.Data,
			},
		})
	}

	return result
}
