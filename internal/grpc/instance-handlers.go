package grpc

import (
	"context"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// GetEvmiInstance implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmiInstance(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmiInstanceRequest]) (*connect.Response[evm_indexerv1.GetEvmiInstanceResponse], error) {
	var instance evmi_database.EvmiInstance

	result := e.db.Conn.First(&instance, req.Msg.Id)
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.GetEvmiInstanceResponse]{
		Msg: &evm_indexerv1.GetEvmiInstanceResponse{
			Instance: toGrpcInstance(instance),
		},
	}, nil
}

// ListEvmiInstances implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmiInstances(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmiInstancesRequest]) (*connect.Response[evm_indexerv1.ListEvmiInstancesResponse], error) {
	var instances []evmi_database.EvmiInstance

	result := e.db.Conn.Model(&evmi_database.EvmiInstance{}).Find(&instances).Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	if result.Error != nil {
		return nil, result.Error
	}

	return &connect.Response[evm_indexerv1.ListEvmiInstancesResponse]{
		Msg: &evm_indexerv1.ListEvmiInstancesResponse{
			Instances: toGrpcInstances(instances),
		},
	}, nil
}

func toGrpcInstance(instance evmi_database.EvmiInstance) *evm_indexerv1.EvmiInstance {
	id := uint32(instance.ID)
	createdAt := uint32(instance.CreatedAt.Unix())
	updatedAt := uint32(instance.UpdatedAt.Unix())
	deletedAt := uint32(instance.DeletedAt.Time.Unix())
	return &evm_indexerv1.EvmiInstance{
		Id:         &id,
		InstanceId: instance.InstanceId,
		Ipv4:       instance.IpV4,
		Status:     instance.Status,
		CreatedAt:  &createdAt,
		UpdatedAt:  &updatedAt,
		DeletedAt:  &deletedAt,
	}
}

func toGrpcInstances(instances []evmi_database.EvmiInstance) []*evm_indexerv1.EvmiInstance {
	var result []*evm_indexerv1.EvmiInstance

	for _, instance := range instances {
		result = append(result, toGrpcInstance(instance))
	}

	return result
}
