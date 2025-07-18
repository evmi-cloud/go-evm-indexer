package grpc

import (
	"context"
	"database/sql"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogSourceResponse], error) {
	newLogSource := evmi_database.EvmLogSource{
		Type:       evmi_database.LogSourceType(req.Msg.Source.Type),
		StartBlock: req.Msg.Source.StartBlock,
		SyncBlock:  req.Msg.Source.SyncBlock,
		Address: sql.NullString{
			String: DerefOrEmpty(req.Msg.Source.Address),
			Valid:  IsNotNil(req.Msg.Source.Address),
		},
		Topic0: sql.NullString{
			String: DerefOrEmpty(req.Msg.Source.Topic0),
			Valid:  IsNotNil(req.Msg.Source.Topic0),
		},
		TopicFilters: req.Msg.Source.TopicFilters,

		// Factory type data
		FactoryChildEvmJsonABI: sql.NullInt32{
			Int32: DerefOrEmpty(req.Msg.Source.FactoryChildEvmJsonAbi),
			Valid: IsNotNil(req.Msg.Source.FactoryChildEvmJsonAbi),
		},
		FactoryCreationFunctionName: sql.NullString{
			String: DerefOrEmpty(req.Msg.Source.FactoryCreationFunctionName),
			Valid:  IsNotNil(req.Msg.Source.FactoryCreationFunctionName),
		},
		FactoryCreationAddressLogArg: sql.NullString{
			String: DerefOrEmpty(req.Msg.Source.FactoryCreationAddressLogArg),
			Valid:  IsNotNil(req.Msg.Source.FactoryCreationAddressLogArg),
		},

		EvmLogPipelineID: uint(req.Msg.Source.EvmLogPipelineId),
		EvmJsonAbiID:     uint(req.Msg.Source.EvmJsonAbiId),
	}

	result := e.db.Conn.Create(&newLogSource)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.CreateEvmLogSourceResponse]{
		Msg: &evm_indexerv1.CreateEvmLogSourceResponse{
			Id: uint32(newLogSource.ID),
		},
	}, nil
}

// DeleteEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogSourceResponse], error) {
	//TODO: verify there is dependent entities

	result := e.db.Conn.Delete(&evmi_database.EvmLogSource{}, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.DeleteEvmLogSourceResponse]{
		Msg: &evm_indexerv1.DeleteEvmLogSourceResponse{},
	}, nil
}

// GetEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.GetEvmLogSourceResponse], error) {
	var logSource evmi_database.EvmLogSource

	result := e.db.Conn.First(&logSource, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.GetEvmLogSourceResponse]{
		Msg: &evm_indexerv1.GetEvmLogSourceResponse{
			Source: toGrpcLogSource(logSource),
		},
	}, nil
}

// ListEvmLogSources implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogSources(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmLogSourcesRequest]) (*connect.Response[evm_indexerv1.ListEvmLogSourcesResponse], error) {
	var blockchains []evmi_database.EvmLogSource

	result := e.db.Conn.Model(&evmi_database.EvmLogSource{}).Find(&blockchains).Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmLogSourcesResponse]{
		Msg: &evm_indexerv1.ListEvmLogSourcesResponse{
			Sources: toGrpcLogSources(blockchains),
		},
	}, nil
}

// UpdateEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogSourceResponse], error) {
	var logSoure evmi_database.EvmLogSource

	result := e.db.Conn.First(&logSoure, req.Msg.Source.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	logSoure.Type = evmi_database.LogSourceType(req.Msg.Source.Type)
	logSoure.StartBlock = req.Msg.Source.StartBlock
	logSoure.SyncBlock = req.Msg.Source.SyncBlock
	logSoure.Address = sql.NullString{
		String: DerefOrEmpty(req.Msg.Source.Address),
		Valid:  IsNotNil(req.Msg.Source.Address),
	}
	logSoure.Topic0 = sql.NullString{
		String: DerefOrEmpty(req.Msg.Source.Topic0),
		Valid:  IsNotNil(req.Msg.Source.Topic0),
	}
	logSoure.TopicFilters = req.Msg.Source.TopicFilters

	logSoure.FactoryChildEvmJsonABI = sql.NullInt32{
		Int32: DerefOrEmpty(req.Msg.Source.FactoryChildEvmJsonAbi),
		Valid: IsNotNil(req.Msg.Source.FactoryChildEvmJsonAbi),
	}
	logSoure.FactoryCreationFunctionName = sql.NullString{
		String: DerefOrEmpty(req.Msg.Source.FactoryCreationFunctionName),
		Valid:  IsNotNil(req.Msg.Source.FactoryCreationFunctionName),
	}
	logSoure.FactoryCreationAddressLogArg = sql.NullString{
		String: DerefOrEmpty(req.Msg.Source.FactoryCreationAddressLogArg),
		Valid:  IsNotNil(req.Msg.Source.FactoryCreationAddressLogArg),
	}

	logSoure.EvmLogPipelineID = uint(req.Msg.Source.EvmLogPipelineId)
	logSoure.EvmJsonAbiID = uint(req.Msg.Source.EvmJsonAbiId)

	result = e.db.Conn.Save(&logSoure)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.UpdateEvmLogSourceResponse]{
		Msg: &evm_indexerv1.UpdateEvmLogSourceResponse{},
	}, nil
}

func DerefOrEmpty[T any](val *T) T {
	if val == nil {
		var empty T
		return empty
	}
	return *val
}

func IsNotNil[T any](val *T) bool {
	return val != nil
}

func toGrpcLogSource(logSource evmi_database.EvmLogSource) *evm_indexerv1.EvmLogSource {
	id := uint32(logSource.ID)
	createdAt := uint32(logSource.CreatedAt.Unix())
	updatedAt := uint32(logSource.UpdatedAt.Unix())
	deletedAt := uint32(logSource.DeletedAt.Time.Unix())
	return &evm_indexerv1.EvmLogSource{
		Id:                           &id,
		Type:                         string(logSource.Type),
		StartBlock:                   logSource.StartBlock,
		SyncBlock:                    logSource.SyncBlock,
		Address:                      &logSource.Address.String,
		Topic0:                       &logSource.Topic0.String,
		TopicFilters:                 logSource.TopicFilters,
		FactoryChildEvmJsonAbi:       &logSource.FactoryChildEvmJsonABI.Int32,
		FactoryCreationFunctionName:  &logSource.FactoryCreationFunctionName.String,
		FactoryCreationAddressLogArg: &logSource.FactoryCreationAddressLogArg.String,
		EvmLogPipelineId:             uint32(logSource.EvmLogPipelineID),
		EvmJsonAbiId:                 uint32(logSource.EvmJsonAbiID),

		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
		DeletedAt: &deletedAt,
	}
}

func toGrpcLogSources(logSources []evmi_database.EvmLogSource) []*evm_indexerv1.EvmLogSource {
	var result []*evm_indexerv1.EvmLogSource

	for _, logSource := range logSources {
		result = append(result, toGrpcLogSource(logSource))
	}

	return result
}
