package evmi_database

import (
	"database/sql"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type LogSourceType string

const (
	ContractLogSourceType LogSourceType = "CONTRACT"
	TopicLogSourceType    LogSourceType = "TOPIC"
	FactoryLogSourceType  LogSourceType = "FACTORY"
)

type EvmiInstance struct {
	gorm.Model

	InstanceId uint64
	IpV4       string
	Status     string
}

type EvmBlockchain struct {
	gorm.Model
	ChainId uint64
	Name    string
	RpcUrl  string

	BlockRange      uint64
	BlockSlice      uint64
	PullInterval    uint64
	RpcMaxBatchSize uint64
}

type EvmJsonAbi struct {
	gorm.Model

	ContractName string
	Content      string
}

type EvmLogStore struct {
	gorm.Model

	Identifier  string
	Description string

	StoreType   string
	StoreConfig datatypes.JSON

	Pipelines []EvmLogPipeline
}

type EvmLogPipeline struct {
	gorm.Model

	Name       string
	Type       string
	LogSources []EvmLogSource

	EvmiInstanceID  uint
	EvmBlockchainID uint
	EvmLogStoreId   uint
}

type EvmLogSource struct {
	gorm.Model

	Type LogSourceType

	StartBlock uint64
	SyncBlock  uint64

	// Contract type data
	Address sql.NullString

	// Topic type data
	Topic0       sql.NullString
	TopicFilters pq.StringArray `gorm:"type:text[]"`

	// Factory type data
	FactoryChildEvmJsonABI       sql.NullInt32
	FactoryCreationFunctionName  sql.NullString
	FactoryCreationAddressLogArg sql.NullString

	EvmLogPipelineID uint
	EvmJsonAbiID     uint
}
