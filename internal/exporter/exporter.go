package exporter

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
)

type ExporterService struct {
	db      *evmi_database.EvmiDatabase
	bus     *bus.Bus
	metrics *metrics.MetricService

	store *log_stores.IndexerStore

	pipeline     evmi_database.EvmLogPipeline
	storeInfo    evmi_database.EvmLogStore
	exporter     evmi_database.EvmiExporter
	contractName string
	abi          abi.ABI

	logger zerolog.Logger

	running bool
	ended   bool
}

func NewExporterService(
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	exporter evmi_database.EvmiExporter,
) *ExporterService {

	logger := zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	return &ExporterService{
		db:       db,
		bus:      bus,
		metrics:  metrics,
		exporter: exporter,
		logger:   logger,
		running:  false,
		ended:    true,
	}
}

func (p *ExporterService) Serve(ctx context.Context) error {
	p.running = true

	logParams := map[string]interface{}{
		"type": p.source.Type,
	}

	p.logger.Info().Fields(logParams).Msg("starting subprocess")
	result := p.db.Conn.First(&p.pipeline, p.exporter.EvmLogPipelineID)
	if result.Error != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("loading store")
	result = p.db.Conn.First(&p.storeInfo, p.pipeline.EvmLogStoreId)
	if result.Error != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("loading store config")
	var storeConfig map[string]string
	err := json.Unmarshal(p.storeInfo.StoreConfig, &storeConfig)
	if err != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("connecting store")
	p.store, err = log_stores.LoadStore(p.storeInfo.StoreType, storeConfig, p.logger)
	if err != nil {
		p.logger.Error().Msg(err.Error())
		return err
	}

	p.logger.Info().Fields(logParams).Msg("update source")
	p.source.Status = string(evmi_database.RunningLogSourceStatus)
	result = p.db.Conn.Save(&p.source)
	if result.Error != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("source updates")

	for {
		if !p.running {
			p.source.Status = "STOPPED"
			result := p.db.Conn.Save(&p.source.Status)
			if result.Error != nil {
				return result.Error
			}

			p.ended = true
			return nil
		}

		time.Sleep(time.Second)
	}

	return errors.New("config types invalid")
}

func (p *ExporterService) Stop() error {
	p.running = false

	for !p.IsEnded() {
		time.Sleep(time.Second)
	}

	return nil
}

func (p *ExporterService) IsEnded() bool {
	return p.ended
}

func getTxSender(chainId *big.Int, tx *ethTypes.Transaction) (string, error) {
	sender, err := ethTypes.Sender(ethTypes.NewPragueSigner(chainId), tx)
	if err != nil {
		return "", err
	}

	return sender.Hex(), nil
}
