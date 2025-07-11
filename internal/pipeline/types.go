package pipeline

import evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"

type ContractFromFactory struct {
	Type             evmi_database.LogSourceType
	StartBlock       uint64
	Address          string
	EvmLogPipelineID uint
	EvmJsonAbiID     uint
}
