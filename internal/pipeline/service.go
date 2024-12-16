package pipeline

import (
	"context"

	"github.com/evmi-cloud/go-evm-indexer/internal/database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/mustafaturan/bus/v3"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/thejerf/suture/v4"
)

type PipelineService struct {
	db         *database.IndexerDatabase
	bus        *bus.Bus
	supervisor *suture.Supervisor
	metrics    *metrics.MetricService

	abiPath string

	pipelineIdToServiceId map[string]suture.ServiceToken
	pipelines             map[string]*IndexationPipeline

	logger zerolog.Logger
	config types.IndexerConfig
	cron   *cron.Cron
}

func (s *PipelineService) Start() error {

	db, err := s.db.GetStoreDatabase()
	if err != nil {
		return err
	}

	stores, err := db.GetStores()
	if err != nil {
		return err
	}

	s.pipelines = make(map[string]*IndexationPipeline)
	s.pipelineIdToServiceId = make(map[string]suture.ServiceToken)

	for _, store := range stores {
		s.logger.Info().Msg("starting " + store.Id + " pipeline")
		s.pipelines[store.Id] = NewPipeline(s.db, s.bus, s.metrics, store.Id, s.abiPath, s.config)
		serviceToken := s.supervisor.Add(s.pipelines[store.Id])
		s.pipelineIdToServiceId[store.Id] = serviceToken
	}

	dbSize, err := db.GetDatabaseDiskSize()
	if err != nil {
		s.logger.Error().Msg(err.Error())
		return err
	}

	s.metrics.StoreDiskSizeMetricsSet(dbSize)

	s.cron = cron.New()
	s.cron.AddFunc("0 * * * * *", func() {
		db, _ := s.db.GetStoreDatabase()
		dbSize, _ := db.GetDatabaseDiskSize()
		s.metrics.StoreDiskSizeMetricsSet(dbSize)
	})

	s.cron.Start()

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
	db *database.IndexerDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	abiPath string,
	logger zerolog.Logger,
	config types.IndexerConfig,
) *PipelineService {

	/**
	* start supervizor
	 */
	supervisor := suture.NewSimple("Indexation service supervisor")

	return &PipelineService{
		db:         db,
		bus:        bus,
		metrics:    metrics,
		supervisor: supervisor,
		logger:     logger,
		abiPath:    abiPath,
		config:     config,
	}
}
