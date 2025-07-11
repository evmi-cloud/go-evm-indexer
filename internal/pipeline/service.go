package pipeline

import (
	"context"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
	"github.com/thejerf/suture/v4"
)

type PipelineService struct {
	instanceId string
	db         *evmi_database.EvmiDatabase
	bus        *bus.Bus
	supervisor *suture.Supervisor
	metrics    *metrics.MetricService

	pipelineIdToServiceId map[uint]suture.ServiceToken
	pipelines             map[uint]*IndexationPipeline

	logger zerolog.Logger
}

func (s *PipelineService) Start() error {

	s.pipelines = make(map[uint]*IndexationPipeline)
	s.pipelineIdToServiceId = make(map[uint]suture.ServiceToken)

	var pipelines []evmi_database.EvmLogPipeline
	result := s.db.Conn.Model(&evmi_database.EvmLogPipeline{}).Where("evmi_instance_id = ?", s.instanceId).Find(&pipelines)
	if result.Error != nil {
		return result.Error
	}

	for _, pipeline := range pipelines {
		s.logger.Info().Msg("starting " + pipeline.Name + " pipeline")
		s.pipelines[pipeline.ID] = NewPipeline(s.db, s.bus, s.metrics, pipeline)

		serviceToken := s.supervisor.Add(s.pipelines[pipeline.ID])
		s.pipelineIdToServiceId[pipeline.ID] = serviceToken
	}

	s.supervisor.ServeBackground(context.Background())
	return nil
}

// func (s *PipelineService) startPipeline(pipelineId uint64) error {

// 	return nil
// }

// func (s *PipelineService) stopPipeline(pipelineId uint64) error {

// 	return nil
// }

func NewPipelineService(
	instanceId string,
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	logger zerolog.Logger,
) *PipelineService {

	/**
	* start supervizor
	 */
	supervisor := suture.NewSimple("Indexation service supervisor")

	return &PipelineService{
		instanceId: instanceId,
		db:         db,
		bus:        bus,
		metrics:    metrics,
		supervisor: supervisor,
		logger:     logger,
	}
}
