package grpc

import (
	"context"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogStore(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogStoreResponse], error) {
	newLogStore := evmi_database.EvmLogStore{
		Identifier:  req.Msg.Store.Identifier,
		Description: req.Msg.Store.Description,
		StoreType:   req.Msg.Store.StoreType,
		StoreConfig: []byte(req.Msg.Store.StoreConfigJson),
	}

	result := e.db.Conn.Create(&newLogStore)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.CreateEvmLogStoreResponse]{
		Msg: &evm_indexerv1.CreateEvmLogStoreResponse{
			Id: uint32(newLogStore.ID),
		},
	}, nil
}

// DeleteEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogStore(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogStoreResponse], error) {
	//TODO: verify there is dependent entities

	result := e.db.Conn.Delete(&evmi_database.EvmLogStore{}, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.DeleteEvmLogStoreResponse]{
		Msg: &evm_indexerv1.DeleteEvmLogStoreResponse{},
	}, nil
}

// GetEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogStore(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.GetEvmLogStoreResponse], error) {
	var logStore evmi_database.EvmLogStore

	result := e.db.Conn.First(&logStore, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.GetEvmLogStoreResponse]{
		Msg: &evm_indexerv1.GetEvmLogStoreResponse{
			Store: toGrpcLogStore(logStore),
		},
	}, nil
}

// ListEvmLogStores implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogStores(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmLogStoresRequest]) (*connect.Response[evm_indexerv1.ListEvmLogStoresResponse], error) {
	var logStores []evmi_database.EvmLogStore

	result := e.db.Conn.Model(&evmi_database.EvmLogStore{}).Find(&logStores).Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmLogStoresResponse]{
		Msg: &evm_indexerv1.ListEvmLogStoresResponse{
			Stores: toGrpcLogStores(logStores),
		},
	}, nil
}

// UpdateEvmLogStore implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogStore(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmLogStoreRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogStoreResponse], error) {
	var logStore evmi_database.EvmLogStore

	result := e.db.Conn.First(&logStore, req.Msg.Store.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	logStore.Identifier = req.Msg.Store.Identifier
	logStore.Description = req.Msg.Store.Description
	logStore.StoreType = req.Msg.Store.StoreType
	logStore.StoreConfig = []byte(req.Msg.Store.StoreConfigJson)

	result = e.db.Conn.Save(&logStore)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.UpdateEvmLogStoreResponse]{
		Msg: &evm_indexerv1.UpdateEvmLogStoreResponse{},
	}, nil
}

func toGrpcLogStore(logStore evmi_database.EvmLogStore) *evm_indexerv1.EvmLogStore {
	id := uint32(logStore.ID)
	createdAt := uint32(logStore.CreatedAt.Unix())
	updatedAt := uint32(logStore.UpdatedAt.Unix())
	deletedAt := uint32(logStore.DeletedAt.Time.Unix())

	return &evm_indexerv1.EvmLogStore{
		Id:              &id,
		Identifier:      logStore.Identifier,
		Description:     logStore.Description,
		StoreType:       logStore.StoreType,
		StoreConfigJson: string(logStore.StoreConfig),
		CreatedAt:       &createdAt,
		UpdatedAt:       &updatedAt,
		DeletedAt:       &deletedAt,
	}
}

func toGrpcLogStores(logStores []evmi_database.EvmLogStore) []*evm_indexerv1.EvmLogStore {
	var result []*evm_indexerv1.EvmLogStore

	for _, logStore := range logStores {
		result = append(result, toGrpcLogStore(logStore))
	}

	return result
}
