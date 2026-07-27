package grpc

import (
	"context"
	"encoding/json"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
)

// ExportConfiguration dumps the whole configuration as a complete config file: the
// non-DB entries (`database`, `metrics`, `pluginStorage`) come straight from the
// loaded config, and `plugins` + `resources` are rebuilt from the metadata DB
// (blockchains, ABIs, stores, pipelines, sources, plugins, exporters). Sources
// created by a factory at runtime (ParentSourceID != 0) are omitted — they are
// re-created from their parent factory's rules on replay, not declared. All
// cross-references are emitted by natural name/identifier (not DB id), matching the
// autoloader's resolution. Admin-only: the output contains store/database
// credentials.
func (e *EvmIndexerServer) ExportConfiguration(ctx context.Context, req *connect.Request[evm_indexerv1.ExportConfigurationRequest]) (*connect.Response[evm_indexerv1.ExportConfigurationResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	cfg, err := e.buildExportedConfig()
	if err != nil {
		return nil, dbError(err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.ExportConfigurationResponse{ConfigJson: string(data)}), nil
}

// buildExportedConfig returns a full config: the loaded config with its `plugins`
// and `resources` replaced by the current metadata-DB state.
func (e *EvmIndexerServer) buildExportedConfig() (types.Config, error) {
	// Start from the loaded config so database/metrics/pluginStorage are preserved.
	out := e.config
	out.Plugins = nil
	out.Resources = types.AutoloadResources{}
	db := e.db.Conn

	var blockchains []evmi_database.EvmBlockchain
	if err := db.Find(&blockchains).Error; err != nil {
		return out, err
	}
	var abis []evmi_database.EvmJsonAbi
	if err := db.Find(&abis).Error; err != nil {
		return out, err
	}
	var stores []evmi_database.EvmLogStore
	if err := db.Find(&stores).Error; err != nil {
		return out, err
	}
	var pipelines []evmi_database.EvmLogPipeline
	if err := db.Find(&pipelines).Error; err != nil {
		return out, err
	}
	// Only user-declared sources — factory-created children are excluded.
	var sources []evmi_database.EvmLogSource
	if err := db.Where("parent_source_id = 0 OR parent_source_id IS NULL").Find(&sources).Error; err != nil {
		return out, err
	}
	var exporters []evmi_database.EvmiExporter
	if err := db.Find(&exporters).Error; err != nil {
		return out, err
	}
	var plugins []evmi_database.Plugin
	if err := db.Find(&plugins).Error; err != nil {
		return out, err
	}

	// id → natural key, so cross-references are emitted by name.
	blockchainName := map[uint]string{}
	for _, b := range blockchains {
		blockchainName[b.ID] = b.Name
	}
	storeIdentifier := map[uint]string{}
	for _, s := range stores {
		storeIdentifier[s.ID] = s.Identifier
	}
	abiName := map[uint]string{}
	for _, a := range abis {
		abiName[a.ID] = a.ContractName
	}
	pipelineName := map[uint]string{}
	for _, p := range pipelines {
		pipelineName[p.ID] = p.Name
	}
	pluginName := map[uint]string{}
	for _, p := range plugins {
		pluginName[p.ID] = p.Name
	}

	for _, b := range blockchains {
		out.Resources.Blockchains = append(out.Resources.Blockchains, types.ConfigBlockchain{
			Name:                b.Name,
			ChainId:             b.ChainId,
			RpcUrl:              b.RpcUrl,
			BlockRange:          b.BlockRange,
			BlockSlice:          b.BlockSlice,
			PullInterval:        b.PullInterval,
			RpcMaxBatchSize:     b.RpcMaxBatchSize,
			SqdGatewayAvailable: b.SqdGatewayAvailable,
			SqdGatewayUrl:       b.SqdGatewayUrl,
		})
	}
	for _, a := range abis {
		out.Resources.Abis = append(out.Resources.Abis, types.ConfigAbi{
			ContractName: a.ContractName,
			Content:      a.Content,
		})
	}
	for _, s := range stores {
		out.Resources.Stores = append(out.Resources.Stores, types.ConfigStore{
			Identifier:  s.Identifier,
			Description: s.Description,
			StoreType:   s.StoreType,
			StoreConfig: json.RawMessage(s.StoreConfig),
		})
	}
	for _, p := range pipelines {
		out.Resources.Pipelines = append(out.Resources.Pipelines, types.ConfigPipeline{
			Name:       p.Name,
			Blockchain: blockchainName[p.EvmBlockchainID],
			Store:      storeIdentifier[p.EvmLogStoreId],
		})
	}
	for _, s := range sources {
		src := types.ConfigSource{
			Pipeline:   pipelineName[s.EvmLogPipelineID],
			Blockchain: blockchainName[s.EvmBlockchainID],
			Abi:        abiName[s.EvmJsonAbiID],
			Type:       s.Type,
			Enabled:    s.Enabled,
			StartBlock: s.StartBlock,
		}
		if s.Address.Valid {
			src.Address = s.Address.String
		}
		if s.Topic0.Valid {
			src.Topic0 = s.Topic0.String
		}
		src.TopicFilters = []string(s.TopicFilters)
		if s.Type == string(evmi_database.FactoryLogSourceType) {
			rules, err := e.exportFactoryRules(s.ID, abiName)
			if err != nil {
				return out, err
			}
			src.FactoryRules = rules
		}
		out.Resources.Sources = append(out.Resources.Sources, src)
	}
	for _, x := range exporters {
		exp := types.ConfigExporter{
			Name:       x.Name,
			Pipeline:   pipelineName[x.EvmLogPipelineID],
			Plugin:     pluginName[x.PluginID],
			Enabled:    x.Enabled,
			StartBlock: x.StartBlock,
		}
		if len(x.PluginConfig) > 0 {
			exp.PluginConfig = json.RawMessage(x.PluginConfig)
		}
		out.Resources.Exporters = append(out.Resources.Exporters, exp)
	}
	for _, p := range plugins {
		out.Plugins = append(out.Plugins, types.ConfigPlugin{
			Name:        p.Name,
			Description: p.Description,
			GitUrl:      p.GitUrl,
			GitRef:      p.GitRef,
		})
	}

	return out, nil
}

// exportFactoryRules rebuilds a FACTORY source's creation-rule tree as config
// (ChildAbi by name, recursive via ChildRules).
func (e *EvmIndexerServer) exportFactoryRules(sourceID uint, abiName map[uint]string) ([]types.ConfigFactoryRule, error) {
	return e.exportRulesAt(sourceID, nil, abiName)
}

func (e *EvmIndexerServer) exportRulesAt(sourceID uint, parentRuleID *uint, abiName map[uint]string) ([]types.ConfigFactoryRule, error) {
	var rows []evmi_database.EvmFactoryRule
	q := e.db.Conn.Preload("Conditions")
	if parentRuleID != nil {
		q = q.Where("parent_rule_id = ?", *parentRuleID)
	} else {
		q = q.Where("evm_log_source_id = ? AND parent_rule_id IS NULL", sourceID)
	}
	if err := q.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]types.ConfigFactoryRule, 0, len(rows))
	for _, r := range rows {
		ruleID := r.ID
		children, err := e.exportRulesAt(0, &ruleID, abiName)
		if err != nil {
			return nil, err
		}
		var conditions []types.ConfigFactoryRuleCondition
		for _, c := range r.Conditions {
			conditions = append(conditions, types.ConfigFactoryRuleCondition{
				Arg: c.Arg, Operator: c.Operator, Value: c.Value,
			})
		}
		out = append(out, types.ConfigFactoryRule{
			CreationFunctionName:  r.CreationFunctionName,
			CreationAddressLogArg: r.CreationAddressLogArg,
			ChildAbi:              abiName[r.EvmJsonAbiID],
			ChildType:             r.ChildType,
			Conditions:            conditions,
			ChildRules:            children,
		})
	}
	return out, nil
}
