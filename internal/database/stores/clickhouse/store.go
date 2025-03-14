package clickhouse_store

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	uuid "github.com/google/uuid"
	"github.com/rs/zerolog"
)

type ClickHouseStore struct {
	logger zerolog.Logger
	store  driver.Conn
}

// GetDatabaseDiskSize implements stores.EvmIndexerStorage.
func (db *ClickHouseStore) GetDatabaseDiskSize() (uint64, error) {
	//TODO: check size on clickhouse
	return uint64(0), nil
}

// GetStoreGlobalLatestBlock implements stores.EvmIndexerStorage.
func (db *ClickHouseStore) GetStoreGlobalLatestBlock(storeId string) (uint64, error) {
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

func (db *ClickHouseStore) Init(config types.Config) error {

	addrs := strings.Split(config.Storage.Config["addr"], `,`)
	database := config.Storage.Config["database"]
	username := config.Storage.Config["username"]
	password := config.Storage.Config["password"]

	ctx := context.Background()
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: addrs,
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
		ClientInfo: clickhouse.ClientInfo{
			Products: []struct {
				Name    string
				Version string
			}{
				{Name: "go-evm-indexer", Version: "0.1"},
			},
		},
		Debugf: func(format string, v ...interface{}) {
			fmt.Printf(format, v)
		},
	})

	if err != nil {
		return err
	}

	if err := conn.Ping(ctx); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			fmt.Printf("Exception [%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		}
		return err
	}

	db.store = conn

	//Tables creations
	err = db.store.Exec(ctx, "SET enable_json_type = 1")
	if err != nil {
		return err
	}

	err = db.store.Exec(ctx, createStoresTableTemplate)
	if err != nil {
		return err
	}

	err = db.store.Exec(ctx, createSourcesTableTemplate)
	if err != nil {
		return err
	}

	err = db.store.Exec(ctx, createLogsTableTemplate)
	if err != nil {
		return err
	}

	err = db.store.Exec(ctx, createTransactionsTableTemplate)
	if err != nil {
		return err
	}

	var result struct {
		Count uint64 `ch:"count"`
	}
	if err := conn.QueryRow(context.Background(), "SELECT COUNT() as count FROM stores").ScanStruct(&result); err != nil {
		return err
	}

	if result.Count == 0 {
		for _, storeConfig := range config.Stores {

			store := &ClickHouseLogStore{
				Id:          uuid.New().String(),
				Identifier:  storeConfig.Identifier,
				Description: storeConfig.Description,

				ChainId: storeConfig.ChainId,
				Rpc:     storeConfig.Rpc,

				Status: "Initialization",
			}

			batch, err := db.store.PrepareBatch(context.Background(), "INSERT INTO stores")
			if err != nil {
				return err
			}

			err = batch.AppendStruct(store)
			if err != nil {
				return err
			}

			err = batch.Send()
			if err != nil {
				return err
			}

			db.logger.Info().Msg(store.Identifier + " store initialized with id " + store.Id)

			batchSources, err := db.store.PrepareBatch(context.Background(), "INSERT INTO sources")
			if err != nil {
				return err
			}

			for _, s := range storeConfig.Sources {

				contracts := []ClickHouseLogSourceContract{}
				for _, contract := range s.Contracts {
					contracts = append(contracts, ClickHouseLogSourceContract{
						ContractName: contract.ContractName,
						Address:      contract.Address,
					})
				}

				source := &ClickHouseLogSource{
					Id:            uuid.New().String(),
					LogStoreId:    store.Id,
					Name:          s.Name,
					Type:          string(s.Type),
					Contracts:     contracts,
					Topic:         s.Topic,
					IndexedTopics: s.IndexedTopics,
					StartBlock:    s.StartBlock,
					BlockRange:    s.BlockRange,
				}

				err = batchSources.AppendStruct(source)
				if err != nil {
					return err
				}

				db.logger.Info().Msg(source.Name + " source has been added to " + store.Identifier)
			}

			err = batchSources.Send()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (db *ClickHouseStore) InsertLogs(logs []types.EvmLog) error {

	batch, err := db.store.PrepareBatch(context.Background(), "INSERT INTO logs")
	if err != nil {
		return err
	}

	for _, log := range logs {
		l := &ClickHouseEvmLog{
			Id:               uuid.New().String(),
			StoreId:          log.StoreId,
			SourceId:         log.SourceId,
			Address:          log.Address,
			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionFrom:  log.TransactionFrom,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,
			MintedAt:         time.Unix(int64(log.MintedAt), 0),

			Metadata: ClickHouseEvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		}

		err = batch.AppendStruct(l)
		if err != nil {
			return err
		}

	}

	return batch.Send()
}

func (db *ClickHouseStore) InsertTransactions(txs []types.EvmTransaction) error {

	batch, err := db.store.PrepareBatch(context.Background(), "INSERT INTO transactions")
	if err != nil {
		return err
	}

	for _, tx := range txs {
		value, ok := new(big.Int).SetString(tx.Value, 10)
		if !ok {
			return errors.New("bad tx value")
		}

		transaction := &ClickHouseEvmTransaction{
			Id:               uuid.New().String(),
			StoreId:          tx.StoreId,
			SourceId:         tx.SourceId,
			BlockNumber:      tx.BlockNumber,
			TransactionIndex: tx.TransactionIndex,
			ChainId:          tx.ChainId,
			From:             tx.From,
			Data:             tx.Data,
			Value:            value,
			Nonce:            tx.Nonce,
			To:               tx.To,
			Hash:             tx.Hash,
			MintedAt:         time.Unix(int64(tx.MintedAt), 0),

			Metadata: ClickHouseEvmMetadata{
				ContractName: tx.Metadata.ContractName,
				EventName:    tx.Metadata.EventName,
				FunctionName: tx.Metadata.FunctionName,
				Data:         tx.Metadata.Data,
			},
		}

		err = batch.AppendStruct(transaction)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}

func (db *ClickHouseStore) UpdateSourceLatestBlock(sourceId string, latestBlock uint64) error {
	request := fmt.Sprintf(`ALTER TABLE sources UPDATE latest_block_indexed = %d WHERE id = '%s'`, latestBlock, sourceId)
	return db.store.Exec(context.Background(), request)
}

// GetLogsByStoreCount implements stores.EvmIndexerStorage.
func (db *ClickHouseStore) GetLogsByStoreCount(storeId string) (uint64, error) {

	var result struct {
		Count uint64 `ch:"count"`
	}
	if err := db.store.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT COUNT() as count FROM logs WHERE store_id = '%s'", storeId),
	).ScanStruct(&result); err != nil {
		return 0, err
	}

	return uint64(result.Count), nil
}

// GetLogsCount implements stores.EvmIndexerStorage.
func (db *ClickHouseStore) GetLogsCount() (uint64, error) {
	var result struct {
		Count uint64 `ch:"count"`
	}
	if err := db.store.QueryRow(context.Background(), "SELECT COUNT() as count FROM logs").ScanStruct(&result); err != nil {
		return 0, err
	}

	return uint64(result.Count), nil
}

func (db *ClickHouseStore) GetStoreById(storeId string) (types.LogStore, error) {

	var result ClickHouseLogStore
	if err := db.store.QueryRow(context.Background(), fmt.Sprintf("SELECT * FROM stores WHERE id = '%s'", storeId)).ScanStruct(&result); err != nil {
		return types.LogStore{}, err
	}

	return types.LogStore{
		Id:          result.Id,
		Identifier:  result.Identifier,
		Description: result.Description,
		Status:      types.PipelineStatus(result.Status),
		ChainId:     result.ChainId,
		Rpc:         result.Rpc,
	}, nil
}

func (db *ClickHouseStore) GetStores() ([]types.LogStore, error) {

	var results []ClickHouseLogStore
	if err := db.store.Select(context.Background(), &results, "SELECT * FROM stores"); err != nil {
		return []types.LogStore{}, err
	}

	stores := []types.LogStore{}
	for _, s := range results {
		stores = append(stores, types.LogStore{
			Id:          s.Id,
			Identifier:  s.Identifier,
			Description: s.Description,
			Status:      types.PipelineStatus(s.Status),
			ChainId:     s.ChainId,
			Rpc:         s.Rpc,
		})
	}

	return stores, nil
}

func (db *ClickHouseStore) GetSources(storeId string) ([]types.LogSource, error) {

	var results []ClickHouseLogSource
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM sources WHERE store_id = '%s'", storeId)); err != nil {
		return []types.LogSource{}, err
	}

	if len(results) == 0 {
		return []types.LogSource{}, errors.New("no source found")
	}

	sources := []types.LogSource{}
	for _, s := range results {

		contracts := []struct {
			ContractName string
			Address      string
		}{}

		for _, contract := range s.Contracts {
			contracts = append(contracts, struct {
				ContractName string
				Address      string
			}{
				ContractName: contract.ContractName,
				Address:      contract.Address,
			})
		}

		sources = append(sources, types.LogSource{
			Id:            s.Id,
			LogStoreId:    s.LogStoreId,
			Name:          s.Name,
			Type:          types.PipelineConfigType(s.Type),
			Contracts:     contracts,
			Topic:         s.Topic,
			IndexedTopics: s.IndexedTopics,
			StartBlock:    s.StartBlock,
			BlockRange:    s.BlockRange,

			LatestBlockIndexed: s.LatestBlockIndexed,
		})
	}

	return sources, nil
}

func (db *ClickHouseStore) GetLogs(storeId string, fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmLog, error) {

	var results []ClickHouseEvmLog
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM logs WHERE store_id = '%s' AND block_number >= %d AND block_number <= %d ORDER BY minted_at OFFSET %d ROW FETCH FIRST %d ROWS ONLY", storeId, fromBlock, toBlock, offset, limit)); err != nil {
		return []types.EvmLog{}, err
	}

	logs := []types.EvmLog{}
	for _, log := range results {
		logs = append(logs, types.EvmLog{
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
			MintedAt:         uint64(log.MintedAt.Unix()),

			Metadata: types.EvmMetadata{
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
func (db *ClickHouseStore) GetLatestLogs(storeId string, limit uint64) ([]types.EvmLog, error) {

	var results []ClickHouseEvmLog
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM logs WHERE store_id = '%s' ORDER BY minted_at OFFSET %d ROW FETCH FIRST %d ROWS ONLY", storeId, 0, limit)); err != nil {
		return []types.EvmLog{}, err
	}

	logs := []types.EvmLog{}
	for _, log := range results {
		logs = append(logs, types.EvmLog{
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
			MintedAt:         uint64(log.MintedAt.Unix()),

			Metadata: types.EvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		})
	}

	return logs, nil
}

func (db *ClickHouseStore) GetTransactions(storeId string, fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmTransaction, error) {

	var results []ClickHouseEvmTransaction
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM transactions WHERE store_id = '%s' AND block_number >= %d AND block_number <= %d ORDER BY minted_at OFFSET %d ROW FETCH FIRST %d ROWS ONLY", storeId, fromBlock, toBlock, offset, limit)); err != nil {
		return []types.EvmTransaction{}, err
	}

	txs := []types.EvmTransaction{}
	for _, tx := range results {

		txs = append(txs, types.EvmTransaction{
			Id:          tx.Id,
			StoreId:     tx.StoreId,
			SourceId:    tx.SourceId,
			BlockNumber: tx.BlockNumber,
			ChainId:     tx.ChainId,
			From:        tx.From,
			Data:        tx.Data,
			Value:       tx.Value.String(),
			Nonce:       tx.Nonce,
			To:          tx.To,
			Hash:        tx.Hash,
			MintedAt:    uint64(tx.MintedAt.Unix()),

			Metadata: types.EvmMetadata{
				ContractName: tx.Metadata.ContractName,
				EventName:    tx.Metadata.EventName,
				FunctionName: tx.Metadata.FunctionName,
				Data:         tx.Metadata.Data,
			},
		})
	}

	return txs, nil
}

func NewClickHouseStore(logger zerolog.Logger) (*ClickHouseStore, error) {
	return &ClickHouseStore{
		logger: logger,
	}, nil
}
