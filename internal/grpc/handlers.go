package grpc

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/evmi-cloud/go-evm-indexer/internal/database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/google/uuid"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
)

type EvmIndexerServer struct {
	db     *database.IndexerDatabase
	bus    *bus.Bus
	logger zerolog.Logger
}

// GetStores implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetStores(ctx context.Context, req *connect.Request[evm_indexerv1.GetStoresRequest]) (*connect.Response[evm_indexerv1.GetStoresResponse], error) {

	var result []*evm_indexerv1.LogStore

	db, err := e.db.GetStoreDatabase()
	if err != nil {
		return &connect.Response[evm_indexerv1.GetStoresResponse]{
			Msg: &evm_indexerv1.GetStoresResponse{
				Success: false,
				Error:   err.Error(),
			},
		}, nil
	}

	stores, err := db.GetStores()
	if err != nil {
		return &connect.Response[evm_indexerv1.GetStoresResponse]{
			Msg: &evm_indexerv1.GetStoresResponse{
				Success: false,
				Error:   err.Error(),
			},
		}, nil
	}

	for _, store := range stores {
		s := evm_indexerv1.LogStore{
			Id:          store.Id,
			Identifier:  store.Identifier,
			Description: store.Description,
			Status:      string(store.Status),

			RpcUrl:  store.Rpc,
			Sources: []*evm_indexerv1.LogSource{},
		}

		sources, err := db.GetSources(s.Id)
		if err != nil {
			return &connect.Response[evm_indexerv1.GetStoresResponse]{
				Msg: &evm_indexerv1.GetStoresResponse{
					Success: false,
					Error:   err.Error(),
				},
			}, nil
		}

		for _, source := range sources {
			contracts := []*evm_indexerv1.LogSourceContract{}
			for _, contract := range source.Contracts {
				contracts = append(contracts, &evm_indexerv1.LogSourceContract{
					Address:      contract.Address,
					ContractName: contract.ContractName,
				})
			}

			s.Sources = append(s.Sources, &evm_indexerv1.LogSource{
				Name:               source.Name,
				Type:               string(source.Type),
				Contracts:          contracts,
				Topic:              source.Topic,
				StartBlock:         source.StartBlock,
				LatestBlockIndexed: source.LatestBlockIndexed,
			})
		}

		result = append(result, &s)
	}

	return &connect.Response[evm_indexerv1.GetStoresResponse]{
		Msg: &evm_indexerv1.GetStoresResponse{
			Success: true,
			Stores:  result,
		},
	}, nil
}

// GetStoreLogs implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetStoreLogs(ctx context.Context, req *connect.Request[evm_indexerv1.GetStoreLogsRequest]) (*connect.Response[evm_indexerv1.GetStoreLogsResponse], error) {
	database, err := e.db.GetStoreDatabase()
	if err != nil {
		return &connect.Response[evm_indexerv1.GetStoreLogsResponse]{
			Msg: &evm_indexerv1.GetStoreLogsResponse{
				Success: false,
				Error:   err.Error(),
			},
		}, nil
	}

	var result []*evm_indexerv1.EvmLog
	logs, err := database.GetLogs(req.Msg.Id, req.Msg.FromBlock, req.Msg.ToBlock, req.Msg.Limit, req.Msg.Offset)
	if err != nil {
		return &connect.Response[evm_indexerv1.GetStoreLogsResponse]{
			Msg: &evm_indexerv1.GetStoreLogsResponse{
				Success: false,
				Error:   err.Error(),
			},
		}, nil
	}

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
			MintedAt:         log.MintedAt,

			Metadata: &evm_indexerv1.EvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		})
	}

	return &connect.Response[evm_indexerv1.GetStoreLogsResponse]{
		Msg: &evm_indexerv1.GetStoreLogsResponse{
			Success: true,
			Logs:    result,
		},
	}, nil
}

// GetLatestsStoreLogs implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetLatestsStoreLogs(ctx context.Context, req *connect.Request[evm_indexerv1.GetLatestStoreLogsRequest]) (*connect.Response[evm_indexerv1.GetLatestStoreLogsResponse], error) {
	database, err := e.db.GetStoreDatabase()
	if err != nil {
		return &connect.Response[evm_indexerv1.GetLatestStoreLogsResponse]{
			Msg: &evm_indexerv1.GetLatestStoreLogsResponse{
				Success: false,
				Error:   err.Error(),
			},
		}, nil
	}

	logs, err := database.GetLatestLogs(req.Msg.Id, req.Msg.Limit)
	if err != nil {
		return &connect.Response[evm_indexerv1.GetLatestStoreLogsResponse]{
			Msg: &evm_indexerv1.GetLatestStoreLogsResponse{
				Success: false,
				Error:   err.Error(),
			},
		}, nil
	}

	return &connect.Response[evm_indexerv1.GetLatestStoreLogsResponse]{
		Msg: &evm_indexerv1.GetLatestStoreLogsResponse{
			Success: true,
			Logs:    toGrpcLogs(logs),
		},
	}, nil
}

// GetStoreLogStream implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetStoreLogStream(ctx context.Context, req *connect.Request[evm_indexerv1.GetStoreLogsStreamRequest], stream *connect.ServerStream[evm_indexerv1.GetStoreLogsStreamResponse]) error {
	database, err := e.db.GetStoreDatabase()
	if err != nil {
		return err
	}

	globalLatestBlock, err := database.GetStoreGlobalLatestBlock(req.Msg.Id)
	if err != nil {
		return err
	}

	fromBlock := req.Msg.FromBlock

	var toBlock uint64
	if req.Msg.ToLatest {
		toBlock = globalLatestBlock
	} else {
		if req.Msg.ToBlock > globalLatestBlock {
			return errors.New("toBlock is higher than global latest block")
		}
		toBlock = req.Msg.ToBlock
	}

	limit := req.Msg.BatchSize
	offset := uint64(0)
	finished := false

	for !finished {
		logs, err := database.GetLogs(req.Msg.Id, fromBlock, toBlock, limit, offset)
		if err != nil {
			return err
		}

		if len(logs) > 0 {
			stream.Send(&evm_indexerv1.GetStoreLogsStreamResponse{
				Logs: toGrpcLogs(logs),
			})

			offset += limit
		} else {
			finished = true
		}
	}

	//if it is not to the latest, close stream
	if !req.Msg.ToLatest {
		return nil
	}

	//First load done, now recheck block emitted since the beginning of the function
	newGlobalLatestBlock, err := database.GetStoreGlobalLatestBlock(req.Msg.Id)
	if err != nil {
		return err
	}

	logs, err := database.GetLogs(req.Msg.Id, globalLatestBlock, newGlobalLatestBlock, 9999999, 0)
	if err != nil {
		return err
	}

	if len(logs) > 0 {
		stream.Send(&evm_indexerv1.GetStoreLogsStreamResponse{
			Logs: toGrpcLogs(logs),
		})
	}

	//Now listen for new blocks and send them when detected
	handlerId := uuid.New()
	e.bus.RegisterHandler(handlerId.String(), bus.Handler{
		Handle: func(ctx context.Context, e bus.Event) {
			logs := e.Data.([]types.EvmLog)
			stream.Send(&evm_indexerv1.GetStoreLogsStreamResponse{Logs: toGrpcLogs(logs)})
		},
		Matcher: "logs.new",
	})

	defer e.bus.DeregisterHandler(handlerId.String())

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Task cancelled")
			return nil
		}
	}

}

// StartPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) StartPipeline(context.Context, *connect.Request[evm_indexerv1.StartPipelineRequest]) (*connect.Response[evm_indexerv1.StartPipelineResponse], error) {
	panic("unimplemented")
}

// StopPipeline implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) StopPipeline(context.Context, *connect.Request[evm_indexerv1.StopPipelineRequest]) (*connect.Response[evm_indexerv1.StopPipelineResponse], error) {
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
			MintedAt:         log.MintedAt,

			Metadata: &evm_indexerv1.EvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		})
	}

	return result
}
