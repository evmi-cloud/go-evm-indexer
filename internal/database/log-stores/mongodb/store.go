// Package mongodb_store implements the EvmIndexerStorage backend on MongoDB.
// Logs and transactions are upserted as documents keyed by their stable id
// (_id); queries are finds with bool/range filters sorted by
// (block_number, log_index).
package mongodb_store

import (
	"context"

	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoStore struct {
	logger zerolog.Logger
	client *mongo.Client
	logs   *mongo.Collection
	txs    *mongo.Collection
}

func NewMongoStore(logger zerolog.Logger) (*MongoStore, error) {
	return &MongoStore{logger: logger}, nil
}

func (s *MongoStore) Init(config map[string]string) error {
	uri := orDefault(config["uri"], "mongodb://localhost:27017")
	ctx := context.Background()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return err
	}
	s.client = client

	db := client.Database(orDefault(config["database"], "evmi"))
	s.logs = db.Collection(orDefault(config["logsCollection"], "logs"))
	s.txs = db.Collection(orDefault(config["transactionsCollection"], "transactions"))

	if _, err := s.logs.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "source_id", Value: 1}, {Key: "block_number", Value: 1}, {Key: "log_index", Value: 1}},
	}); err != nil {
		return err
	}
	if _, err := s.txs.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "source_id", Value: 1}, {Key: "block_number", Value: 1}},
	}); err != nil {
		return err
	}
	return nil
}

// --- documents ------------------------------------------------------------

type mongoMetadata struct {
	ContractName string            `bson:"contract_name"`
	EventName    string            `bson:"event_name"`
	FunctionName string            `bson:"function_name"`
	Data         map[string]string `bson:"data"`
}

type mongoLog struct {
	Id               string        `bson:"_id"`
	SourceId         uint          `bson:"source_id"`
	ChainId          uint64        `bson:"chain_id"`
	Address          string        `bson:"address"`
	Topics           []string      `bson:"topics"`
	Data             string        `bson:"data"`
	BlockNumber      uint64        `bson:"block_number"`
	BlockTimestamp   uint64        `bson:"block_timestamp"`
	TransactionFrom  string        `bson:"transaction_from"`
	TransactionHash  string        `bson:"transaction_hash"`
	TransactionIndex uint64        `bson:"transaction_index"`
	BlockHash        string        `bson:"block_hash"`
	LogIndex         uint64        `bson:"log_index"`
	Removed          bool          `bson:"removed"`
	Metadata         mongoMetadata `bson:"metadata"`
}

type mongoTx struct {
	Id               string        `bson:"_id"`
	SourceId         uint          `bson:"source_id"`
	BlockNumber      uint64        `bson:"block_number"`
	BlockTimestamp   uint64        `bson:"block_timestamp"`
	TransactionIndex uint64        `bson:"transaction_index"`
	ChainId          uint64        `bson:"chain_id"`
	From             string        `bson:"from"`
	Data             string        `bson:"data"`
	Value            string        `bson:"value"`
	Nonce            uint64        `bson:"nonce"`
	To               string        `bson:"to"`
	Hash             string        `bson:"hash"`
	Metadata         mongoMetadata `bson:"metadata"`
}

func toMongoMetadata(m types.EvmMetadata) mongoMetadata {
	return mongoMetadata{ContractName: m.ContractName, EventName: m.EventName, FunctionName: m.FunctionName, Data: m.Data}
}

func (d mongoLog) toType() types.EvmLog {
	return types.EvmLog{
		Id: d.Id, SourceId: d.SourceId, ChainId: d.ChainId, Address: d.Address, Topics: d.Topics, Data: d.Data,
		BlockNumber: d.BlockNumber, BlockTimestamp: d.BlockTimestamp, TransactionFrom: d.TransactionFrom, TransactionHash: d.TransactionHash,
		TransactionIndex: d.TransactionIndex, BlockHash: d.BlockHash, LogIndex: d.LogIndex, Removed: d.Removed,
		Metadata: types.EvmMetadata{ContractName: d.Metadata.ContractName, EventName: d.Metadata.EventName, FunctionName: d.Metadata.FunctionName, Data: d.Metadata.Data},
	}
}

func (d mongoTx) toType() types.EvmTransaction {
	return types.EvmTransaction{
		Id: d.Id, SourceId: d.SourceId, BlockNumber: d.BlockNumber, BlockTimestamp: d.BlockTimestamp, TransactionIndex: d.TransactionIndex, ChainId: d.ChainId,
		From: d.From, Data: d.Data, Value: d.Value, Nonce: d.Nonce, To: d.To, Hash: d.Hash,
		Metadata: types.EvmMetadata{ContractName: d.Metadata.ContractName, EventName: d.Metadata.EventName, FunctionName: d.Metadata.FunctionName, Data: d.Metadata.Data},
	}
}

// --- writes ---------------------------------------------------------------

func (s *MongoStore) InsertLogs(logs []types.EvmLog) error {
	if len(logs) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, len(logs))
	for i, l := range logs {
		doc := mongoLog{
			Id: l.Id, SourceId: l.SourceId, ChainId: l.ChainId, Address: l.Address, Topics: l.Topics, Data: l.Data,
			BlockNumber: l.BlockNumber, BlockTimestamp: l.BlockTimestamp, TransactionFrom: l.TransactionFrom, TransactionHash: l.TransactionHash,
			TransactionIndex: l.TransactionIndex, BlockHash: l.BlockHash, LogIndex: l.LogIndex, Removed: l.Removed,
			Metadata: toMongoMetadata(l.Metadata),
		}
		models[i] = mongo.NewReplaceOneModel().SetFilter(bson.M{"_id": l.Id}).SetReplacement(doc).SetUpsert(true)
	}
	_, err := s.logs.BulkWrite(context.Background(), models, options.BulkWrite().SetOrdered(false))
	return err
}

func (s *MongoStore) InsertTransactions(txs []types.EvmTransaction) error {
	if len(txs) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, len(txs))
	for i, t := range txs {
		doc := mongoTx{
			Id: t.Id, SourceId: t.SourceId, BlockNumber: t.BlockNumber, BlockTimestamp: t.BlockTimestamp, TransactionIndex: t.TransactionIndex, ChainId: t.ChainId,
			From: t.From, Data: t.Data, Value: t.Value, Nonce: t.Nonce, To: t.To, Hash: t.Hash, Metadata: toMongoMetadata(t.Metadata),
		}
		models[i] = mongo.NewReplaceOneModel().SetFilter(bson.M{"_id": t.Id}).SetReplacement(doc).SetUpsert(true)
	}
	_, err := s.txs.BulkWrite(context.Background(), models, options.BulkWrite().SetOrdered(false))
	return err
}

// --- reads ----------------------------------------------------------------

var sortAsc = options.Find().SetSort(bson.D{{Key: "block_number", Value: 1}, {Key: "log_index", Value: 1}})

func (s *MongoStore) GetLogsCount() (uint64, error) {
	count, err := s.logs.CountDocuments(context.Background(), bson.M{})
	return uint64(count), err
}

func (s *MongoStore) GetLogs(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmLog, error) {
	filter := bson.M{"source_id": sourceId, "block_number": bson.M{"$gte": fromBlock, "$lte": toBlock}}
	return s.findLogs(filter, sortAsc)
}

func (s *MongoStore) GetLogsAfter(sourceIds []uint64, afterBlock uint64, afterLogIndex uint64, toBlock uint64) ([]types.EvmLog, error) {
	if len(sourceIds) == 0 {
		return []types.EvmLog{}, nil
	}
	filter := bson.M{
		"source_id":    bson.M{"$in": sourceIds},
		"block_number": bson.M{"$lte": toBlock},
		"$or": []bson.M{
			{"block_number": bson.M{"$gt": afterBlock}},
			{"block_number": afterBlock, "log_index": bson.M{"$gt": afterLogIndex}},
		},
	}
	return s.findLogs(filter, sortAsc)
}

func (s *MongoStore) GetLogStream(sourceId uint64, fromBlock uint64, toBlock uint64, stream chan types.EvmLog) error {
	logs, err := s.GetLogs(sourceId, fromBlock, toBlock)
	if err != nil {
		close(stream)
		return err
	}
	for _, l := range logs {
		stream <- l
	}
	close(stream)
	return nil
}

func (s *MongoStore) GetLatestLogs(sourceId uint64, limit uint64) ([]types.EvmLog, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "block_number", Value: -1}, {Key: "log_index", Value: -1}}).
		SetLimit(int64(limit))
	return s.findLogs(bson.M{"source_id": sourceId}, opts)
}

func (s *MongoStore) GetTransactions(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error) {
	filter := bson.M{"source_id": sourceId, "block_number": bson.M{"$gte": fromBlock, "$lte": toBlock}}
	opts := options.Find().SetSort(bson.D{{Key: "block_number", Value: -1}})

	cursor, err := s.txs.Find(context.Background(), filter, opts)
	if err != nil {
		return nil, err
	}
	var docs []mongoTx
	if err := cursor.All(context.Background(), &docs); err != nil {
		return nil, err
	}
	out := make([]types.EvmTransaction, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toType())
	}
	return out, nil
}

func (s *MongoStore) findLogs(filter bson.M, opts *options.FindOptions) ([]types.EvmLog, error) {
	cursor, err := s.logs.Find(context.Background(), filter, opts)
	if err != nil {
		return nil, err
	}
	var docs []mongoLog
	if err := cursor.All(context.Background(), &docs); err != nil {
		return nil, err
	}
	out := make([]types.EvmLog, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toType())
	}
	return out, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
