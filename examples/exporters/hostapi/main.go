// Example EVMI exporter plugin using the host API.
//
// It shows the three things a plugin can ask the indexer to do (exporter.Host):
//
//  1. read the chain's JSON-RPC endpoint, to fetch data the logs don't carry;
//  2. register an ABI at startup, so a source created later can decode with it;
//  3. create a log source for a contract, as a child of the source whose log
//     announced it.
//
// The point of (3) is deployments the built-in FACTORY rules cannot catch. A
// FACTORY rule reads the new contract's address straight out of a decoded event
// argument; when the creation event does not carry the address, a rule has
// nothing to work with. A plugin can resolve it another way — a getter call, the
// transaction receipt, an off-chain registry — and then register the source.
//
// Build it like any other plugin:
//
//	go build -o hostapi ./examples/exporters/hostapi
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

// pluginConfig is decoded from the exporter's PluginConfig JSON.
type pluginConfig struct {
	// ContractName is the name to register the child ABI under.
	ContractName string `json:"contractName"`
	// Abi is the JSON ABI new sources decode with.
	Abi string `json:"abi"`
	// CreateOn is the decoded event name that signals a deployment.
	CreateOn string `json:"createOn"`
	// AddressArg is the decoded event argument holding the new contract address.
	// Leave it empty to model the hard case: the event does not carry the
	// address, so resolveAddress has to work it out.
	AddressArg string `json:"addressArg"`
}

type hostAPIExporter struct {
	cfg  pluginConfig
	host exporter.Host

	// abiID is the ABI every source this plugin creates decodes with, resolved
	// once at startup.
	abiID uint64
	// rpcURL is the endpoint the indexer polls; use it to open a client when a
	// log alone is not enough.
	rpcURL string

	created int
}

func (e *hostAPIExporter) Name() string { return "hostapi" }

func (e *hostAPIExporter) ConfigSchema() []exporter.ConfigField {
	return []exporter.ConfigField{
		{Name: "contractName", Type: exporter.StringField, Required: true, Description: "ABI name for contracts this plugin registers"},
		{Name: "abi", Type: exporter.StringField, Required: true, Description: "JSON ABI new sources decode with"},
		{Name: "createOn", Type: exporter.StringField, Required: true, Description: "Event name that signals a deployment"},
		{Name: "addressArg", Type: exporter.StringField, Description: "Event arg holding the new address (empty = resolve it another way)"},
	}
}

func (e *hostAPIExporter) Init(ctx exporter.Context) error {
	// Host is nil against a server older than the host API. A plugin that needs
	// it should say so plainly rather than failing later in a confusing way.
	if ctx.Host == nil {
		return errors.New("this plugin needs an EVMI server exposing the host API")
	}
	e.host = ctx.Host

	if len(ctx.Config) > 0 {
		if err := json.Unmarshal(ctx.Config, &e.cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}
	}
	if e.cfg.ContractName == "" || e.cfg.Abi == "" || e.cfg.CreateOn == "" {
		return errors.New("contractName, abi and createOn are required")
	}

	// (2) Make sure the ABI the new sources need exists, and keep its id. Upsert
	// is idempotent, so it is safe on every restart; it never overwrites an ABI
	// someone already registered under this name.
	ref, err := e.host.UpsertAbi(e.cfg.ContractName, e.cfg.Abi)
	if err != nil {
		return fmt.Errorf("registering abi %q: %w", e.cfg.ContractName, err)
	}
	e.abiID = ref.Id
	log.Printf("[hostapi] abi %q id=%d (newly created: %t)", e.cfg.ContractName, ref.Id, ref.Created)

	// (1) The chain the pipeline indexes, including the endpoint the indexer
	// itself polls. Open your own client against it when needed:
	//
	//	client, err := ethclient.Dial(chain.RpcUrl)
	//
	// It is the same node the indexer uses, so keep the call volume modest.
	chain, err := e.host.Blockchain()
	if err != nil {
		return fmt.Errorf("reading blockchain: %w", err)
	}
	e.rpcURL = chain.RpcUrl
	log.Printf("[hostapi] chain %s (id=%d) rpc=%s", chain.Name, chain.ChainId, chain.RpcUrl)

	return nil
}

func (e *hostAPIExporter) NewLogEvent(l exporter.LogEvent) error {
	if l.EventName != e.cfg.CreateOn {
		return nil
	}

	address, err := e.resolveAddress(l)
	if err != nil {
		return err
	}
	if address == "" {
		log.Printf("[hostapi] %s at block %d: no address resolved, skipping", l.EventName, l.BlockNumber)
		return nil
	}

	// (3) Register the contract as a child of the source that announced it, so it
	// nests under that source in the UI exactly like a factory-spawned child.
	//
	// Idempotent per (parent, address): delivery is at-least-once, so this same
	// log may arrive again after a restart. Re-registering is a no-op that
	// returns the existing source.
	ref, err := e.host.CreateLogSource(exporter.NewLogSource{
		Parent:     uint64(l.SourceId),
		Address:    address,
		Type:       exporter.SourceContract,
		AbiId:      e.abiID,
		StartBlock: l.BlockNumber,
	})
	if err != nil {
		// Returning the error stops the exporter without advancing its cursor, so
		// the deployment is retried rather than silently dropped.
		return fmt.Errorf("creating source for %s: %w", address, err)
	}

	if ref.Created {
		e.created++
		log.Printf("[hostapi] indexing %s from block %d (source %d)", address, l.BlockNumber, ref.Id)
	}
	return nil
}

// resolveAddress works out the address of the newly deployed contract.
//
// When the creation event carries it, read it straight off the decoded args —
// though in that case the built-in FACTORY rules would handle it too, without a
// plugin. The interesting branch is the other one: the event does not carry the
// address, and this is where a plugin earns its place. Using e.rpcURL you can
// open an ethclient and call a getter on the factory, walk the transaction
// receipt for the creation, or consult something off-chain entirely.
func (e *hostAPIExporter) resolveAddress(l exporter.LogEvent) (string, error) {
	if e.cfg.AddressArg != "" {
		return l.Args[e.cfg.AddressArg], nil
	}

	// Stand-in for the real lookup, which depends on the contract. For example:
	//
	//	client, err := ethclient.Dial(e.rpcURL)
	//	if err != nil { return "", err }
	//	defer client.Close()
	//	receipt, err := client.TransactionReceipt(context.Background(), common.HexToHash(l.TransactionHash))
	//	// → receipt.ContractAddress, or scan receipt.Logs for the deployment
	//
	// Here we fall back to the emitting contract so the example stays runnable
	// without a node.
	return l.Address, nil
}

func (e *hostAPIExporter) Close() error {
	log.Printf("[hostapi] closing, registered %d source(s)", e.created)
	return nil
}

func main() {
	exporter.Serve(&hostAPIExporter{})
}
