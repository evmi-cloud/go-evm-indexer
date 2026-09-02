package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/google/uuid"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
	"github.com/thejerf/suture/v4"
)

type IndexerService struct {
	instanceId string
	db         *evmi_database.EvmiDatabase
	bus        *bus.Bus
	supervisor *suture.Supervisor
	metrics    *metrics.MetricService

	// mu guards the service maps; bus handlers (source enable/disable, factory
	// discovery) can fire concurrently from different indexer goroutines.
	mu                  sync.Mutex
	sourceIdToServiceId map[uint]suture.ServiceToken
	sourceIndexers      map[uint]*SourceIndexerService
	// One head watcher per blockchain, shared by that chain's sources and
	// retired with its last source. Keyed by EvmBlockchain ID.
	heads      map[uint]*HeadWatcher
	headTokens map[uint]suture.ServiceToken

	logger zerolog.Logger
}

func (s *IndexerService) Start() error {

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
			if source.Enabled {
				s.startSource(source)
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

// startSource registers and supervises an indexer for source. Caller need not
// hold the lock; this takes it and dedupes so a source is started at most once.
func (s *IndexerService) startSource(source evmi_database.EvmLogSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sourceIndexers[source.ID]; ok {
		return
	}
	s.logger.Info().Msg("starting source id " + fmt.Sprint(source.ID))
	service := NewSourceIndexerService(s.db, s.bus, s.metrics, source)
	service.head = s.acquireHead(source.EvmBlockchainID)
	s.sourceIndexers[source.ID] = service
	s.sourceIdToServiceId[source.ID] = s.supervisor.Add(service)
}

func (s *IndexerService) EnableSource(sourceId uint) error {

	var source evmi_database.EvmLogSource
	result := s.db.Conn.First(&source, sourceId)
	if result.Error != nil {
		return result.Error
	}

	// Column-scoped update: the row is shared with a possibly-running worker
	// that owns sync_block, so a full-row Save would write this stale copy back.
	source.Enabled = true
	source.Status = string(evmi_database.StoppedLogSourceStatus)
	result = s.db.Conn.Model(&source).Updates(map[string]interface{}{
		"enabled": true,
		"status":  source.Status,
	})
	if result.Error != nil {
		return result.Error
	}

	s.startSource(source)
	return nil
}

func (s *IndexerService) DisableSource(sourceId uint) error {

	var source evmi_database.EvmLogSource
	result := s.db.Conn.First(&source, sourceId)
	if result.Error != nil {
		return result.Error
	}

	// Column-scoped update: the worker still owns sync_block until it is removed
	// below, so a full-row Save would write this stale copy back.
	source.Enabled = false
	result = s.db.Conn.Model(&source).Update("enabled", false)
	if result.Error != nil {
		return result.Error
	}

	s.mu.Lock()
	token, ok := s.sourceIdToServiceId[sourceId]
	var chainID uint
	if ok {
		chainID = s.sourceIndexers[sourceId].source.EvmBlockchainID
		delete(s.sourceIdToServiceId, sourceId)
		delete(s.sourceIndexers, sourceId)
	}
	s.mu.Unlock()
	if !ok {
		return errors.New("service already stopped")
	}

	s.logger.Info().Msg("disable source id " + fmt.Sprint(source.ID))
	s.supervisor.RemoveAndWait(token, time.Minute)
	s.releaseHead(chainID)

	source.Status = string(evmi_database.StoppedLogSourceStatus)
	if result := s.db.Conn.Model(&source).Update("status", source.Status); result.Error != nil {
		return result.Error
	}
	return nil
}

func NewIndexerService(
	instanceId string,
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	logger zerolog.Logger,
) *IndexerService {

	/**
	* start supervizor
	 */
	supervisor := suture.NewSimple("Indexation service supervisor")

	return &IndexerService{
		instanceId:          instanceId,
		db:                  db,
		bus:                 bus,
		metrics:             metrics,
		supervisor:          supervisor,
		sourceIndexers:      make(map[uint]*SourceIndexerService),
		sourceIdToServiceId: make(map[uint]suture.ServiceToken),
		heads:               make(map[uint]*HeadWatcher),
		headTokens:          make(map[uint]suture.ServiceToken),
		logger:              logger,
	}
}

// acquireHead returns the chain's shared head watcher, starting it under the
// supervisor on first use. Caller holds s.mu. A chain that cannot be loaded
// yields nil: the source then polls the head itself, as before.
func (s *IndexerService) acquireHead(chainID uint) *HeadWatcher {
	if w, ok := s.heads[chainID]; ok {
		w.refs.Add(1)
		return w
	}
	var chain evmi_database.EvmBlockchain
	if result := s.db.Conn.First(&chain, chainID); result.Error != nil {
		s.logger.Warn().Err(result.Error).Msg("head watcher: blockchain not loaded, source will poll on its own")
		return nil
	}
	w := NewHeadWatcher(chain, s.metrics, s.logger)
	w.refs.Store(1)
	s.heads[chainID] = w
	s.headTokens[chainID] = s.supervisor.Add(w)
	s.logger.Info().Msg("head watcher started for blockchain id " + fmt.Sprint(chainID))
	return w
}

// releaseHead drops one source's reference and retires the watcher with the
// last one. Caller must NOT hold s.mu.
func (s *IndexerService) releaseHead(chainID uint) {
	s.mu.Lock()
	w, ok := s.heads[chainID]
	if !ok || w.refs.Add(-1) > 0 {
		s.mu.Unlock()
		return
	}
	token := s.headTokens[chainID]
	delete(s.heads, chainID)
	delete(s.headTokens, chainID)
	s.mu.Unlock()
	s.supervisor.RemoveAndWait(token, time.Minute)
	s.logger.Info().Msg("head watcher stopped for blockchain id " + fmt.Sprint(chainID))
}
