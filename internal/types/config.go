package types

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
