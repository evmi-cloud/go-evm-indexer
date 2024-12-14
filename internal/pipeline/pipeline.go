package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/evmi-cloud/go-evm-indexer/internal/database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
	"github.com/thejerf/suture/v4"
)

type IndexationPipeline struct {
	db      *database.IndexerDatabase
	bus     *bus.Bus
	metrics *metrics.MetricService

	rpc string

	abiPath string

	subprocessIdToServiceToken map[string]suture.ServiceToken
	subProcesses               map[string]*IndexationPipelineSubprocess
	supervisor                 *suture.Supervisor

	logger zerolog.Logger

	storeId string
	config  types.IndexerConfig
	running bool
}

func NewPipeline(db *database.IndexerDatabase, bus *bus.Bus, metrics *metrics.MetricService, storeId string, abiPath string, config types.IndexerConfig) *IndexationPipeline {

	logger := zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	supervisor := suture.NewSimple("Indexation pipeline supervisor")

	return &IndexationPipeline{
		db:         db,
		bus:        bus,
		metrics:    metrics,
		logger:     logger,
		running:    false,
		supervisor: supervisor,
		storeId:    storeId,
		abiPath:    abiPath,
		config:     config,
	}
}

func (p *IndexationPipeline) Serve(ctx context.Context) error {
	if !p.running {

		p.logger.Info().Msg("starting pipeline " + p.storeId)
		err := p.LoadFromDB()
		if err != nil {
			p.logger.Error().Msg(err.Error())
			return err
		}

		p.logger.Info().Msg("database loaded for pipeline " + p.storeId)

		for processId, process := range p.subProcesses {
			p.subprocessIdToServiceToken[processId] = p.supervisor.Add(process)
		}

		p.logger.Info().Msg(fmt.Sprint(len(p.subProcesses)) + " subprocesses of pipeline " + p.storeId + " added to supervisor")

		p.running = true
		p.supervisor.Serve(ctx)
	} else {
		return errors.New("already running")
	}

	return nil
}

func (p *IndexationPipeline) Stop() error {
	if p.running {

		for subprocessId, process := range p.subProcesses {
			process.Stop()
			for !process.IsEnded() {
				time.Sleep(500 * time.Millisecond)
			}

			err := p.supervisor.Remove(p.subprocessIdToServiceToken[subprocessId])
			if err != nil {
				return err
			}

			delete(p.subprocessIdToServiceToken, subprocessId)
		}

		p.running = false

		return nil
	} else {
		return nil
	}
}

func (p *IndexationPipeline) LoadFromDB() error {
	if p.running {
		return errors.New("pipeline is running")
	} else {

		db, err := p.db.GetStoreDatabase()
		if err != nil {
			return err
		}

		p.logger.Info().Msg("Loading pipeline from db: " + p.storeId)
		store, err := db.GetStoreById(p.storeId)
		if err != nil {
			return err
		}

		sources, err := db.GetSources(store.Id)
		if err != nil {
			return err
		}

		//init gauge metrics
		logCount, err := db.GetLogsByStoreCount(store.Id)
		p.metrics.LogsCountMetricsSet(store.Identifier, logCount)

		p.logger.Info().Msg("Sources founds for pipeline: " + fmt.Sprint(len(sources)))

		p.rpc = store.Rpc
		p.subProcesses = make(map[string]*IndexationPipelineSubprocess)
		p.subprocessIdToServiceToken = make(map[string]suture.ServiceToken)

		for _, source := range sources {
			p.subProcesses[source.Id] = NewPipelineSubrocess(
				p.db,
				p.bus,
				p.metrics,
				p.rpc,
				&source,
				&store,
				p.abiPath,
				p.config,
			)
		}

		return nil
	}
}
