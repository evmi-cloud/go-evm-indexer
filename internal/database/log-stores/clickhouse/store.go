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

	logTableName string
	txTableName  string
}

func (db *ClickHouseStore) Init(config map[string]string) error {

	addrs := strings.Split(config["addr"], `,`)
	database := config["database"]
	username := config["username"]
	password := config["password"]
	db.logTableName = config["logsTableName"]
	db.txTableName = config["transactionsTableName"]

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

	err = db.store.Exec(ctx, fmt.Sprintf(createLogsTableTemplate, db.logTableName))
	if err != nil {
		return err
	}

	err = db.store.Exec(ctx, fmt.Sprintf(createTransactionsTableTemplate, db.txTableName))
	if err != nil {
		return err
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

func (db *ClickHouseStore) GetLogs(fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmLog, error) {

	var results []ClickHouseEvmLog
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM %s WHERE block_number >= %d AND block_number <= %d ORDER BY minted_at OFFSET %d ROW FETCH FIRST %d ROWS ONLY", db.logTableName, fromBlock, toBlock, offset, limit)); err != nil {
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
func (db *ClickHouseStore) GetLatestLogs(limit uint64) ([]types.EvmLog, error) {

	var results []ClickHouseEvmLog
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM %s ORDER BY minted_at OFFSET %d ROW FETCH FIRST %d ROWS ONLY", db.logTableName, 0, limit)); err != nil {
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

func (db *ClickHouseStore) GetTransactions(fromBlock uint64, toBlock uint64, limit uint64, offset uint64) ([]types.EvmTransaction, error) {

	var results []ClickHouseEvmTransaction
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM %s AND block_number >= %d AND block_number <= %d ORDER BY minted_at OFFSET %d ROW FETCH FIRST %d ROWS ONLY", db.txTableName, fromBlock, toBlock, offset, limit)); err != nil {
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
