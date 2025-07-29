package evmi_database

import (
	"database/sql"
	"database/sql/driver"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type LogSourceType string

const (
	FullLogSourceType     LogSourceType = "FULL"
	ContractLogSourceType LogSourceType = "CONTRACT"
	TopicLogSourceType    LogSourceType = "TOPIC"
	FactoryLogSourceType  LogSourceType = "FACTORY"
)

func (ct *LogSourceType) Scan(value interface{}) error {
	*ct = LogSourceType(value.(string))
	return nil
}

func (ct LogSourceType) Value() (driver.Value, error) {
	return string(ct), nil
}

type LogSourceStatus string

const (
	RunningLogSourceStatus     LogSourceStatus = "RUNNING"
	LoopbackOffLogSourceStatus LogSourceStatus = "LOOPBACKOFF"
	StoppedLogSourceStatus     LogSourceStatus = "STOPPED"
)

func (ct *LogSourceStatus) Scan(value interface{}) error {
	*ct = LogSourceStatus(value.(string))
	return nil
}

func (ct LogSourceStatus) Value() (driver.Value, error) {
	return string(ct), nil
}

type EvmiInstance struct {
	gorm.Model

	InstanceId string
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
	LogSources []EvmLogSource

	EvmiInstanceID  uint
	EvmBlockchainID uint
	EvmLogStoreId   uint
}

type EvmLogSource struct {
	gorm.Model

	Enabled bool
	Status  string
	Type    string

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
	EvmBlockchainID  uint
}
