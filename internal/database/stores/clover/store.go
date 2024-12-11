package clover

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
	uuid "github.com/google/uuid"
	"github.com/ostafen/clover/v2"
	"github.com/ostafen/clover/v2/document"
	"github.com/ostafen/clover/v2/query"
	"github.com/rs/zerolog"
)

const (
	StoreCollectionName       string = "stores"
	SourceCollectionName      string = "sources"
	LogCollectionName         string = "logs"
	TransactionCollectionName string = "transactions"
)

type CloverStore struct {
	path   string
	logger zerolog.Logger
	store  *clover.DB
}

// GetDatabaseDiskSize implements stores.EvmIndexerStorage.
func (db *CloverStore) GetDatabaseDiskSize() (uint64, error) {

	var totalSize int64
	err := filepath.Walk(db.path, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		return 0, err
	}

	return uint64(totalSize), nil
}

// GetStoreGlobalLatestBlock implements stores.EvmIndexerStorage.
func (db *CloverStore) GetStoreGlobalLatestBlock(storeId string) (uint64, error) {
	sources, err := db.GetSources(storeId)
	if err != nil {
		return 0, err
	}

	var globalLatestBlock uint64 = 0
	for _, source := range sources {
		if globalLatestBlock == 0 {
			globalLatestBlock = source.LatestBlockIndexed
		} else {
			if source.LatestBlockIndexed < globalLatestBlock {
				globalLatestBlock = source.LatestBlockIndexed
			}
		}
	}

	if globalLatestBlock == 0 {
		return 0, errors.New("an error occured on sources latest block load")
	}

	return globalLatestBlock, nil
}

func (db *CloverStore) Init(config models.Config) error {

	store, err := clover.Open(db.path)
	if err != nil {
		return err
	}

	db.store = store

	exists, err := db.store.HasCollection(LogCollectionName)
	if err != nil {
		return err
	}

	if !exists {
		err := db.store.CreateCollection(LogCollectionName)
		if err != nil {
			return err
		}

		db.store.CreateIndex(LogCollectionName, "blockNumber")
	}

	exists, err = db.store.HasCollection(TransactionCollectionName)
	if err != nil {
		return err
	}

	if !exists {
		err = db.store.CreateCollection(TransactionCollectionName)
		if err != nil {
			return err
		}

		db.store.CreateIndex(TransactionCollectionName, "blockNumber")
	}

	exists, err = db.store.HasCollection(SourceCollectionName)
	if err != nil {
		return err
	}

	if !exists {
		db.store.CreateCollection(SourceCollectionName)
		if err != nil {
			return err
		}
	}

	exists, err = db.store.HasCollection(StoreCollectionName)
	if err != nil {
		return err
	}

	if !exists {
		db.store.CreateCollection(StoreCollectionName)

		for _, storeConfig := range config.Stores {

			store := &CloverLogStore{
				Id:          uuid.New().String(),
				Identifier:  storeConfig.Identifier,
				Description: storeConfig.Description,

				ChainId: storeConfig.ChainId,
				Rpc:     storeConfig.Rpc,

				Status:           "Initialization",
				LatestChainBlock: 0,
			}

			storeId, err := db.store.InsertOne(StoreCollectionName, document.NewDocumentOf(store))
			if err != nil {
				return err
			}

			db.logger.Info().Msg(store.Identifier + " store initialized with id " + storeId)

			for _, s := range storeConfig.Sources {

				contracts := []CloverLogSourceContract{}
				for _, contract := range s.Contracts {
					contracts = append(contracts, CloverLogSourceContract{
						ContractName: contract.ContractName,
						Address:      contract.Address,
					})
				}

				source := &CloverLogSource{
					Id:                 uuid.New().String(),
					LogStoreId:         storeId,
					Name:               s.Name,
					Type:               string(s.Type),
					Contracts:          contracts,
					Topic:              s.Topic,
					IndexedTopics:      s.IndexedTopics,
					StartBlock:         s.StartBlock,
					BlockRange:         s.BlockRange,
					LatestBlockIndexed: s.StartBlock,
				}

				_, err := db.store.InsertOne(SourceCollectionName, document.NewDocumentOf(source))
				if err != nil {
					return err
				}

				db.logger.Info().Msg(source.Name + " source has been added to " + store.Identifier)
			}
		}
	}

	return nil
}

func (db *CloverStore) InsertLogs(logs []models.EvmLog) error {

	var logsToInsert []*document.Document
	for _, log := range logs {
		l := CloverEvmLog{
			Id:               uuid.New().String(),
			StoreId:          log.StoreId,
			SourceId:         log.SourceId,
			Address:          log.Address,
			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,
			MintedAt:         log.MintedAt,

			Metadata: CloverEvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		}

		logsToInsert = append(logsToInsert, document.NewDocumentOf(l))
	}

	err := db.store.Insert(LogCollectionName, logsToInsert...)
	if err != nil {
		return err
	}

	return nil
}

func (db *CloverStore) InsertTransactions(txs []models.EvmTransaction) error {

	var txsToInsert []*document.Document
	for _, tx := range txs {
		transaction := &CloverEvmTransaction{
			Id:          uuid.New().String(),
			StoreId:     tx.StoreId,
			SourceId:    tx.SourceId,
			BlockNumber: tx.BlockNumber,
			ChainId:     tx.ChainId,
			From:        tx.From,
			Data:        tx.Data,
			Value:       tx.Value,
			Nonce:       tx.Nonce,
			To:          tx.To,
			Hash:        tx.Hash,
			MintedAt:    tx.MintedAt,

			Metadata: CloverEvmMetadata{
				ContractName: tx.Metadata.ContractName,
				EventName:    tx.Metadata.EventName,
				FunctionName: tx.Metadata.FunctionName,
				Data:         tx.Metadata.Data,
			},
		}

		txsToInsert = append(txsToInsert, document.NewDocumentOf(transaction))
	}

	err := db.store.Insert(TransactionCollectionName, txsToInsert...)
	if err != nil {
		return err
	}

	return nil
}

func (db *CloverStore) UpdateSourceLatestBlock(sourceId string, latestBlock uint64) error {

	err := db.store.Update(
		query.NewQuery(SourceCollectionName).Where(
			query.Field("_id").Eq(sourceId),
		),
		map[string]interface{}{"latestBlockIndexed": latestBlock},
	)

	if err != nil {
		return err
	}

	return nil
}

// GetLogsByStoreCount implements stores.EvmIndexerStorage.
func (db *CloverStore) GetLogsByStoreCount(storeId string) (uint64, error) {
	count, err := db.store.Count(query.NewQuery(LogCollectionName).Where(query.Field("storeId").Eq(storeId)))
	if err != nil {
		return 0, err
	}

	return uint64(count), nil
}

// GetLogsCount implements stores.EvmIndexerStorage.
func (db *CloverStore) GetLogsCount() (uint64, error) {
	count, err := db.store.Count(query.NewQuery(LogCollectionName))
	if err != nil {
		return 0, err
	}

	return uint64(count), nil
}

func (db *CloverStore) GetStoreById(storeId string) (models.LogStore, error) {

	doc, err := db.store.FindById(StoreCollectionName, storeId)
	if err != nil {
		return models.LogStore{}, err
	}
	if doc == nil {
		return models.LogStore{}, errors.New("no store found")
	}

	store := &CloverLogStore{}
	err = doc.Unmarshal(store)
	if err != nil {
		return models.LogStore{}, err
	}

	return models.LogStore{
		Id:          store.Id,
		Identifier:  store.Identifier,
		Description: store.Description,
		Status:      models.PipelineStatus(store.Status),
		ChainId:     store.ChainId,
		Rpc:         store.Rpc,

		LatestChainBlock: store.LatestChainBlock,
	}, nil
}

func (db *CloverStore) GetStores() ([]models.LogStore, error) {

	docs, err := db.store.FindAll(query.NewQuery(StoreCollectionName))
	if err != nil {
		return []models.LogStore{}, err
	}

	if len(docs) == 0 {
		return []models.LogStore{}, errors.New("no store found")
	}

	stores := []models.LogStore{}
	for _, doc := range docs {
		store := &CloverLogStore{}
		err = doc.Unmarshal(store)
		if err != nil {
			return []models.LogStore{}, err
		}

		stores = append(stores, models.LogStore{
			Id:          store.Id,
			Identifier:  store.Identifier,
			Description: store.Description,
			Status:      models.PipelineStatus(store.Status),
			ChainId:     store.ChainId,
			Rpc:         store.Rpc,

			LatestChainBlock: store.LatestChainBlock,
		})
	}

	return stores, nil
}

func (db *CloverStore) GetSources(storeId string) ([]models.LogSource, error) {

	docs, err := db.store.FindAll(query.NewQuery(SourceCollectionName).Where(query.Field("storeId").Eq(storeId)))
	if err != nil {
		return []models.LogSource{}, err
	}

	otherdocs, err := db.store.FindAll(query.NewQuery(SourceCollectionName))
	if err != nil {
		return []models.LogSource{}, err
	}

	for _, doc := range otherdocs {
		source := &CloverLogSource{}
		err = doc.Unmarshal(source)
		if err != nil {
			return []models.LogSource{}, err
		}
	}

	if len(docs) == 0 {
		return []models.LogSource{}, errors.New("no source found")
	}

	sources := []models.LogSource{}
	for _, doc := range docs {
		source := &CloverLogSource{}
		err = doc.Unmarshal(source)
		if err != nil {
			return []models.LogSource{}, err
		}

		contracts := []struct {
			ContractName string
			Address      string
		}{}

		for _, contract := range source.Contracts {
			contracts = append(contracts, struct {
				ContractName string
				Address      string
			}{
				ContractName: contract.ContractName,
				Address:      contract.Address,
			})
		}

		sources = append(sources, models.LogSource{
			Id:            source.Id,
			LogStoreId:    source.LogStoreId,
			Name:          source.Name,
			Type:          models.PipelineConfigType(source.Type),
			Contracts:     contracts,
			Topic:         source.Topic,
			IndexedTopics: source.IndexedTopics,
			StartBlock:    source.StartBlock,
			BlockRange:    source.BlockRange,

			LatestBlockIndexed: source.LatestBlockIndexed,
		})
	}

	return sources, nil
}

func (db *CloverStore) GetLogs(storeId string, fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]models.EvmLog, error) {

	docs, err := db.store.FindAll(
		query.NewQuery(LogCollectionName).Where(
			query.Field("storeId").Eq(storeId).
				And(query.Field("blockNumber").GtEq(fromBlock)).
				And(query.Field("blockNumber").LtEq(toBlock)),
		).Sort(query.SortOption{Field: "blockNumber", Direction: 1}).
			Skip(int(offset)).
			Limit(int(limit)),
	)

	if err != nil {
		return []models.EvmLog{}, err
	}

	logs := []models.EvmLog{}
	for _, doc := range docs {
		log := CloverEvmLog{}
		err = doc.Unmarshal(log)
		if err != nil {
			return []models.EvmLog{}, err
		}

		logs = append(logs, models.EvmLog{
			Id:               log.Id,
			StoreId:          log.StoreId,
			SourceId:         log.SourceId,
			Address:          log.Address,
			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,
			MintedAt:         log.MintedAt,

			Metadata: models.EvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		})
	}

	return logs, nil
}

// GetLatestLogs implements stores.EvmIndexerStorage.
func (db *CloverStore) GetLatestLogs(storeId string, limit uint64) ([]models.EvmLog, error) {

	docs, err := db.store.FindAll(query.NewQuery(LogCollectionName).
		Where(query.Field("storeId").Eq(storeId)).
		Sort(query.SortOption{Field: "blockNumber", Direction: -1}).
		Limit(int(limit)))

	if err != nil {
		return []models.EvmLog{}, err
	}

	logs := []models.EvmLog{}
	for _, doc := range docs {
		log := &CloverEvmLog{}
		err = doc.Unmarshal(log)
		if err != nil {
			return []models.EvmLog{}, err
		}

		logs = append(logs, models.EvmLog{
			Id:               log.Id,
			StoreId:          log.StoreId,
			SourceId:         log.SourceId,
			Address:          log.Address,
			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,
			MintedAt:         log.MintedAt,

			Metadata: models.EvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		})
	}

	return logs, nil
}

func (db *CloverStore) GetTransactions(storeId string, fromBlock uint64, toBlock uint64) ([]models.EvmTransaction, error) {

	docs, err := db.store.FindAll(query.NewQuery(TransactionCollectionName).Where(
		query.Field("storeId").Eq(storeId).
			And(query.Field("blockNumber").GtEq(fromBlock)).
			And(query.Field("blockNumber").LtEq(toBlock)),
	))

	if err != nil {
		return []models.EvmTransaction{}, err
	}

	txs := []models.EvmTransaction{}
	for _, doc := range docs {
		tx := &CloverEvmTransaction{}
		err = doc.Unmarshal(tx)
		if err != nil {
			return []models.EvmTransaction{}, err
		}

		txs = append(txs, models.EvmTransaction{
			Id:          tx.Id,
			StoreId:     tx.StoreId,
			SourceId:    tx.SourceId,
			BlockNumber: tx.BlockNumber,
			ChainId:     tx.ChainId,
			From:        tx.From,
			Data:        tx.Data,
			Value:       tx.Value,
			Nonce:       tx.Nonce,
			To:          tx.To,
			Hash:        tx.Hash,
			MintedAt:    tx.MintedAt,

			Metadata: models.EvmMetadata{
				ContractName: tx.Metadata.ContractName,
				EventName:    tx.Metadata.EventName,
				FunctionName: tx.Metadata.FunctionName,
				Data:         tx.Metadata.Data,
			},
		})
	}

	return txs, nil
}

func NewCloverStore(path string, logger zerolog.Logger) (*CloverStore, error) {

	return &CloverStore{
		path:   path,
		logger: logger,
	}, nil
}
