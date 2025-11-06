package clickhouse_store

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

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
			Id:       uuid.New().String(),
			SourceId: log.SourceId,
			Address:  log.Address,

			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionFrom:  log.TransactionFrom,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,

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

func (db *ClickHouseStore) GetLogsCount() (uint64, error) {
	var result struct {
		Count uint64 `ch:"count"`
	}
	if err := db.store.QueryRow(context.Background(), fmt.Sprintf("SELECT COUNT() as count FROM %s", db.txTableName)).ScanStruct(&result); err != nil {
		return 0, err
	}

	return uint64(result.Count), nil
}

func (db *ClickHouseStore) GetLogs(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmLog, error) {

	var results []ClickHouseEvmLog
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM %s WHERE source_id = %d AND block_number >= %d AND block_number <= %d ORDER BY block_number", db.logTableName, sourceId, fromBlock, toBlock)); err != nil {
		return []types.EvmLog{}, err
	}

	logs := []types.EvmLog{}
	for _, log := range results {
		logs = append(logs, types.EvmLog{
			Id:               log.Id,
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

func (db *ClickHouseStore) GetLogStream(sourceId uint64, fromBlock uint64, toBlock uint64, stream chan types.EvmLog) error {

	rows, err := db.store.Query(context.Background(), fmt.Sprintf("SELECT * FROM %s WHERE source_id = %d AND block_number >= %d AND block_number <= %d ORDER BY block_number", db.logTableName, sourceId, fromBlock, toBlock))
	if err != nil {
		return err
	}

	for rows.Next() {
		log := &ClickHouseEvmLog{}
		if err := rows.ScanStruct(log); err != nil {
			return err
		}

		stream <- types.EvmLog{
			Id:               log.Id,
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

			Metadata: types.EvmMetadata{
				ContractName: log.Metadata.ContractName,
				EventName:    log.Metadata.EventName,
				FunctionName: log.Metadata.FunctionName,
				Data:         log.Metadata.Data,
			},
		}
	}

	defer rows.Close()
	close(stream)

	return nil
}

func (db *ClickHouseStore) GetLatestLogs(sourceId uint64, limit uint64) ([]types.EvmLog, error) {

	var results []ClickHouseEvmLog
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM %s WHERE source_id = %d ORDER BY block_number DESC FETCH FIRST %d ROWS ONLY", db.logTableName, sourceId, limit)); err != nil {
		return []types.EvmLog{}, err
	}

	logs := []types.EvmLog{}
	for _, log := range results {
		logs = append(logs, types.EvmLog{
			Id:       log.Id,
			SourceId: log.SourceId,

			Address:          log.Address,
			Topics:           log.Topics,
			Data:             log.Data,
			BlockNumber:      log.BlockNumber,
			TransactionHash:  log.TransactionHash,
			TransactionIndex: log.TransactionIndex,
			BlockHash:        log.BlockHash,
			LogIndex:         log.LogIndex,
			Removed:          log.Removed,

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

func (db *ClickHouseStore) GetTransactions(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error) {

	var results []ClickHouseEvmTransaction
	if err := db.store.Select(context.Background(), &results, fmt.Sprintf("SELECT * FROM %s WHERE source_id = %d AND block_number >= %d AND block_number <= %d ORDER BY block_number DESC", db.txTableName, sourceId, fromBlock, toBlock)); err != nil {
		return []types.EvmTransaction{}, err
	}

	txs := []types.EvmTransaction{}
	for _, tx := range results {

		txs = append(txs, types.EvmTransaction{
			Id:          tx.Id,
			SourceId:    tx.SourceId,
			BlockNumber: tx.BlockNumber,
			ChainId:     tx.ChainId,
			From:        tx.From,
			Data:        tx.Data,
			Value:       tx.Value.String(),
			Nonce:       tx.Nonce,
			To:          tx.To,
			Hash:        tx.Hash,

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
