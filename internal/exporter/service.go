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

// ExporterServiceManager owns the lifecycle of every exporter bound to a
// pipeline of this instance, supervising each as an independent restartable
// service and reacting to enable/disable events on the bus.
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

func NewExporterServiceManager(
	instanceId string,
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	logger zerolog.Logger,
) *ExporterServiceManager {

	supervisor := suture.NewSimple("Exporter service supervisor")

	return &ExporterServiceManager{
		instanceId: instanceId,
		db:         db,
		bus:        bus,
		metrics:    metrics,
		supervisor: supervisor,
		logger:     logger,
	}
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

	s.logger.Info().Msg("exporter manager instanceId: " + s.instanceId)

	for _, pipeline := range pipelines {
		var exporters []evmi_database.EvmiExporter
		result := s.db.Conn.Model(&evmi_database.EvmiExporter{}).Where("evm_log_pipeline_id = ?", pipeline.ID).Find(&exporters)
		if result.Error != nil {
			return result.Error
		}

		for _, exp := range exporters {
			if exp.Enabled {
				s.startExporter(exp)
			}
		}
	}

	enableHandlerId := uuid.New()
	s.bus.RegisterHandler(enableHandlerId.String(), bus.Handler{
		Handle: func(ctx context.Context, e bus.Event) {
			exporterId := e.Data.(uint)
			if err := s.EnableExporter(exporterId); err != nil {
				s.logger.Error().Msg(err.Error())
			}
		},
		Matcher: internal_bus.EnableExporterTopic,
	})

	disableHandlerId := uuid.New()
	s.bus.RegisterHandler(disableHandlerId.String(), bus.Handler{
		Handle: func(ctx context.Context, e bus.Event) {
			exporterId := e.Data.(uint)
			if err := s.DisableExporter(exporterId); err != nil {
				s.logger.Error().Msg(err.Error())
			}
		},
		Matcher: internal_bus.DisableExporterTopic,
	})

	s.supervisor.ServeBackground(context.Background())
	return nil
}

func (s *ExporterServiceManager) startExporter(exp evmi_database.EvmiExporter) {
	s.logger.Info().Msg("starting exporter id " + fmt.Sprint(exp.ID))
	service := NewExporterService(s.db, s.metrics, exp)
	s.exporterServices[exp.ID] = service
	s.exporterIdToServiceId[exp.ID] = s.supervisor.Add(service)
}

func (s *ExporterServiceManager) EnableExporter(exporterId uint) error {
	var exp evmi_database.EvmiExporter
	if result := s.db.Conn.First(&exp, exporterId); result.Error != nil {
		return result.Error
	}

	if _, ok := s.exporterServices[exporterId]; ok {
		return errors.New("exporter already running")
	}

	exp.Enabled = true
	if result := s.db.Conn.Save(&exp); result.Error != nil {
		return result.Error
	}

	s.startExporter(exp)
	return nil
}

func (s *ExporterServiceManager) DisableExporter(exporterId uint) error {
	var exp evmi_database.EvmiExporter
	if result := s.db.Conn.First(&exp, exporterId); result.Error != nil {
		return result.Error
	}

	token, ok := s.exporterIdToServiceId[exporterId]
	if !ok {
		return errors.New("exporter already stopped")
	}

	exp.Enabled = false
	if result := s.db.Conn.Save(&exp); result.Error != nil {
		return result.Error
	}

	s.logger.Info().Msg("disabling exporter id " + fmt.Sprint(exp.ID))
	s.supervisor.RemoveAndWait(token, time.Minute)

	exp.Status = string(evmi_database.StoppedExporterStatus)
	if result := s.db.Conn.Save(&exp); result.Error != nil {
		return result.Error
	}

	delete(s.exporterIdToServiceId, exp.ID)
	delete(s.exporterServices, exp.ID)
	return nil
}
