package grpc

import (
	"context"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmJsonAbi(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.CreateEvmJsonAbiResponse], error) {
	newAbi := evmi_database.EvmJsonAbi{
		ContractName: req.Msg.Abi.ContractName,
		Content:      req.Msg.Abi.Content,
	}

	result := e.db.Conn.Create(&newAbi)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.CreateEvmJsonAbiResponse]{
		Msg: &evm_indexerv1.CreateEvmJsonAbiResponse{
			Id: uint32(newAbi.ID),
		},
	}, nil
}

// ListEvmJsonAbis implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmJsonAbis(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmJsonAbisRequest]) (*connect.Response[evm_indexerv1.ListEvmJsonAbisResponse], error) {
	var abis []evmi_database.EvmJsonAbi

	result := e.db.Conn.Model(&evmi_database.EvmJsonAbi{}).Find(&abis).Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmJsonAbisResponse]{
		Msg: &evm_indexerv1.ListEvmJsonAbisResponse{
			Abis: toGrpcAbis(abis),
		},
	}, nil
}

// DeleteEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmJsonAbi(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.DeleteEvmJsonAbiResponse], error) {
	//TODO: verify there is dependent entities

	result := e.db.Conn.Delete(&evmi_database.EvmJsonAbi{}, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.DeleteEvmJsonAbiResponse]{
		Msg: &evm_indexerv1.DeleteEvmJsonAbiResponse{},
	}, nil
}

// GetEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmJsonAbi(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.GetEvmJsonAbiResponse], error) {
	var abi evmi_database.EvmJsonAbi

	result := e.db.Conn.First(&abi, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.GetEvmJsonAbiResponse]{
		Msg: &evm_indexerv1.GetEvmJsonAbiResponse{
			Abi: toGrpcAbi(abi),
		},
	}, nil
}

// UpdateEvmJsonAbi implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmJsonAbi(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmJsonAbiRequest]) (*connect.Response[evm_indexerv1.UpdateEvmJsonAbiResponse], error) {
	var blockchain evmi_database.EvmJsonAbi

	result := e.db.Conn.First(&blockchain, req.Msg.Abi.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	blockchain.Content = req.Msg.Abi.Content
	blockchain.ContractName = req.Msg.Abi.ContractName

	result = e.db.Conn.Save(&blockchain)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.UpdateEvmJsonAbiResponse]{
		Msg: &evm_indexerv1.UpdateEvmJsonAbiResponse{},
	}, nil
}

func toGrpcAbi(blockchain evmi_database.EvmJsonAbi) *evm_indexerv1.EvmJsonAbi {
	id := uint32(blockchain.ID)
	createdAt := uint32(blockchain.CreatedAt.Unix())
	updatedAt := uint32(blockchain.UpdatedAt.Unix())
	deletedAt := uint32(blockchain.DeletedAt.Time.Unix())
	return &evm_indexerv1.EvmJsonAbi{
		Id:           &id,
		ContractName: blockchain.ContractName,
		Content:      blockchain.Content,
		CreatedAt:    &createdAt,
		UpdatedAt:    &updatedAt,
		DeletedAt:    &deletedAt,
	}
}

func toGrpcAbis(blockchains []evmi_database.EvmJsonAbi) []*evm_indexerv1.EvmJsonAbi {
	var result []*evm_indexerv1.EvmJsonAbi

	for _, blockchain := range blockchains {
		result = append(result, toGrpcAbi(blockchain))
	}

	return result
}
