package exporter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// exporterHost implements pkg/exporter.Host: the EVMI functions a plugin can
// call back into while it runs. One instance is built per running exporter and
// is bound to that exporter's pipeline and chain, which is what scopes the API —
// a plugin can only attach sources to its own pipeline's topology.
//
// It is served over a go-plugin broker connection (pkg/exporter/host_server.go),
// so every method here answers a call made from inside the plugin process. That
// means it can run concurrently with the export loop and with the plugin's own
// goroutines; it keeps no mutable state and leaves consistency to the database.
type exporterHost struct {
	db  *evmi_database.EvmiDatabase
	bus *bus.Bus

	// pipelineID scopes every call: sources may only be attached under a parent
	// belonging to this pipeline.
	pipelineID uint
	chain      evmi_database.EvmBlockchain

	exporterName string
	logger       zerolog.Logger
}

var _ pluginsdk.Host = (*exporterHost)(nil)

// Blockchain returns the chain this exporter's pipeline indexes, RPC endpoint
// included, so the plugin can open its own client against the same node.
func (h *exporterHost) Blockchain() (pluginsdk.Blockchain, error) {
	return pluginsdk.Blockchain{
		Id:              uint64(h.chain.ID),
		ChainId:         h.chain.ChainId,
		Name:            h.chain.Name,
		RpcUrl:          h.chain.RpcUrl,
		BlockRange:      h.chain.BlockRange,
		BlockSlice:      h.chain.BlockSlice,
		PullInterval:    h.chain.PullInterval,
		RpcMaxBatchSize: h.chain.RpcMaxBatchSize,
	}, nil
}

// CreateLogSource registers a contract as a child source of an existing source,
// for deployments the FACTORY rules cannot catch on their own (typically when
// the creation event does not carry the new address and the plugin had to
// resolve it another way).
//
// It mirrors the factory system's semantics deliberately, so a plugin-created
// child is indistinguishable from a rule-created one: same pipeline, store and
// chain as its parent, created enabled, started immediately over the bus, and
// nested under the parent in the UI. It is likewise idempotent per
// (parent, address) — required here, because log delivery is at-least-once and a
// plugin re-seeing a deployment log after a restart must not create a duplicate.
func (h *exporterHost) CreateLogSource(src pluginsdk.NewLogSource) (pluginsdk.SourceRef, error) {
	address := strings.ToLower(strings.TrimSpace(src.Address))
	if address == "" {
		return pluginsdk.SourceRef{}, errors.New("address is required")
	}
	if src.Parent == 0 {
		return pluginsdk.SourceRef{}, errors.New("parent source id is required")
	}

	sourceType := src.Type
	if sourceType == "" {
		sourceType = pluginsdk.SourceContract
	}
	if sourceType != pluginsdk.SourceContract && sourceType != pluginsdk.SourceFactory {
		return pluginsdk.SourceRef{}, fmt.Errorf("unsupported source type %q: want CONTRACT or FACTORY", sourceType)
	}

	// The parent must exist and belong to this exporter's pipeline: that check is
	// the only thing keeping a plugin inside its own topology.
	var parent evmi_database.EvmLogSource
	if err := h.db.Conn.First(&parent, uint(src.Parent)).Error; err != nil {
		return pluginsdk.SourceRef{}, fmt.Errorf("parent source %d: %w", src.Parent, err)
	}
	if parent.EvmLogPipelineID != h.pipelineID {
		return pluginsdk.SourceRef{}, fmt.Errorf(
			"parent source %d belongs to pipeline %d, not this exporter's pipeline %d",
			src.Parent, parent.EvmLogPipelineID, h.pipelineID)
	}

	if src.AbiId != 0 {
		var count int64
		if err := h.db.Conn.Model(&evmi_database.EvmJsonAbi{}).
			Where("id = ?", uint(src.AbiId)).Count(&count).Error; err != nil {
			return pluginsdk.SourceRef{}, err
		}
		if count == 0 {
			return pluginsdk.SourceRef{}, fmt.Errorf("abi %d does not exist", src.AbiId)
		}
	}

	// Idempotent per (parent, address). Addresses compare lowercased, so a
	// checksummed address and its lowercase form are the same contract.
	var existing evmi_database.EvmLogSource
	err := h.db.Conn.
		Where("parent_source_id = ? AND LOWER(address) = ?", parent.ID, address).
		First(&existing).Error
	if err == nil {
		return pluginsdk.SourceRef{Id: uint64(existing.ID), Created: false}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return pluginsdk.SourceRef{}, err
	}

	// The cursor sits just before the first block to index. Guard the underflow
	// at block 0, which would otherwise wrap to the maximum uint64 and leave the
	// source permanently ahead of the chain head.
	cursor := src.StartBlock
	if cursor > 0 {
		cursor--
	}

	child := evmi_database.EvmLogSource{
		Enabled:          true,
		Status:           string(evmi_database.StoppedLogSourceStatus),
		Type:             string(sourceType),
		StartBlock:       cursor,
		SyncBlock:        cursor,
		Address:          sql.NullString{String: address, Valid: true},
		ParentSourceID:   parent.ID,
		EvmLogPipelineID: parent.EvmLogPipelineID,
		EvmJsonAbiID:     uint(src.AbiId),
		EvmBlockchainID:  parent.EvmBlockchainID,
	}
	if err := h.db.Conn.Create(&child).Error; err != nil {
		return pluginsdk.SourceRef{}, err
	}

	h.logger.Info().
		Str("exporter", h.exporterName).
		Uint("source", child.ID).
		Uint("parent", parent.ID).
		Str("address", address).
		Str("type", string(sourceType)).
		Msg("plugin registered log source")

	// Best-effort start, exactly like a factory child: on a missed emit the source
	// stays enabled in the DB and starts on the next manager boot.
	if h.bus != nil {
		h.bus.Emit(context.Background(), internal_bus.EnableSourceTopic, child.ID)
	}

	return pluginsdk.SourceRef{Id: uint64(child.ID), Created: true}, nil
}

// UpsertAbi registers an ABI under a contract name if that name is free, and
// returns its id either way. It never overwrites an existing ABI: rewriting one
// would silently change how every source already decoding with it behaves.
func (h *exporterHost) UpsertAbi(contractName string, content string) (pluginsdk.AbiRef, error) {
	name := strings.TrimSpace(contractName)
	if name == "" {
		return pluginsdk.AbiRef{}, errors.New("contract name is required")
	}

	var existing evmi_database.EvmJsonAbi
	err := h.db.Conn.Where("contract_name = ?", name).First(&existing).Error
	if err == nil {
		return pluginsdk.AbiRef{Id: uint64(existing.ID), Created: false}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return pluginsdk.AbiRef{}, err
	}

	// Reject a malformed ABI here rather than letting a source fail to decode long
	// afterwards, when the cause is no longer obvious.
	if strings.TrimSpace(content) == "" {
		return pluginsdk.AbiRef{}, errors.New("abi content is required")
	}
	if _, err := abi.JSON(strings.NewReader(content)); err != nil {
		return pluginsdk.AbiRef{}, fmt.Errorf("invalid contract abi: %w", err)
	}

	created := evmi_database.EvmJsonAbi{ContractName: name, Content: content}
	if err := h.db.Conn.Create(&created).Error; err != nil {
		return pluginsdk.AbiRef{}, err
	}

	h.logger.Info().
		Str("exporter", h.exporterName).
		Uint("abi", created.ID).
		Str("contract", name).
		Msg("plugin registered abi")

	return pluginsdk.AbiRef{Id: uint64(created.ID), Created: true}, nil
}

// GetAbi looks one ABI up by contract name.
func (h *exporterHost) GetAbi(contractName string) (pluginsdk.Abi, bool, error) {
	return h.findAbi(h.db.Conn.Where("contract_name = ?", strings.TrimSpace(contractName)))
}

// GetAbiByID looks one ABI up by id.
func (h *exporterHost) GetAbiByID(id uint64) (pluginsdk.Abi, bool, error) {
	return h.findAbi(h.db.Conn.Where("id = ?", uint(id)))
}

func (h *exporterHost) findAbi(query *gorm.DB) (pluginsdk.Abi, bool, error) {
	var found evmi_database.EvmJsonAbi
	if err := query.First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return pluginsdk.Abi{}, false, nil
		}
		return pluginsdk.Abi{}, false, err
	}
	return toSdkAbi(found), true, nil
}

// ListAbis returns every registered ABI, so a plugin can reconcile everything it
// needs in one call at Init instead of probing names one at a time.
func (h *exporterHost) ListAbis() ([]pluginsdk.Abi, error) {
	var abis []evmi_database.EvmJsonAbi
	if err := h.db.Conn.Find(&abis).Error; err != nil {
		return nil, err
	}
	out := make([]pluginsdk.Abi, 0, len(abis))
	for _, a := range abis {
		out = append(out, toSdkAbi(a))
	}
	return out, nil
}

func toSdkAbi(a evmi_database.EvmJsonAbi) pluginsdk.Abi {
	return pluginsdk.Abi{Id: uint64(a.ID), ContractName: a.ContractName, Content: a.Content}
}
