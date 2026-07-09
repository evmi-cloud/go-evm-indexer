package exporter

import (
	"context"
	"encoding/json"
	"os"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
)

// defaultBlockBatch bounds how many blocks of logs are pulled from the store per
// iteration when the blockchain does not define a BlockRange.
const defaultBlockBatch uint64 = 1000

// ExporterService runs a single plugin-backed exporter bound to one pipeline. It
// streams every stored log of the pipeline's sources, in ascending
// (block_number, log_index) order, into the plugin's NewLogEvent, committing the
// sync cursor at block boundaries so it resumes cleanly after a restart.
type ExporterService struct {
	db      *evmi_database.EvmiDatabase
	metrics *metrics.MetricService

	store  *log_stores.IndexerStore
	plugin pluginsdk.Exporter

	exporter  evmi_database.EvmiExporter
	pipeline  evmi_database.EvmLogPipeline
	chain     evmi_database.EvmBlockchain
	storeInfo evmi_database.EvmLogStore

	logger zerolog.Logger

	running bool
	ended   bool
}

func NewExporterService(
	db *evmi_database.EvmiDatabase,
	metrics *metrics.MetricService,
	exporter evmi_database.EvmiExporter,
) *ExporterService {

	logger := zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	return &ExporterService{
		db:       db,
		metrics:  metrics,
		exporter: exporter,
		logger:   logger,
		running:  false,
		ended:    true,
	}
}

func (p *ExporterService) Serve(ctx context.Context) error {
	p.running = true
	p.ended = false

	logParams := map[string]interface{}{"exporter": p.exporter.Name}
	p.logger.Info().Fields(logParams).Msg("starting exporter")

	// Reload the exporter row so we resume from the persisted cursor.
	if result := p.db.Conn.First(&p.exporter, p.exporter.ID); result.Error != nil {
		p.ended = true
		return result.Error
	}

	if result := p.db.Conn.First(&p.pipeline, p.exporter.EvmLogPipelineID); result.Error != nil {
		p.ended = true
		return result.Error
	}

	if result := p.db.Conn.First(&p.chain, p.pipeline.EvmBlockchainID); result.Error != nil {
		p.ended = true
		return result.Error
	}

	if result := p.db.Conn.First(&p.storeInfo, p.pipeline.EvmLogStoreId); result.Error != nil {
		p.ended = true
		return result.Error
	}

	var storeConfig map[string]string
	if err := json.Unmarshal(p.storeInfo.StoreConfig, &storeConfig); err != nil {
		p.ended = true
		return err
	}

	p.logger.Info().Fields(logParams).Msg("connecting store")
	store, err := log_stores.LoadStore(p.storeInfo.StoreType, storeConfig, p.logger)
	if err != nil {
		p.fail(err)
		return err
	}
	p.store = store

	p.logger.Info().Fields(logParams).Msg("loading plugin")
	plug, err := loadExporterPlugin(p.exporter, p.logger)
	if err != nil {
		p.fail(err)
		return err
	}
	p.plugin = plug

	if err := p.plugin.Init(pluginsdk.Context{
		ExporterName: p.exporter.Name,
		PipelineId:   uint64(p.pipeline.ID),
		ChainId:      p.chain.ChainId,
		Config:       []byte(p.exporter.PluginConfig),
	}); err != nil {
		p.fail(err)
		return err
	}

	p.exporter.Status = string(evmi_database.RunningExporterStatus)
	p.db.Conn.Save(&p.exporter)

	err = p.run(logParams)

	// Always give the plugin a chance to flush/close.
	if closeErr := p.plugin.Close(); closeErr != nil {
		p.logger.Error().Fields(logParams).Msg("plugin close error: " + closeErr.Error())
	}
	p.ended = true
	return err
}

// run is the main export loop. It returns nil on a clean stop and an error if the
// plugin or store fails (letting the supervisor restart it).
//
// The cursor is a (completedBlock, lastLogIndex) pair: completedBlock is the last
// fully-processed block, lastLogIndex is the last log_index delivered within the
// in-progress block (completedBlock+1), or -1 when none is. This pins the exact
// last log executed, so a restart resumes mid-block rather than replaying it.
func (p *ExporterService) run(logParams map[string]interface{}) error {
	completedBlock := p.exporter.SyncBlock
	lastLogIndex := p.exporter.SyncLogIndex
	if p.exporter.StartBlock > 0 && completedBlock < p.exporter.StartBlock {
		completedBlock = p.exporter.StartBlock - 1
		lastLogIndex = -1
	}

	batch := p.chain.BlockRange
	if batch == 0 {
		batch = defaultBlockBatch
	}

	pullInterval := time.Duration(p.chain.PullInterval) * time.Second
	if pullInterval == 0 {
		pullInterval = time.Second
	}

	for {
		if !p.running {
			p.exporter.Status = string(evmi_database.StoppedExporterStatus)
			p.db.Conn.Save(&p.exporter)
			return nil
		}

		sourceIds, head, err := p.sourcesAndHead()
		if err != nil {
			p.fail(err)
			return err
		}

		if len(sourceIds) == 0 || head <= completedBlock {
			time.Sleep(pullInterval)
			continue
		}

		toBlock := completedBlock + batch
		if toBlock > head {
			toBlock = head
		}

		completedBlock, lastLogIndex, err = p.exportRange(sourceIds, completedBlock, lastLogIndex, toBlock)
		if err != nil {
			return err
		}

		p.metrics.LatestBlockIndexedMetricsSet(p.exporter.Name, "exporter", p.chain.ChainId, toBlock)
		p.logger.Info().Fields(map[string]interface{}{
			"exporter": p.exporter.Name, "toBlock": toBlock,
		}).Msg("exported block range")
	}
}

// exportRange fetches the logs strictly after (completedBlock, lastLogIndex) up to
// toBlock and delivers them to the plugin one at a time, in (block, log_index)
// order, persisting the cursor after each log. It returns the advanced cursor.
//
// Delivery is strictly sequential: logs are handed to NewLogEvent one by one in a
// plain loop, and a failure returns immediately with the cursor at the last
// successfully delivered log, so the failing log is replayed on restart
// (at-least-once). It never delivers logs concurrently or out of order.
func (p *ExporterService) exportRange(sourceIds []uint64, completedBlock uint64, lastLogIndex int64, toBlock uint64) (uint64, int64, error) {
	afterBlock, afterIndex := cursorBound(completedBlock, lastLogIndex)
	logs, err := p.store.GetStorage().GetLogsAfter(sourceIds, afterBlock, afterIndex, toBlock)
	if err != nil {
		p.fail(err)
		return completedBlock, lastLogIndex, err
	}

	for _, l := range logs {
		if err := p.plugin.NewLogEvent(toLogEvent(l)); err != nil {
			p.fail(err)
			return completedBlock, lastLogIndex, err
		}
		// A delivered log at (B, I) means every block < B is complete and block B
		// is in progress at index I.
		completedBlock = blockBefore(l.BlockNumber)
		lastLogIndex = int64(l.LogIndex)
		if err := p.persistCursor(completedBlock, lastLogIndex); err != nil {
			return completedBlock, lastLogIndex, err
		}
	}

	// The whole range up to toBlock has now been scanned: toBlock is complete
	// (including any empty tail) and there is no in-progress block.
	completedBlock = toBlock
	lastLogIndex = -1
	if err := p.persistCursor(completedBlock, lastLogIndex); err != nil {
		return completedBlock, lastLogIndex, err
	}
	return completedBlock, lastLogIndex, nil
}

// blockBefore returns b-1, guarding the genesis edge (block 0 with logs is not
// resumable mid-block; such logs are effectively never present on EVM chains).
func blockBefore(b uint64) uint64 {
	if b == 0 {
		return 0
	}
	return b - 1
}

// cursorBound converts a (completedBlock, lastLogIndex) cursor into a strict
// "after this log" bound for GetLogsAfter.
func cursorBound(completedBlock uint64, lastLogIndex int64) (uint64, uint64) {
	if lastLogIndex < 0 {
		// Nothing in progress: resume strictly after the completed block.
		return completedBlock, ^uint64(0)
	}
	// Mid-block: resume strictly after (completedBlock+1, lastLogIndex).
	return completedBlock + 1, uint64(lastLogIndex)
}

// sourcesAndHead returns the enabled source ids of the pipeline and the highest
// block that is safe to export: the minimum SyncBlock across those sources, so no
// source is left behind within an exported range.
func (p *ExporterService) sourcesAndHead() ([]uint64, uint64, error) {
	var sources []evmi_database.EvmLogSource
	result := p.db.Conn.Model(&evmi_database.EvmLogSource{}).
		Where("evm_log_pipeline_id = ? AND enabled = ?", p.pipeline.ID, true).
		Find(&sources)
	if result.Error != nil {
		return nil, 0, result.Error
	}

	if len(sources) == 0 {
		return nil, 0, nil
	}

	sourceIds := make([]uint64, 0, len(sources))
	var head uint64
	for i, s := range sources {
		sourceIds = append(sourceIds, uint64(s.ID))
		if i == 0 || s.SyncBlock < head {
			head = s.SyncBlock
		}
	}
	return sourceIds, head, nil
}

// persistCursor writes the (block, logIndex) cursor to the exporter row.
func (p *ExporterService) persistCursor(block uint64, logIndex int64) error {
	p.exporter.SyncBlock = block
	p.exporter.SyncLogIndex = logIndex
	result := p.db.Conn.Model(&p.exporter).Updates(map[string]interface{}{
		"sync_block":     block,
		"sync_log_index": logIndex,
	})
	if result.Error != nil {
		p.fail(result.Error)
		return result.Error
	}
	return nil
}

func (p *ExporterService) fail(err error) {
	p.logger.Error().Str("exporter", p.exporter.Name).Msg(err.Error())
	p.exporter.Status = string(evmi_database.FailedExporterStatus)
	p.db.Conn.Save(&p.exporter)
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

func toLogEvent(l types.EvmLog) pluginsdk.LogEvent {
	return pluginsdk.LogEvent{
		Id:               l.Id,
		SourceId:         l.SourceId,
		ChainId:          l.ChainId,
		Address:          l.Address,
		Topics:           l.Topics,
		Data:             l.Data,
		BlockNumber:      l.BlockNumber,
		TransactionHash:  l.TransactionHash,
		TransactionFrom:  l.TransactionFrom,
		TransactionIndex: l.TransactionIndex,
		BlockHash:        l.BlockHash,
		LogIndex:         l.LogIndex,
		Removed:          l.Removed,

		ContractName: l.Metadata.ContractName,
		EventName:    l.Metadata.EventName,
		Args:         l.Metadata.Data,
	}
}
