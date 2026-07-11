package types

import "encoding/json"

type Config struct {
	Database struct {
		Type   string            `json:"type"`
		Config map[string]string `json:"config"`
	} `json:"database"`

	Metrics struct {
		Enabled bool   `json:"enabled"`
		Path    string `json:"path"`
		Port    uint64 `json:"port"`
	} `json:"metrics"`

	// Plugins are git repositories imported as Plugin rows on startup (created if
	// absent, matched by name) and installed.
	Plugins []ConfigPlugin `json:"plugins"`

	// Resources are metadata-DB rows (blockchains, ABIs, stores, pipelines,
	// sources, exporters) declared in the config and created on startup if they
	// don't already exist. See AutoloadResources.
	Resources AutoloadResources `json:"resources"`
}

type ConfigPlugin struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	GitUrl       string `json:"gitUrl"`
	RelativePath string `json:"relativePath"`
}

// AutoloadResources declares metadata-DB resources to create on startup with a
// "create if not exists" policy (idempotent — existing rows are left untouched).
// Cross-references between resources use the referenced resource's natural
// name/identifier (not its DB id, which isn't known ahead of time); the
// autoloader resolves those to ids, including references to rows already present
// in the DB (created on a previous boot or via the API). Resources are loaded in
// dependency order: blockchains, ABIs and stores first, then pipelines, then
// sources and exporters.
type AutoloadResources struct {
	Blockchains []ConfigBlockchain `json:"blockchains"`
	Abis        []ConfigAbi        `json:"abis"`
	Stores      []ConfigStore      `json:"stores"`
	Pipelines   []ConfigPipeline   `json:"pipelines"`
	Sources     []ConfigSource     `json:"sources"`
	Exporters   []ConfigExporter   `json:"exporters"`
}

// ConfigBlockchain matches an EvmBlockchain by Name.
type ConfigBlockchain struct {
	Name                string `json:"name"`
	ChainId             uint64 `json:"chainId"`
	RpcUrl              string `json:"rpcUrl"`
	BlockRange          uint64 `json:"blockRange"`
	BlockSlice          uint64 `json:"blockSlice"`
	PullInterval        uint64 `json:"pullInterval"`
	RpcMaxBatchSize     uint64 `json:"rpcMaxBatchSize"`
	SqdGatewayAvailable bool   `json:"sqdGatewayAvailable"`
	SqdGatewayUrl       string `json:"sqdGatewayUrl"`
}

// ConfigAbi matches an EvmJsonAbi by ContractName. Content is the ABI JSON.
type ConfigAbi struct {
	ContractName string `json:"contractName"`
	Content      string `json:"content"`
}

// ConfigStore matches an EvmLogStore by Identifier. StoreConfig is the
// backend-specific JSON config blob (see the log-store docs).
type ConfigStore struct {
	Identifier  string          `json:"identifier"`
	Description string          `json:"description"`
	StoreType   string          `json:"storeType"`
	StoreConfig json.RawMessage `json:"storeConfig"`
}

// ConfigPipeline matches an EvmLogPipeline by (Name, instance). Blockchain and
// Store reference a ConfigBlockchain.Name and ConfigStore.Identifier.
type ConfigPipeline struct {
	Name       string `json:"name"`
	Blockchain string `json:"blockchain"`
	Store      string `json:"store"`
}

// ConfigSource matches an EvmLogSource within its pipeline: for CONTRACT/FACTORY
// by Address, for TOPIC by Topic0, for FULL by (pipeline, type). Pipeline / Abi /
// FactoryChildAbi reference resources by name; Blockchain defaults to the
// pipeline's blockchain when empty.
type ConfigSource struct {
	Pipeline   string `json:"pipeline"`
	Blockchain string `json:"blockchain,omitempty"`
	Abi        string `json:"abi,omitempty"`
	Type       string `json:"type"`
	Enabled    bool   `json:"enabled"`
	StartBlock uint64 `json:"startBlock"`

	Address string `json:"address,omitempty"`

	Topic0       string   `json:"topic0,omitempty"`
	TopicFilters []string `json:"topicFilters,omitempty"`

	FactoryChildAbi              string `json:"factoryChildAbi,omitempty"`
	FactoryCreationFunctionName  string `json:"factoryCreationFunctionName,omitempty"`
	FactoryCreationAddressLogArg string `json:"factoryCreationAddressLogArg,omitempty"`
}

// ConfigExporter matches an EvmiExporter by (Name, pipeline). Pipeline and Plugin
// reference a ConfigPipeline.Name and a Plugin.Name (see Config.Plugins).
type ConfigExporter struct {
	Name         string          `json:"name"`
	Pipeline     string          `json:"pipeline"`
	Plugin       string          `json:"plugin"`
	Enabled      bool            `json:"enabled"`
	StartBlock   uint64          `json:"startBlock"`
	PluginConfig json.RawMessage `json:"pluginConfig,omitempty"`
}

type IndexerConfig struct {
	BlockSlice      uint64 `json:"blockSlice"`
	MaxBlockRange   uint64 `json:"maxBlockRange"`
	PullInterval    uint64 `json:"pullInterval"`
	RpcMaxBatchSize uint64 `json:"rpcMaxBatchSize"`
}

type ConfigLogStore struct {
	Identifier  string            `json:"identifier"`
	Description string            `json:"description"`
	ChainId     uint64            `json:"chainId"`
	Rpc         string            `json:"rpc"`
	Sources     []ConfigLogSource `json:"sources"`
}

type ConfigLogSource struct {
	Name      string             `json:"name"`
	Type      PipelineConfigType `json:"type"`
	Contracts []struct {
		ContractName string `json:"contractName"`
		Address      string `json:"address"`
	} `json:"contracts,omitempty"`
	Topic         string   `json:"topic,omitempty"`
	IndexedTopics []string `json:"indexedTopics,omitempty"`

	StartBlock uint64 `json:"startBlock"`
	BlockRange uint64 `json:"blockRange"`
}
