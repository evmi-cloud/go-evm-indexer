package exporter

// Host is the EVMI server as a plugin sees it: a small set of indexer functions
// a plugin can call back into while it runs. It is the reverse direction of the
// Exporter interface — EVMI calls the plugin through Exporter, the plugin calls
// EVMI through Host.
//
// A plugin receives it on Context.Host at Init and may keep it for the whole run;
// calls are safe from NewLogEvent. Every call is scoped to the calling exporter's
// pipeline: a plugin cannot read or touch another pipeline's topology.
//
// Host is nil when the server is older than this SDK and exposes no callback
// service, so a plugin that uses it should check once in Init:
//
//	func (e *myExporter) Init(ctx exporter.Context) error {
//	    if ctx.Host == nil {
//	        return errors.New("this plugin requires an EVMI server with host API support")
//	    }
//	    e.host = ctx.Host
//	    ...
//	}
type Host interface {
	// Blockchain returns the chain this exporter's pipeline indexes, including
	// the JSON-RPC endpoint the indexer polls. Use RpcUrl to open your own
	// client (ethclient.Dial) when a log does not carry everything you need —
	// e.g. to call a getter on a contract, or read a block header.
	//
	// The endpoint is shared with the indexer's own polling, so keep call volume
	// modest: hammering it slows indexing on the same node.
	Blockchain() (Blockchain, error)

	// CreateLogSource registers a new contract to index as a child of an
	// existing source. Use it when a deployment cannot be caught by the built-in
	// FACTORY rules — typically because the creation event does not carry the new
	// address, so you have to resolve it yourself (a receipt, a getter call, an
	// off-chain lookup) before you know what to index.
	//
	// The new source is created enabled and starts immediately, exactly like a
	// factory-spawned child: it shares the parent's pipeline, store and chain, and
	// nests under the parent in the web UI.
	//
	// It is idempotent per (Parent, Address): calling it again for a contract
	// already registered under that parent returns the existing source and
	// created=false. That matters because log delivery is at-least-once — a
	// plugin re-seeing a log after a restart must not create duplicates.
	CreateLogSource(src NewLogSource) (SourceRef, error)

	// UpsertAbi registers an ABI under a contract name if no ABI with that name
	// exists yet, and returns its id either way. Call it from Init to make sure
	// the ABIs your sources need are present, then use the returned id in
	// NewLogSource.AbiId.
	//
	// It never overwrites an existing ABI: if the name is taken, the stored
	// content is kept and created=false is reported. Rename or edit ABIs through
	// the API/UI instead — silently rewriting one would change how every source
	// already using it decodes.
	UpsertAbi(contractName string, content string) (AbiRef, error)

	// GetAbi looks one ABI up by contract name. The bool reports whether it
	// exists.
	GetAbi(contractName string) (Abi, bool, error)

	// GetAbiByID looks one ABI up by id. The bool reports whether it exists.
	GetAbiByID(id uint64) (Abi, bool, error)

	// ListAbis returns every ABI registered on the server.
	ListAbis() ([]Abi, error)
}

// Blockchain describes the chain an exporter's pipeline indexes.
type Blockchain struct {
	// Id is the EvmBlockchain row id.
	Id uint64
	// ChainId is the EVM chain id (1 = mainnet).
	ChainId uint64
	Name    string
	// RpcUrl is the JSON-RPC endpoint the indexer polls for this chain.
	RpcUrl string

	BlockRange      uint64
	BlockSlice      uint64
	PullInterval    uint64
	RpcMaxBatchSize uint64
}

// NewLogSource describes a source to register through Host.CreateLogSource.
type NewLogSource struct {
	// Parent is the source the new contract descends from — normally the
	// LogEvent.SourceId of the log that announced the deployment, so the new
	// source nests under the contract that created it. Required, and must belong
	// to the exporter's pipeline.
	Parent uint64
	// Address of the contract to index. Required.
	Address string
	// Type is the kind of source to create: SourceContract (the default) or
	// SourceFactory when the new contract itself deploys others.
	Type SourceType
	// AbiId decodes the new source's logs. Get it from UpsertAbi or GetAbi.
	AbiId uint64
	// StartBlock is the first block to index — normally the block the deployment
	// happened in, so nothing is missed and nothing before it is replayed.
	StartBlock uint64
}

// SourceType is the kind of log source to create (mirrors LogSourceType).
type SourceType string

const (
	// SourceContract indexes one contract's logs.
	SourceContract SourceType = "CONTRACT"
	// SourceFactory indexes one contract's logs and spawns children from its
	// creation rules. A source created this way has no rules of its own yet, so
	// it behaves like a CONTRACT source until rules are added.
	SourceFactory SourceType = "FACTORY"
)

// SourceRef identifies a source returned by CreateLogSource.
type SourceRef struct {
	Id uint64
	// Created is false when the source already existed (idempotent re-call).
	Created bool
}

// AbiRef identifies an ABI returned by UpsertAbi.
type AbiRef struct {
	Id uint64
	// Created is false when an ABI with that contract name already existed.
	Created bool
}

// Abi is a registered contract ABI.
type Abi struct {
	Id           uint64
	ContractName string
	// Content is the JSON ABI.
	Content string
}
