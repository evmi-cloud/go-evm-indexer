package exporter

import (
	"context"
	"errors"
	"fmt"
	"time"

	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/google/uuid"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
	"github.com/thejerf/suture/v4"
)

type ExporterServiceManager struct {
	instanceId string
	db         *evmi_database.EvmiDatabase
	bus        *bus.Bus
	supervisor *suture.Supervisor
	metrics    *metrics.MetricService

	exporterIdToServiceId map[uint]suture.ServiceToken
	exporterServices      map[uint]*ExporterService

	logger zerolog.Logger
}

func (s *ExporterServiceManager) Start() error {

	s.exporterServices = make(map[uint]*ExporterService)
	s.exporterIdToServiceId = make(map[uint]suture.ServiceToken)

	var evmInstance evmi_database.EvmiInstance
	result := s.db.Conn.Model(&evmi_database.EvmiInstance{}).Where("instance_id = ?", s.instanceId).First(&evmInstance)
	if result.Error != nil {
		return result.Error
	}

	var pipelines []evmi_database.EvmLogPipeline
	result = s.db.Conn.Model(&evmi_database.EvmLogPipeline{}).Where("evmi_instance_id = ?", evmInstance.ID).Find(&pipelines)
	if result.Error != nil {
		return result.Error
	}

	s.logger.Info().Msg("instanceId: " + s.instanceId)
	s.logger.Info().Msg("pipeline founds: " + fmt.Sprint(len(pipelines)))

	for _, pipeline := range pipelines {
		s.logger.Info().Msg("check source of pipeline id " + fmt.Sprint(pipeline.ID))
		var sources []evmi_database.EvmLogSource
		result := s.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("evm_log_pipeline_id = ?", pipeline.ID).Find(&sources)
		if result.Error != nil {
			return result.Error
		}

		for _, source := range sources {
			s.logger.Info().Msg("check if enabled on source id " + fmt.Sprint(source.ID))
			if source.Enabled {
				s.logger.Info().Msg("starting source id " + fmt.Sprint(source.ID))
				s.sourceIndexers[source.ID] = NewExporterService(s.db, s.bus, s.metrics, source)

				serviceToken := s.supervisor.Add(s.sourceIndexers[source.ID])
				s.sourceIdToServiceId[source.ID] = serviceToken
			}
		}
	}

	enableSourceHandlerId := uuid.New()
	s.bus.RegisterHandler(enableSourceHandlerId.String(), bus.Handler{
		Handle: func(ctx context.Context, e bus.Event) {
			sourceId := e.Data.(uint)
			s.EnableSource(sourceId)
		},
		Matcher: internal_bus.EnableSourceTopic,
	})

	disableSourceHandlerId := uuid.New()
	s.bus.RegisterHandler(disableSourceHandlerId.String(), bus.Handler{
		Handle: func(ctx context.Context, e bus.Event) {
			sourceId := e.Data.(uint)
			s.DisableSource(sourceId)
		},
		Matcher: internal_bus.DisableSourceTopic,
	})

	s.supervisor.ServeBackground(context.Background())
	return nil
}

func (s *ExporterService) EnableSource(sourceId uint) error {

	var source evmi_database.EvmLogSource
	result := s.db.Conn.First(&source, sourceId)
	if result.Error != nil {
		return result.Error
	}

	source.Enabled = true

	result = s.db.Conn.Save(&source)
	if result.Error != nil {
		return result.Error
	}

	_, ok := s.sourceIndexers[sourceId]
	if ok {
		return errors.New("service already running")
	}

	s.logger.Info().Msg("starting source id " + fmt.Sprint(source.ID))
	s.sourceIndexers[source.ID] = NewExporterService(s.db, s.bus, s.metrics, source)

	serviceToken := s.supervisor.Add(s.sourceIndexers[source.ID])
	s.sourceIdToServiceId[source.ID] = serviceToken

	source.Status = string(evmi_database.StoppedLogSourceStatus)
	result = s.db.Conn.Save(&source)
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func (s *ExporterService) DisableSource(sourceId uint) error {

	var source evmi_database.EvmLogSource
	result := s.db.Conn.First(&source, sourceId)
	if result.Error != nil {
		return result.Error
	}

	source.Enabled = false
	result = s.db.Conn.Save(&source)
	if result.Error != nil {
		return result.Error
	}

	_, ok := s.sourceIndexers[sourceId]
	if !ok {
		return errors.New("service already stoped")
	}

	s.logger.Info().Msg("disable source id " + fmt.Sprint(source.ID))
	s.supervisor.RemoveAndWait(s.sourceIdToServiceId[source.ID], time.Minute)

	source.Status = "STOPPED"
	result = s.db.Conn.Save(&source)
	if result.Error != nil {
		return result.Error
	}

	delete(s.sourceIdToServiceId, source.ID)
	return nil
}

func NewExporterServiceManager(
	instanceId string,
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	logger zerolog.Logger,
) *ExporterServiceManager {

	/**
	* start supervizor
	 */
	supervisor := suture.NewSimple("Indexation service supervisor")

	return &ExporterServiceManager{
		instanceId: instanceId,
		db:         db,
		bus:        bus,
		metrics:    metrics,
		supervisor: supervisor,
		logger:     logger,
	}
}
