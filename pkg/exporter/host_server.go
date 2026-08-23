package exporter

import (
	"context"

	exporterproto "github.com/evmi-cloud/go-evm-indexer/pkg/exporter/proto"
)

// hostServer adapts a Host implementation to the gRPC service the plugin calls.
// It runs inside the EVMI server process, on a broker connection opened for one
// plugin. The implementation behind it (internal/exporter) is already scoped to
// the calling exporter, so nothing here needs to authorize.
//
// Errors are returned as plain gRPC errors; the plugin's hostClient turns the
// status message back into an error, so the server's message reaches the plugin
// author verbatim.
type hostServer struct {
	exporterproto.UnimplementedExporterHostServiceServer

	impl Host
}

func (s *hostServer) GetBlockchain(ctx context.Context, req *exporterproto.GetBlockchainRequest) (*exporterproto.GetBlockchainResponse, error) {
	chain, err := s.impl.Blockchain()
	if err != nil {
		return nil, err
	}
	return &exporterproto.GetBlockchainResponse{
		Id:              chain.Id,
		ChainId:         chain.ChainId,
		Name:            chain.Name,
		RpcUrl:          chain.RpcUrl,
		BlockRange:      chain.BlockRange,
		BlockSlice:      chain.BlockSlice,
		PullInterval:    chain.PullInterval,
		RpcMaxBatchSize: chain.RpcMaxBatchSize,
	}, nil
}

func (s *hostServer) CreateLogSource(ctx context.Context, req *exporterproto.CreateLogSourceRequest) (*exporterproto.CreateLogSourceResponse, error) {
	ref, err := s.impl.CreateLogSource(NewLogSource{
		Parent:     req.ParentSourceId,
		Address:    req.Address,
		Type:       SourceType(req.Type),
		AbiId:      req.AbiId,
		StartBlock: req.StartBlock,
	})
	if err != nil {
		return nil, err
	}
	return &exporterproto.CreateLogSourceResponse{SourceId: ref.Id, Created: ref.Created}, nil
}

func (s *hostServer) UpsertAbi(ctx context.Context, req *exporterproto.UpsertAbiRequest) (*exporterproto.UpsertAbiResponse, error) {
	ref, err := s.impl.UpsertAbi(req.ContractName, req.Content)
	if err != nil {
		return nil, err
	}
	return &exporterproto.UpsertAbiResponse{AbiId: ref.Id, Created: ref.Created}, nil
}

func (s *hostServer) GetAbi(ctx context.Context, req *exporterproto.GetAbiRequest) (*exporterproto.GetAbiResponse, error) {
	var (
		abi   Abi
		found bool
		err   error
	)
	// abi_id wins when both are set (documented in the proto).
	if req.AbiId != 0 {
		abi, found, err = s.impl.GetAbiByID(req.AbiId)
	} else {
		abi, found, err = s.impl.GetAbi(req.ContractName)
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return &exporterproto.GetAbiResponse{Found: false}, nil
	}
	return &exporterproto.GetAbiResponse{Found: true, Abi: abiToProto(abi)}, nil
}

func (s *hostServer) ListAbis(ctx context.Context, req *exporterproto.ListAbisRequest) (*exporterproto.ListAbisResponse, error) {
	abis, err := s.impl.ListAbis()
	if err != nil {
		return nil, err
	}
	out := make([]*exporterproto.Abi, 0, len(abis))
	for _, a := range abis {
		out = append(out, abiToProto(a))
	}
	return &exporterproto.ListAbisResponse{Abis: out}, nil
}

func abiToProto(a Abi) *exporterproto.Abi {
	return &exporterproto.Abi{Id: a.Id, ContractName: a.ContractName, Content: a.Content}
}
