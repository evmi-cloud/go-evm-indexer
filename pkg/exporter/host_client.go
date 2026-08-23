package exporter

import (
	"context"

	exporterproto "github.com/evmi-cloud/go-evm-indexer/pkg/exporter/proto"
)

// hostClient is the plugin-side implementation of Host: it forwards each call
// over the broker connection back to the EVMI server. Plugin authors never
// construct it — it arrives on Context.Host.
type hostClient struct {
	client exporterproto.ExporterHostServiceClient
}

var _ Host = (*hostClient)(nil)

func (h *hostClient) Blockchain() (Blockchain, error) {
	resp, err := h.client.GetBlockchain(context.Background(), &exporterproto.GetBlockchainRequest{})
	if err != nil {
		return Blockchain{}, pluginError(err)
	}
	return Blockchain{
		Id:              resp.Id,
		ChainId:         resp.ChainId,
		Name:            resp.Name,
		RpcUrl:          resp.RpcUrl,
		BlockRange:      resp.BlockRange,
		BlockSlice:      resp.BlockSlice,
		PullInterval:    resp.PullInterval,
		RpcMaxBatchSize: resp.RpcMaxBatchSize,
	}, nil
}

func (h *hostClient) CreateLogSource(src NewLogSource) (SourceRef, error) {
	resp, err := h.client.CreateLogSource(context.Background(), &exporterproto.CreateLogSourceRequest{
		ParentSourceId: src.Parent,
		Address:        src.Address,
		Type:           string(src.Type),
		AbiId:          src.AbiId,
		StartBlock:     src.StartBlock,
	})
	if err != nil {
		return SourceRef{}, pluginError(err)
	}
	return SourceRef{Id: resp.SourceId, Created: resp.Created}, nil
}

func (h *hostClient) UpsertAbi(contractName string, content string) (AbiRef, error) {
	resp, err := h.client.UpsertAbi(context.Background(), &exporterproto.UpsertAbiRequest{
		ContractName: contractName,
		Content:      content,
	})
	if err != nil {
		return AbiRef{}, pluginError(err)
	}
	return AbiRef{Id: resp.AbiId, Created: resp.Created}, nil
}

func (h *hostClient) GetAbi(contractName string) (Abi, bool, error) {
	return h.getAbi(&exporterproto.GetAbiRequest{ContractName: contractName})
}

func (h *hostClient) GetAbiByID(id uint64) (Abi, bool, error) {
	return h.getAbi(&exporterproto.GetAbiRequest{AbiId: id})
}

func (h *hostClient) getAbi(req *exporterproto.GetAbiRequest) (Abi, bool, error) {
	resp, err := h.client.GetAbi(context.Background(), req)
	if err != nil {
		return Abi{}, false, pluginError(err)
	}
	if !resp.Found || resp.Abi == nil {
		return Abi{}, false, nil
	}
	return abiFromProto(resp.Abi), true, nil
}

func (h *hostClient) ListAbis() ([]Abi, error) {
	resp, err := h.client.ListAbis(context.Background(), &exporterproto.ListAbisRequest{})
	if err != nil {
		return nil, pluginError(err)
	}
	out := make([]Abi, 0, len(resp.Abis))
	for _, a := range resp.Abis {
		out = append(out, abiFromProto(a))
	}
	return out, nil
}

func abiFromProto(a *exporterproto.Abi) Abi {
	if a == nil {
		return Abi{}
	}
	return Abi{Id: a.Id, ContractName: a.ContractName, Content: a.Content}
}
