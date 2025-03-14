package types

type PipelineStatus string
type PipelineConfigType string

const (
	PipelineRunning PipelineStatus = "RUNNING"
	PipelineStopped PipelineStatus = "STOPPED"

	StaticPipelineConfigType PipelineConfigType = "STATIC"
	TopicPipelineConfigType  PipelineConfigType = "TOPIC"
)

type LogStore struct {
	Id          string
	Identifier  string
	Description string
	Status      PipelineStatus
	ChainId     uint64
	Rpc         string
}

type LogSource struct {
	Id         string
	LogStoreId string
	Name       string
	Type       PipelineConfigType
	Contracts  []struct {
		ContractName string
		Address      string
	}
	Topic         string
	IndexedTopics []string
	StartBlock    uint64
	BlockRange    uint64

	LatestBlockIndexed uint64
}
