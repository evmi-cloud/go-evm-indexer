package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
	"github.com/thejerf/suture/v4"
)

type IndexationPipeline struct {
	db      *evmi_database.EvmiDatabase
	bus     *bus.Bus
	metrics *metrics.MetricService
	store   *log_stores.IndexerStore

	pipeline  evmi_database.EvmLogPipeline
	chain     evmi_database.EvmBlockchain
	storeInfo evmi_database.EvmLogStore

	subprocessIdToServiceToken map[uint]suture.ServiceToken
	subProcesses               map[uint]*PipelineIndexerSubprocess
	supervisor                 *suture.Supervisor

	logger  zerolog.Logger
	running bool
}

func NewPipeline(db *evmi_database.EvmiDatabase, bus *bus.Bus, metrics *metrics.MetricService, pipeline evmi_database.EvmLogPipeline) *IndexationPipeline {

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
		pipeline:   pipeline,
	}
}

func (p *IndexationPipeline) Serve(ctx context.Context) error {
	if !p.running {

		p.logger.Info().Msg("starting pipeline " + p.pipeline.Name)

		err := p.Init()
		if err != nil {
			p.logger.Error().Msg(err.Error())
			return err
		}

		p.logger.Info().Msg(p.pipeline.Name + " pipeline initialized")

		for processId, process := range p.subProcesses {
			p.subprocessIdToServiceToken[processId] = p.supervisor.Add(process)
		}

		p.logger.Info().Msg(fmt.Sprint(len(p.subProcesses)) + " subprocesses of pipeline " + p.pipeline.Name + " added to supervisor")

		p.running = true
		p.supervisor.Serve(ctx)
	} else {
		return errors.New(p.pipeline.Name + " pipeline already initialized")
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

func (p *IndexationPipeline) Init() error {
	if p.running {
		return errors.New("pipeline is running")
	} else {
		p.logger.Info().Msg("Loading pipeline " + p.pipeline.Name)

		result := p.db.Conn.First(&p.chain, p.pipeline.EvmBlockchainID)
		if result.Error != nil {
			return result.Error
		}

		var storeInfo evmi_database.EvmLogStore
		result = p.db.Conn.First(&storeInfo, p.pipeline.EvmLogStoreId)
		if result.Error != nil {
			return result.Error
		}

		var storeConfig map[string]string
		err := storeInfo.StoreConfig.Scan(&storeConfig)
		if err != nil {
			return err
		}

		store, err := log_stores.LoadStore(storeInfo.StoreType, storeConfig, p.logger)
		if err != nil {
			return err
		}

		p.store = store

		var sources []evmi_database.EvmLogSource
		result = p.db.Conn.Model(&evmi_database.EvmLogSource{}).Where("evm_log_pipeline_id = ?", p.pipeline.ID).Find(&sources)
		if result.Error != nil {
			return result.Error
		}

		//init gauge metrics
		logCount, err := store.GetStorage().GetLogsCount()
		if err != nil {
			return err
		}

		p.metrics.LogsCountMetricsSet(storeInfo.Identifier, logCount)

		p.logger.Info().Msg("sources founds for pipeline: " + fmt.Sprint(len(sources)))

		p.subProcesses = make(map[uint]*PipelineIndexerSubprocess)
		p.subprocessIdToServiceToken = make(map[uint]suture.ServiceToken)

		for _, source := range sources {

			var evmAbi evmi_database.EvmJsonAbi
			result = p.db.Conn.Model(&evmi_database.EvmJsonAbi{}).First(&evmAbi, source.EvmJsonAbiID)
			if result.Error != nil {
				return result.Error
			}

			contractAbi, err := abi.JSON(bytes.NewReader([]byte(evmAbi.Content)))
			if err != nil {
				return err
			}

			p.subProcesses[source.ID] = NewPipelineIndexer(
				p.db,
				p.bus,
				p.metrics,
				p.store,
				source,
				p.pipeline,
				p.chain,
				p.storeInfo,
				evmAbi.ContractName,
				contractAbi,
			)
		}

		return nil
	}
}
