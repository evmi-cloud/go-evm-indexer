// Package autoloader provisions metadata-DB resources declared in the config
// file with a "create if not exists" policy on startup. It is idempotent —
// existing rows (created on a previous boot or via the gRPC API) are matched by
// their natural key and left untouched — and best-effort: an error on one
// resource is logged and the rest continue.
package autoloader

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/lib/pq"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Load creates the resources declared in the config in dependency order:
// blockchains, ABIs and stores (no dependencies) first, then pipelines (which
// reference a blockchain + store), then sources and exporters (which reference a
// pipeline, and an ABI / plugin). References are resolved by name against the DB,
// so a resource may reference another declared earlier in the same config or one
// already present in the DB.
func Load(db *evmi_database.EvmiDatabase, instanceID uint, res types.AutoloadResources, logger zerolog.Logger) {
	for _, b := range res.Blockchains {
		if _, err := ensureBlockchain(db, b); err != nil {
			logger.Error().Str("blockchain", b.Name).Msg("autoload blockchain: " + err.Error())
		}
	}
	for _, a := range res.Abis {
		if _, err := ensureAbi(db, a); err != nil {
			logger.Error().Str("abi", a.ContractName).Msg("autoload abi: " + err.Error())
		}
	}
	for _, s := range res.Stores {
		if _, err := ensureStore(db, s); err != nil {
			logger.Error().Str("store", s.Identifier).Msg("autoload store: " + err.Error())
		}
	}
	for _, p := range res.Pipelines {
		if _, err := ensurePipeline(db, instanceID, p); err != nil {
			logger.Error().Str("pipeline", p.Name).Msg("autoload pipeline: " + err.Error())
		}
	}
	for _, s := range res.Sources {
		if err := ensureSource(db, instanceID, s, logger); err != nil {
			logger.Error().Str("pipeline", s.Pipeline).Msg("autoload source: " + err.Error())
		}
	}
	for _, e := range res.Exporters {
		if err := ensureExporter(db, instanceID, e, logger); err != nil {
			logger.Error().Str("exporter", e.Name).Msg("autoload exporter: " + err.Error())
		}
	}
}

func ensureBlockchain(db *evmi_database.EvmiDatabase, cfg types.ConfigBlockchain) (uint, error) {
	if cfg.Name == "" {
		return 0, errors.New("blockchain name is required")
	}
	var existing evmi_database.EvmBlockchain
	err := db.Conn.Where("name = ?", cfg.Name).First(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	row := evmi_database.EvmBlockchain{
		ChainId:             cfg.ChainId,
		Name:                cfg.Name,
		RpcUrl:              cfg.RpcUrl,
		BlockRange:          cfg.BlockRange,
		BlockSlice:          cfg.BlockSlice,
		PullInterval:        cfg.PullInterval,
		RpcMaxBatchSize:     cfg.RpcMaxBatchSize,
		SqdGatewayAvailable: cfg.SqdGatewayAvailable,
		SqdGatewayUrl:       cfg.SqdGatewayUrl,
	}
	if err := db.Conn.Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func ensureAbi(db *evmi_database.EvmiDatabase, cfg types.ConfigAbi) (uint, error) {
	if cfg.ContractName == "" {
		return 0, errors.New("abi contractName is required")
	}
	var existing evmi_database.EvmJsonAbi
	err := db.Conn.Where("contract_name = ?", cfg.ContractName).First(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	row := evmi_database.EvmJsonAbi{ContractName: cfg.ContractName, Content: cfg.Content}
	if err := db.Conn.Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func ensureStore(db *evmi_database.EvmiDatabase, cfg types.ConfigStore) (uint, error) {
	if cfg.Identifier == "" {
		return 0, errors.New("store identifier is required")
	}
	var existing evmi_database.EvmLogStore
	err := db.Conn.Where("identifier = ?", cfg.Identifier).First(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	row := evmi_database.EvmLogStore{
		Identifier:  cfg.Identifier,
		Description: cfg.Description,
		StoreType:   cfg.StoreType,
	}
	if len(cfg.StoreConfig) > 0 {
		row.StoreConfig = datatypes.JSON(cfg.StoreConfig)
	}
	if err := db.Conn.Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func ensurePipeline(db *evmi_database.EvmiDatabase, instanceID uint, cfg types.ConfigPipeline) (uint, error) {
	if cfg.Name == "" {
		return 0, errors.New("pipeline name is required")
	}
	var existing evmi_database.EvmLogPipeline
	err := db.Conn.Where("name = ? AND evmi_instance_id = ?", cfg.Name, instanceID).First(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	chainID, err := blockchainIDByName(db, cfg.Blockchain)
	if err != nil {
		return 0, err
	}
	storeID, err := storeIDByIdentifier(db, cfg.Store)
	if err != nil {
		return 0, err
	}

	row := evmi_database.EvmLogPipeline{
		Name:            cfg.Name,
		EvmiInstanceID:  instanceID,
		EvmBlockchainID: chainID,
		EvmLogStoreId:   storeID,
	}
	if err := db.Conn.Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func ensureSource(db *evmi_database.EvmiDatabase, instanceID uint, cfg types.ConfigSource, logger zerolog.Logger) error {
	pipelineID, err := pipelineIDByName(db, cfg.Pipeline, instanceID)
	if err != nil {
		return err
	}
	sourceType := strings.ToUpper(cfg.Type)

	// Blockchain defaults to the pipeline's when not explicitly set.
	var chainID uint
	if cfg.Blockchain != "" {
		if chainID, err = blockchainIDByName(db, cfg.Blockchain); err != nil {
			return err
		}
	} else {
		var pipeline evmi_database.EvmLogPipeline
		if err := db.Conn.First(&pipeline, pipelineID).Error; err != nil {
			return err
		}
		chainID = pipeline.EvmBlockchainID
	}

	// ABI is optional (a FULL source doesn't decode).
	var abiID uint
	if cfg.Abi != "" {
		if abiID, err = abiIDByName(db, cfg.Abi); err != nil {
			return err
		}
	}

	// Existence check by the source's natural key within its pipeline.
	query := db.Conn.Model(&evmi_database.EvmLogSource{}).
		Where("evm_log_pipeline_id = ? AND type = ?", pipelineID, sourceType)
	switch sourceType {
	case string(evmi_database.ContractLogSourceType), string(evmi_database.FactoryLogSourceType):
		query = query.Where("address = ?", cfg.Address)
	case string(evmi_database.TopicLogSourceType):
		query = query.Where("topic0 = ?", cfg.Topic0)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	row := evmi_database.EvmLogSource{
		Enabled:          cfg.Enabled,
		Status:           string(evmi_database.StoppedLogSourceStatus),
		Type:             sourceType,
		StartBlock:       cfg.StartBlock,
		SyncBlock:        cfg.StartBlock,
		EvmLogPipelineID: pipelineID,
		EvmJsonAbiID:     abiID,
		EvmBlockchainID:  chainID,
	}
	if cfg.Address != "" {
		row.Address = sql.NullString{String: cfg.Address, Valid: true}
	}
	if cfg.Topic0 != "" {
		row.Topic0 = sql.NullString{String: cfg.Topic0, Valid: true}
	}
	if len(cfg.TopicFilters) > 0 {
		row.TopicFilters = pq.StringArray(cfg.TopicFilters)
	}
	if sourceType == string(evmi_database.FactoryLogSourceType) {
		if cfg.FactoryChildAbi != "" {
			childID, err := abiIDByName(db, cfg.FactoryChildAbi)
			if err != nil {
				return err
			}
			row.FactoryChildEvmJsonABI = sql.NullInt32{Int32: int32(childID), Valid: true}
		}
		if cfg.FactoryCreationFunctionName != "" {
			row.FactoryCreationFunctionName = sql.NullString{String: cfg.FactoryCreationFunctionName, Valid: true}
		}
		if cfg.FactoryCreationAddressLogArg != "" {
			row.FactoryCreationAddressLogArg = sql.NullString{String: cfg.FactoryCreationAddressLogArg, Valid: true}
		}
	}
	if err := db.Conn.Create(&row).Error; err != nil {
		return err
	}
	logger.Info().Str("pipeline", cfg.Pipeline).Str("type", sourceType).Msg("autoloaded source")
	return nil
}

func ensureExporter(db *evmi_database.EvmiDatabase, instanceID uint, cfg types.ConfigExporter, logger zerolog.Logger) error {
	if cfg.Name == "" {
		return errors.New("exporter name is required")
	}
	pipelineID, err := pipelineIDByName(db, cfg.Pipeline, instanceID)
	if err != nil {
		return err
	}

	var existing evmi_database.EvmiExporter
	err = db.Conn.Where("name = ? AND evm_log_pipeline_id = ?", cfg.Name, pipelineID).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	pluginID, err := pluginIDByName(db, cfg.Plugin)
	if err != nil {
		return err
	}

	row := evmi_database.EvmiExporter{
		Name:             cfg.Name,
		EvmLogPipelineID: pipelineID,
		Enabled:          cfg.Enabled,
		Status:           string(evmi_database.StoppedExporterStatus),
		StartBlock:       cfg.StartBlock,
		SyncBlock:        cfg.StartBlock,
		SyncLogIndex:     -1,
		PluginID:         pluginID,
	}
	if len(cfg.PluginConfig) > 0 {
		row.PluginConfig = datatypes.JSON(cfg.PluginConfig)
	}
	if err := db.Conn.Create(&row).Error; err != nil {
		return err
	}
	logger.Info().Str("exporter", cfg.Name).Msg("autoloaded exporter")
	return nil
}

// --- reference resolvers (by natural name) ---

func blockchainIDByName(db *evmi_database.EvmiDatabase, name string) (uint, error) {
	if name == "" {
		return 0, errors.New("a blockchain reference is required")
	}
	var row evmi_database.EvmBlockchain
	if err := db.Conn.Where("name = ?", name).First(&row).Error; err != nil {
		return 0, fmt.Errorf("blockchain %q not found: %w", name, err)
	}
	return row.ID, nil
}

func abiIDByName(db *evmi_database.EvmiDatabase, contractName string) (uint, error) {
	var row evmi_database.EvmJsonAbi
	if err := db.Conn.Where("contract_name = ?", contractName).First(&row).Error; err != nil {
		return 0, fmt.Errorf("abi %q not found: %w", contractName, err)
	}
	return row.ID, nil
}

func storeIDByIdentifier(db *evmi_database.EvmiDatabase, identifier string) (uint, error) {
	if identifier == "" {
		return 0, errors.New("a store reference is required")
	}
	var row evmi_database.EvmLogStore
	if err := db.Conn.Where("identifier = ?", identifier).First(&row).Error; err != nil {
		return 0, fmt.Errorf("store %q not found: %w", identifier, err)
	}
	return row.ID, nil
}

func pipelineIDByName(db *evmi_database.EvmiDatabase, name string, instanceID uint) (uint, error) {
	if name == "" {
		return 0, errors.New("a pipeline reference is required")
	}
	var row evmi_database.EvmLogPipeline
	if err := db.Conn.Where("name = ? AND evmi_instance_id = ?", name, instanceID).First(&row).Error; err != nil {
		return 0, fmt.Errorf("pipeline %q not found: %w", name, err)
	}
	return row.ID, nil
}

func pluginIDByName(db *evmi_database.EvmiDatabase, name string) (uint, error) {
	if name == "" {
		return 0, errors.New("a plugin reference is required")
	}
	var row evmi_database.Plugin
	if err := db.Conn.Where("name = ?", name).First(&row).Error; err != nil {
		return 0, fmt.Errorf("plugin %q not found: %w", name, err)
	}
	return row.ID, nil
}
