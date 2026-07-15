// Package elasticsearch_store implements the EvmIndexerStorage backend on top of
// Elasticsearch: logs and transactions are bulk-indexed as documents (keyed by
// their stable id) and queries are run as bool/range searches sorted by
// (block_number, log_index).
package elasticsearch_store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

// maxHits bounds a single search response (Elasticsearch's default max window).
const maxHits = 10000

type ElasticsearchStore struct {
	logger  zerolog.Logger
	client  *elasticsearch.Client
	logsIdx string
	txIdx   string
}

func NewElasticsearchStore(logger zerolog.Logger) (*ElasticsearchStore, error) {
	return &ElasticsearchStore{logger: logger}, nil
}

func (s *ElasticsearchStore) Init(config map[string]string) error {
	addresses := strings.Split(config["addresses"], ",")
	if config["addresses"] == "" {
		addresses = []string{"http://localhost:9200"}
	}

	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
		Username:  config["username"],
		Password:  config["password"],
	})
	if err != nil {
		return err
	}
	s.client = client

	s.logsIdx = orDefault(config["logsIndex"], "evmi_logs")
	s.txIdx = orDefault(config["transactionsIndex"], "evmi_transactions")

	if err := s.ensureIndex(s.logsIdx); err != nil {
		return err
	}
	return s.ensureIndex(s.txIdx)
}

// numericMapping keeps the queried/sorted fields as longs so range, term and
// sort behave correctly.
const numericMapping = `{"mappings":{"properties":{
  "source_id":{"type":"long"},"chain_id":{"type":"long"},"block_number":{"type":"long"},
  "log_index":{"type":"long"},"transaction_index":{"type":"long"},"nonce":{"type":"long"},
  "address":{"type":"keyword"},"transaction_hash":{"type":"keyword"},"block_hash":{"type":"keyword"},
  "hash":{"type":"keyword"},"id":{"type":"keyword"},"topics":{"type":"keyword"}
}}}`

func (s *ElasticsearchStore) ensureIndex(index string) error {
	res, err := s.client.Indices.Exists([]string{index})
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode == 200 {
		return nil
	}

	create, err := s.client.Indices.Create(index, s.client.Indices.Create.WithBody(strings.NewReader(numericMapping)))
	if err != nil {
		return err
	}
	defer create.Body.Close()
	if create.IsError() {
		body, _ := io.ReadAll(create.Body)
		return fmt.Errorf("elasticsearch: create index %s failed: %s", index, string(body))
	}
	return nil
}

// --- documents ------------------------------------------------------------

type esMetadata struct {
	ContractName string            `json:"contract_name"`
	EventName    string            `json:"event_name"`
	FunctionName string            `json:"function_name"`
	Data         map[string]string `json:"data"`
}

type esLog struct {
	Id               string     `json:"id"`
	SourceId         uint       `json:"source_id"`
	ChainId          uint64     `json:"chain_id"`
	Address          string     `json:"address"`
	Topics           []string   `json:"topics"`
	Data             string     `json:"data"`
	BlockNumber      uint64     `json:"block_number"`
	TransactionFrom  string     `json:"transaction_from"`
	TransactionHash  string     `json:"transaction_hash"`
	TransactionIndex uint64     `json:"transaction_index"`
	BlockHash        string     `json:"block_hash"`
	LogIndex         uint64     `json:"log_index"`
	Removed          bool       `json:"removed"`
	Metadata         esMetadata `json:"metadata"`
}

type esTx struct {
	Id               string     `json:"id"`
	SourceId         uint       `json:"source_id"`
	BlockNumber      uint64     `json:"block_number"`
	TransactionIndex uint64     `json:"transaction_index"`
	ChainId          uint64     `json:"chain_id"`
	From             string     `json:"from"`
	Data             string     `json:"data"`
	Value            string     `json:"value"`
	Nonce            uint64     `json:"nonce"`
	To               string     `json:"to"`
	Hash             string     `json:"hash"`
	Metadata         esMetadata `json:"metadata"`
}

func toEsMetadata(m types.EvmMetadata) esMetadata {
	return esMetadata{ContractName: m.ContractName, EventName: m.EventName, FunctionName: m.FunctionName, Data: m.Data}
}

func (d esLog) toType() types.EvmLog {
	return types.EvmLog{
		Id: d.Id, SourceId: d.SourceId, ChainId: d.ChainId, Address: d.Address, Topics: d.Topics, Data: d.Data,
		BlockNumber: d.BlockNumber, TransactionFrom: d.TransactionFrom, TransactionHash: d.TransactionHash,
		TransactionIndex: d.TransactionIndex, BlockHash: d.BlockHash, LogIndex: d.LogIndex, Removed: d.Removed,
		Metadata: types.EvmMetadata{ContractName: d.Metadata.ContractName, EventName: d.Metadata.EventName, FunctionName: d.Metadata.FunctionName, Data: d.Metadata.Data},
	}
}

func (d esTx) toType() types.EvmTransaction {
	return types.EvmTransaction{
		Id: d.Id, SourceId: d.SourceId, BlockNumber: d.BlockNumber, TransactionIndex: d.TransactionIndex, ChainId: d.ChainId,
		From: d.From, Data: d.Data, Value: d.Value, Nonce: d.Nonce, To: d.To, Hash: d.Hash,
		Metadata: types.EvmMetadata{ContractName: d.Metadata.ContractName, EventName: d.Metadata.EventName, FunctionName: d.Metadata.FunctionName, Data: d.Metadata.Data},
	}
}

// --- writes ---------------------------------------------------------------

func (s *ElasticsearchStore) InsertLogs(logs []types.EvmLog) error {
	var body bytes.Buffer
	for _, l := range logs {
		doc := esLog{
			Id: l.Id, SourceId: l.SourceId, ChainId: l.ChainId, Address: l.Address, Topics: l.Topics, Data: l.Data,
			BlockNumber: l.BlockNumber, TransactionFrom: l.TransactionFrom, TransactionHash: l.TransactionHash,
			TransactionIndex: l.TransactionIndex, BlockHash: l.BlockHash, LogIndex: l.LogIndex, Removed: l.Removed,
			Metadata: toEsMetadata(l.Metadata),
		}
		writeBulkEntry(&body, s.logsIdx, l.Id, doc)
	}
	return s.bulk(&body)
}

func (s *ElasticsearchStore) InsertTransactions(txs []types.EvmTransaction) error {
	var body bytes.Buffer
	for _, t := range txs {
		doc := esTx{
			Id: t.Id, SourceId: t.SourceId, BlockNumber: t.BlockNumber, TransactionIndex: t.TransactionIndex, ChainId: t.ChainId,
			From: t.From, Data: t.Data, Value: t.Value, Nonce: t.Nonce, To: t.To, Hash: t.Hash, Metadata: toEsMetadata(t.Metadata),
		}
		writeBulkEntry(&body, s.txIdx, t.Id, doc)
	}
	return s.bulk(&body)
}

func writeBulkEntry(body *bytes.Buffer, index, id string, doc any) {
	action, _ := json.Marshal(map[string]any{"index": map[string]any{"_index": index, "_id": id}})
	line, _ := json.Marshal(doc)
	body.Write(action)
	body.WriteByte('\n')
	body.Write(line)
	body.WriteByte('\n')
}

func (s *ElasticsearchStore) bulk(body *bytes.Buffer) error {
	if body.Len() == 0 {
		return nil
	}
	res, err := s.client.Bulk(bytes.NewReader(body.Bytes()), s.client.Bulk.WithRefresh("true"))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch bulk failed: %s", string(b))
	}
	var parsed struct {
		Errors bool `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return err
	}
	if parsed.Errors {
		return fmt.Errorf("elasticsearch bulk: one or more documents failed to index")
	}
	return nil
}

// --- reads ----------------------------------------------------------------

func (s *ElasticsearchStore) GetLogsCount() (uint64, error) {
	query := map[string]any{"size": 0, "track_total_hits": true, "query": map[string]any{"match_all": map[string]any{}}}
	total, _, err := s.searchLogs(query)
	return total, err
}

func (s *ElasticsearchStore) GetLogs(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmLog, error) {
	query := map[string]any{
		"size": maxHits,
		"sort": ascByBlockLog(),
		"query": boolFilter(
			term("source_id", sourceId),
			rangeGteLte("block_number", fromBlock, toBlock),
		),
	}
	return s.searchLogsPaged(query)
}

func (s *ElasticsearchStore) GetLogsAfter(sourceIds []uint64, afterBlock uint64, afterLogIndex uint64, toBlock uint64) ([]types.EvmLog, error) {
	if len(sourceIds) == 0 {
		return []types.EvmLog{}, nil
	}
	query := map[string]any{
		"size": maxHits,
		"sort": ascByBlockLog(),
		"query": boolFilter(
			map[string]any{"terms": map[string]any{"source_id": sourceIds}},
			map[string]any{"range": map[string]any{"block_number": map[string]any{"lte": toBlock}}},
			map[string]any{"bool": map[string]any{
				"minimum_should_match": 1,
				"should": []any{
					map[string]any{"range": map[string]any{"block_number": map[string]any{"gt": afterBlock}}},
					map[string]any{"bool": map[string]any{"must": []any{
						term("block_number", afterBlock),
						map[string]any{"range": map[string]any{"log_index": map[string]any{"gt": afterLogIndex}}},
					}}},
				},
			}},
		),
	}
	return s.searchLogsPaged(query)
}

func (s *ElasticsearchStore) GetLogStream(sourceId uint64, fromBlock uint64, toBlock uint64, stream chan types.EvmLog) error {
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

func (s *ElasticsearchStore) GetLatestLogs(sourceId uint64, limit uint64) ([]types.EvmLog, error) {
	query := map[string]any{
		"size": limit,
		"sort": []any{
			map[string]any{"block_number": "desc"},
			map[string]any{"log_index": "desc"},
		},
		"query": boolFilter(term("source_id", sourceId)),
	}
	_, logs, err := s.searchLogs(query)
	return logs, err
}

func (s *ElasticsearchStore) GetTransactions(sourceId uint64, fromBlock uint64, toBlock uint64) ([]types.EvmTransaction, error) {
	query := map[string]any{
		"size": maxHits,
		// id tie-breaks equal block_numbers so search_after pagination is stable.
		"sort": []any{map[string]any{"block_number": "desc"}, map[string]any{"id": "desc"}},
		"query": boolFilter(
			term("source_id", sourceId),
			rangeGteLte("block_number", fromBlock, toBlock),
		),
	}
	sources, err := s.searchPaged(s.txIdx, query)
	if err != nil {
		return nil, err
	}
	out := []types.EvmTransaction{}
	for _, src := range sources {
		var doc esTx
		if err := json.Unmarshal(src, &doc); err != nil {
			return nil, err
		}
		out = append(out, doc.toType())
	}
	return out, nil
}

// --- search plumbing ------------------------------------------------------

type searchResponse struct {
	Hits struct {
		Total struct {
			Value uint64 `json:"value"`
		} `json:"total"`
		Hits []struct {
			Source json.RawMessage `json:"_source"`
			Sort   []any           `json:"sort"`
		} `json:"hits"`
	} `json:"hits"`
}

func (s *ElasticsearchStore) searchLogs(query map[string]any) (uint64, []types.EvmLog, error) {
	res, err := s.search(s.logsIdx, query)
	if err != nil {
		return 0, nil, err
	}
	out := []types.EvmLog{}
	for _, hit := range res.Hits.Hits {
		var doc esLog
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			return 0, nil, err
		}
		out = append(out, doc.toType())
	}
	return res.Hits.Total.Value, out, nil
}

// searchPaged pages through every hit of a query with search_after (a single
// search response is capped at maxHits, and results beyond that were silently
// dropped before). The query's sort must be deterministic and unique.
func (s *ElasticsearchStore) searchPaged(index string, query map[string]any) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	for {
		res, err := s.search(index, query)
		if err != nil {
			return nil, err
		}
		for _, hit := range res.Hits.Hits {
			out = append(out, hit.Source)
		}
		n := len(res.Hits.Hits)
		if n < maxHits {
			return out, nil
		}
		query["search_after"] = res.Hits.Hits[n-1].Sort
	}
}

func (s *ElasticsearchStore) searchLogsPaged(query map[string]any) ([]types.EvmLog, error) {
	sources, err := s.searchPaged(s.logsIdx, query)
	if err != nil {
		return nil, err
	}
	out := []types.EvmLog{}
	for _, src := range sources {
		var doc esLog
		if err := json.Unmarshal(src, &doc); err != nil {
			return nil, err
		}
		out = append(out, doc.toType())
	}
	return out, nil
}

func (s *ElasticsearchStore) search(index string, query map[string]any) (*searchResponse, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	res, err := s.client.Search(
		s.client.Search.WithContext(context.Background()),
		s.client.Search.WithIndex(index),
		s.client.Search.WithBody(bytes.NewReader(body)),
		s.client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("elasticsearch search failed: %s", string(b))
	}
	var parsed searchResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// --- query builders -------------------------------------------------------

func boolFilter(clauses ...any) map[string]any {
	return map[string]any{"bool": map[string]any{"filter": clauses}}
}

func term(field string, value any) map[string]any {
	return map[string]any{"term": map[string]any{field: value}}
}

func rangeGteLte(field string, gte, lte uint64) map[string]any {
	return map[string]any{"range": map[string]any{field: map[string]any{"gte": gte, "lte": lte}}}
}

func ascByBlockLog() []any {
	// id tie-breaks (block_number, log_index) collisions across sources so
	// search_after pagination never skips or repeats a document.
	return []any{
		map[string]any{"block_number": "asc"},
		map[string]any{"log_index": "asc"},
		map[string]any{"id": "asc"},
	}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
