package clover

type CloverLogStore struct {
	Id          string `clover:"_id"`
	Identifier  string `clover:"identifier" json:"identifier"`
	Description string `clover:"description" json:"description"`
	Status      string `clover:"status" json:"status"`
	ChainId     uint64 `clover:"chainId" json:"chainId"`
	Rpc         string `clover:"rpc" json:"rpc"`

	LatestChainBlock uint64 `clover:"latestChainBlock" json:"latestBlockIndexed"`
}

type CloverLogSource struct {
	Id            string                    `clover:"_id"`
	LogStoreId    string                    `clover:"storeId"`
	Name          string                    `clover:"name"`
	Type          string                    `clover:"type"`
	Contracts     []CloverLogSourceContract `clover:"contracts"`
	Topic         string                    `clover:"topic"`
	IndexedTopics []string                  `clover:"indexedTopics"`
	StartBlock    uint64                    `clover:"startBlock"`
	BlockRange    uint64                    `clover:"blockRange"`

	LatestBlockIndexed uint64 `clover:"latestBlockIndexed" json:"latestBlockIndexed"`
}

type CloverLogSourceContract struct {
	ContractName string `clover:"contractName"`
	Address      string `clover:"address"`
}

type CloverEvmMetadata struct {
	ContractName string            `clover:"contractName"`
	EventName    string            `clover:"eventName"`
	FunctionName string            `clover:"functionName"`
	Data         map[string]string `clover:"data"`
}

type CloverEvmLog struct {
	Id               string   `clover:"_id"`
	StoreId          string   `clover:"storeId"`
	SourceId         string   `clover:"sourceId"`
	Address          string   `clover:"address"`
	Topics           []string `clover:"topics"`
	Data             string   `clover:"data"`
	BlockNumber      uint64   `clover:"blockNumber"`
	TransactionHash  string   `clover:"transactionHash"`
	TransactionIndex uint64   `clover:"transactionIndex"`
	BlockHash        string   `clover:"blockHash"`
	LogIndex         uint64   `clover:"logIndex"`
	Removed          bool     `clover:"removed"`
	MintedAt         uint64   `clover:"mintedAt"`

	Metadata CloverEvmMetadata `clover:"metadata"`
}

type CloverEvmTransaction struct {
	Id          string `clover:"_id"`
	StoreId     string `clover:"storeId"`
	SourceId    string `clover:"sourceId"`
	BlockNumber uint64 `clover:"blockNumber"`
	ChainId     uint64 `clover:"chainId"`
	From        string `clover:"from"`
	Data        string `clover:"data"`
	Value       string `clover:"value"`
	Nonce       uint64 `clover:"nonce"`
	To          string `clover:"to"`
	Hash        string `clover:"hash"`
	MintedAt    uint64 `clover:"mintedAt"`

	Metadata CloverEvmMetadata `clover:"metadata"`
}
