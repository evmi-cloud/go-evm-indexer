package grpc

import (
	"context"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmBlockchain implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmBlockchain(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmBlockchainRequest]) (*connect.Response[evm_indexerv1.CreateEvmBlockchainResponse], error) {
	newBlockchain := evmi_database.EvmBlockchain{
		ChainId:         req.Msg.Blockchain.ChainId,
		Name:            req.Msg.Blockchain.Name,
		RpcUrl:          req.Msg.Blockchain.RpcUrl,
		BlockRange:      req.Msg.Blockchain.BlockRange,
		BlockSlice:      req.Msg.Blockchain.BlockSlice,
		PullInterval:    req.Msg.Blockchain.PullInterval,
		RpcMaxBatchSize: req.Msg.Blockchain.RpcMaxBatchSize,
	}

	result := e.db.Conn.Create(&newBlockchain)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.CreateEvmBlockchainResponse]{
		Msg: &evm_indexerv1.CreateEvmBlockchainResponse{
			Id: uint32(newBlockchain.ID),
		},
	}, nil
}

// GetEvmBlockchain implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmBlockchain(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmBlockchainRequest]) (*connect.Response[evm_indexerv1.GetEvmBlockchainResponse], error) {

	var blockchain evmi_database.EvmBlockchain

	result := e.db.Conn.First(&blockchain, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.GetEvmBlockchainResponse]{
		Msg: &evm_indexerv1.GetEvmBlockchainResponse{
			Blockchain: toGrpcBlockchain(blockchain),
		},
	}, nil
}

// ListEvmBlockchains implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmBlockchains(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmBlockchainsRequest]) (*connect.Response[evm_indexerv1.ListEvmBlockchainsResponse], error) {
	var blockchains []evmi_database.EvmBlockchain

	result := e.db.Conn.Model(&evmi_database.EvmBlockchain{}).Find(&blockchains).Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmBlockchainsResponse]{
		Msg: &evm_indexerv1.ListEvmBlockchainsResponse{
			Blockchains: toGrpcBlockchains(blockchains),
		},
	}, nil
}

// UpdateEvmBlockchain implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmBlockchain(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmBlockchainRequest]) (*connect.Response[evm_indexerv1.UpdateEvmBlockchainResponse], error) {
	var blockchain evmi_database.EvmBlockchain

	result := e.db.Conn.First(&blockchain, req.Msg.Blockchain.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	blockchain.ChainId = req.Msg.Blockchain.ChainId
	blockchain.Name = req.Msg.Blockchain.Name
	blockchain.RpcUrl = req.Msg.Blockchain.RpcUrl
	blockchain.BlockRange = req.Msg.Blockchain.BlockRange
	blockchain.BlockSlice = req.Msg.Blockchain.BlockSlice
	blockchain.PullInterval = req.Msg.Blockchain.PullInterval
	blockchain.RpcMaxBatchSize = req.Msg.Blockchain.RpcMaxBatchSize

	result = e.db.Conn.Save(&blockchain)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.UpdateEvmBlockchainResponse]{
		Msg: &evm_indexerv1.UpdateEvmBlockchainResponse{},
	}, nil
}

// DeleteEvmBlockchain implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmBlockchain(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmBlockchainRequest]) (*connect.Response[evm_indexerv1.DeleteEvmBlockchainResponse], error) {

	//TODO: verify there is dependent entities

	result := e.db.Conn.Delete(&evmi_database.EvmBlockchain{}, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.DeleteEvmBlockchainResponse]{
		Msg: &evm_indexerv1.DeleteEvmBlockchainResponse{},
	}, nil
}

func toGrpcBlockchain(blockchain evmi_database.EvmBlockchain) *evm_indexerv1.EvmBlockchain {
	id := uint32(blockchain.ID)
	createdAt := uint32(blockchain.CreatedAt.Unix())
	updatedAt := uint32(blockchain.UpdatedAt.Unix())
	deletedAt := uint32(blockchain.DeletedAt.Time.Unix())
	return &evm_indexerv1.EvmBlockchain{
		Id:              &id,
		ChainId:         blockchain.ChainId,
		Name:            blockchain.Name,
		RpcUrl:          blockchain.RpcUrl,
		BlockRange:      blockchain.BlockRange,
		BlockSlice:      blockchain.BlockSlice,
		PullInterval:    blockchain.PullInterval,
		RpcMaxBatchSize: blockchain.RpcMaxBatchSize,
		CreatedAt:       &createdAt,
		UpdatedAt:       &updatedAt,
		DeletedAt:       &deletedAt,
	}
}

func toGrpcBlockchains(blockchains []evmi_database.EvmBlockchain) []*evm_indexerv1.EvmBlockchain {
	var result []*evm_indexerv1.EvmBlockchain

	for _, blockchain := range blockchains {
		result = append(result, toGrpcBlockchain(blockchain))
	}

	return result
}
