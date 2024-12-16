package backup

import (
	"fmt"
	"math"
	"os"

	"github.com/evmi-cloud/go-evm-indexer/internal/backup/exporter"
	gcpgcs "github.com/evmi-cloud/go-evm-indexer/internal/backup/storage/gcp-gcs"
	"github.com/evmi-cloud/go-evm-indexer/internal/database"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

type BackupService struct {
	logger zerolog.Logger
	config types.Config

	db database.IndexerDatabase

	cron     *cron.Cron
	store    types.EvmIndexerBackupStorage
	exporter types.EvmIndexerBackupExporter
}

func (h *BackupService) Start() {

	if h.config.Backup.Enabled {
		h.cron.AddFunc(h.config.Backup.Crontab, func() {
			err := h.ProcessBackup()
			if err != nil {
				h.logger.Error().Msg("error during backing process: " + err.Error())
			}
		})

		h.cron.Start()
		h.logger.Info().Msg("Backup service started")
	}
}

func (h *BackupService) ProcessBackup() error {

	database, err := h.db.GetStoreDatabase()
	if err != nil {
		return err
	}

	stores, err := database.GetStores()
	if err != nil {
		return err
	}

	for _, store := range stores {

		state, exists, err := h.getState(store.Identifier)
		if err != nil {
			return err
		}

		if !exists {
			fromBlock := math.Inf(1)
			sources, err := database.GetSources(store.Id)
			if err != nil {
				return err
			}

			for _, source := range sources {
				if source.StartBlock < uint64(fromBlock) {
					fromBlock = float64(source.StartBlock)
				}
			}

			state = types.EvmIndexerBackupState{
				FromBlock: uint64(fromBlock),
				ToBlock:   uint64(fromBlock),

				FileList: []types.EvmIndexerBackupFile{},
			}
		}

		//get new blocks to add
		oldToBlock := state.ToBlock
		newToBlock, err := database.GetStoreGlobalLatestBlock(store.Id)
		if err != nil {
			return err
		}

		//load logs and txs from database
		logs, err := database.GetLogs(store.Id, oldToBlock+1, newToBlock, uint64(math.Inf(1)), 0)
		if err != nil {
			return err
		}

		txs, err := database.GetTransactions(store.Id, oldToBlock+1, newToBlock, uint64(math.Inf(1)), 0)
		if err != nil {
			return err
		}

		//package file with exporter
		logsFileName := "/" + store.Identifier + "-logs-" + fmt.Sprint(oldToBlock+1) + "-" + fmt.Sprint(newToBlock)
		err = h.exporter.ExportLogsToFile("/tmp/"+logsFileName, logs)
		if err != nil {
			return err
		}

		txsFileName := "/" + store.Identifier + "-txs-" + fmt.Sprint(oldToBlock+1) + "-" + fmt.Sprint(newToBlock)
		err = h.exporter.ExportTransactionsToFile("/tmp/"+txsFileName, txs)
		if err != nil {
			return err
		}

		//upload file
		err = h.store.UploadFile("/tmp/"+logsFileName, logsFileName, false)
		if err != nil {
			return err
		}

		err = h.store.UploadFile("/tmp/"+txsFileName, txsFileName, false)
		if err != nil {
			return err
		}

		//update state
		state.ToBlock = newToBlock
		state.FileList = append(state.FileList, types.EvmIndexerBackupFile{
			Identifier: logsFileName,
			FromBlock:  oldToBlock + 1,
			ToBlock:    newToBlock,
		})

		stateFileName := "/" + store.Identifier + "-state"
		err = h.exporter.ExportStateToFile("/tmp/"+stateFileName, state)
		if err != nil {
			return err
		}

		err = h.store.UploadFile("/tmp/"+stateFileName, stateFileName, true)
		if err != nil {
			return err
		}

		//clean tmp files
		err = os.Remove("/tmp/" + logsFileName)
		if err != nil {
			return err
		}

		err = os.Remove("/tmp/" + txsFileName)
		if err != nil {
			return err
		}

		err = os.Remove("/tmp/" + stateFileName)
		if err != nil {
			return err
		}

		return nil
	}

	h.logger.Info().Msg("Backup processing start")
	return nil
}

func (h *BackupService) getState(indentifier string) (types.EvmIndexerBackupState, bool, error) {

	content, exists, err := h.store.LoadFile("/" + indentifier + "-state")
	if err != nil {
		return types.EvmIndexerBackupState{}, false, err
	}

	if !exists {
		return types.EvmIndexerBackupState{}, false, nil
	}

	state, err := h.exporter.ImportStateFromBytes(content)
	if err != nil {
		return types.EvmIndexerBackupState{}, true, err
	}

	return state, true, nil
}

func NewBackupService(db *database.IndexerDatabase, config types.Config, logger zerolog.Logger) (*BackupService, error) {

	service := &BackupService{
		logger: logger,
		config: config,
		cron:   cron.New(),
	}

	//Load store
	if config.Backup.Storage == "gcp-gcs" {
		service.store = gcpgcs.NewGoogleCloudStorageBackupService(logger, config)
	}

	//Load exporter
	exporter, err := exporter.NewBackupExporterService(config.Backup.Exporter)
	if err != nil {
		return nil, err
	}

	service.exporter = exporter
	return service, nil
}
