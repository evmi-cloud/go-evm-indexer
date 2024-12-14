package hooks

import "github.com/evmi-cloud/go-evm-indexer/internal/types"

type EvmIndexerHook interface {
	Init(config types.Config, index uint64) error
	PublishNewLogs(logs []types.EvmLog) error
}
