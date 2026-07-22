package indexer

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/lmittmann/w3/w3types"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
)

type SourceIndexerService struct {
	db      *evmi_database.EvmiDatabase
	bus     *bus.Bus
	metrics *metrics.MetricService

	store *log_stores.IndexerStore

	chain        evmi_database.EvmBlockchain
	pipeline     evmi_database.EvmLogPipeline
	storeInfo    evmi_database.EvmLogStore
	source       evmi_database.EvmLogSource
	contractName string
	abi          abi.ABI

	logger zerolog.Logger
}

func NewSourceIndexerService(
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	source evmi_database.EvmLogSource,
) *SourceIndexerService {

	logger := zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	return &SourceIndexerService{
		db:      db,
		bus:     bus,
		metrics: metrics,
		source:  source,
		logger:  logger,
	}
}

// registerFactoryChild creates a CONTRACT source for a contract newly deployed
// by this FACTORY source. Uniqueness is per (factory source id, child address):
// re-seeing the same deployment is a no-op. It returns an error on DB failure so
// the caller can block the factory cursor and let the supervisor retry the range
// (the child is created enabled and started best-effort via the enable topic,
// which is idempotent and also picked up on restart).
func (p *SourceIndexerService) registerFactoryChild(address string, startBlock uint64) error {
	var existing int64
	if err := p.db.Conn.Model(&evmi_database.EvmLogSource{}).
		Where("parent_source_id = ? AND address = ?", p.source.ID, address).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	child := evmi_database.EvmLogSource{
		Enabled:          true,
		Status:           string(evmi_database.StoppedLogSourceStatus),
		Type:             string(evmi_database.ContractLogSourceType),
		StartBlock:       startBlock,
		SyncBlock:        startBlock,
		Address:          sql.NullString{String: address, Valid: true},
		ParentSourceID:   p.source.ID,
		EvmLogPipelineID: p.pipeline.ID,
		EvmJsonAbiID:     uint(p.source.FactoryChildEvmJsonABI.Int32),
		EvmBlockchainID:  p.source.EvmBlockchainID,
	}
	if err := p.db.Conn.Create(&child).Error; err != nil {
		return err
	}

	p.logger.Info().Msg("registered factory child source id " + fmt.Sprint(child.ID) + " for " + address)
	// Best-effort start; the manager creates+supervises the indexer. On failure the
	// child stays enabled in DB and is started on the next manager boot.
	p.bus.Emit(context.Background(), internal_bus.EnableSourceTopic, child.ID)
	return nil
}

// registerFactoryChildren scans a batch of decoded logs for this factory's
// creation event and registers a child source for each deployed contract. A
// failure must NOT advance the cursor: the error is returned so the supervisor
// retries the range and re-attempts registration.
func (p *SourceIndexerService) registerFactoryChildren(dbLogs []types.EvmLog) error {
	for _, log := range dbLogs {
		if log.Metadata.EventName != p.source.FactoryCreationFunctionName.String {
			continue
		}

		newContractAddress, ok := log.Metadata.Data[p.source.FactoryCreationAddressLogArg.String]
		if !ok {
			logParams := map[string]interface{}{
				"transaction": log.TransactionHash,
				"logIndex":    log.LogIndex,
				"block":       log.BlockNumber,
			}

			p.logger.Error().Fields(logParams).Msg("Unable to retrieve new contract from factory log")
			continue
		}

		if err := p.registerFactoryChild(newContractAddress, log.BlockNumber); err != nil {
			p.logger.Error().Msg("factory child registration failed: " + err.Error())
			return err
		}
	}

	return nil
}

func (p *SourceIndexerService) Serve(ctx context.Context) error {

	logParams := map[string]interface{}{
		"type": p.source.Type,
	}

	p.logger.Info().Fields(logParams).Msg("starting subprocess")

	p.logger.Info().Fields(logParams).Msg("loading blockchain")
	result := p.db.Conn.First(&p.chain, p.source.EvmBlockchainID)
	if result.Error != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("loading pipeline")
	result = p.db.Conn.First(&p.pipeline, p.source.EvmLogPipelineID)
	if result.Error != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("loading store")
	result = p.db.Conn.First(&p.storeInfo, p.pipeline.EvmLogStoreId)
	if result.Error != nil {
		return result.Error
	}

	// FULL sources index every log without decoding, so they need no ABI.
	if p.source.Type != string(evmi_database.FullLogSourceType) {
		p.logger.Info().Fields(logParams).Msg("loading abi")
		var abiEntry evmi_database.EvmJsonAbi
		result = p.db.Conn.First(&abiEntry, p.source.EvmJsonAbiID)
		if result.Error != nil {
			return result.Error
		}

		abi, err := abi.JSON(strings.NewReader(abiEntry.Content))
		if err != nil {
			return err
		}

		p.contractName = abiEntry.ContractName
		p.abi = abi
	}

	p.logger.Info().Fields(logParams).Msg("loading store config")
	var storeConfig map[string]string
	err := json.Unmarshal(p.storeInfo.StoreConfig, &storeConfig)
	if err != nil {
		return err
	}

	p.logger.Info().Fields(logParams).Msg("connecting store")
	p.store, err = log_stores.LoadStore(p.storeInfo.StoreType, storeConfig, p.logger)
	if err != nil {
		p.logger.Error().Msg(err.Error())
		return err
	}

	p.logger.Info().Fields(logParams).Msg("update source")
	// Only update the columns this worker owns (status, sync_block): the row is
	// shared with the manager (enabled) and a full-row Save would write a stale
	// copy of the other columns back.
	p.source.Status = string(evmi_database.RunningLogSourceStatus)
	result = p.db.Conn.Model(&p.source).Update("status", p.source.Status)
	if result.Error != nil {
		return result.Error
	}
	p.emitSourceUpdate()

	p.logger.Info().Fields(logParams).Msg("source updates")

	// Mark the source up for the lifetime of its indexing loop.
	p.metrics.SetSourceUp(p.sourceLabels(), true)
	defer p.metrics.SetSourceUp(p.sourceLabels(), false)

	switch p.source.Type {
	case string(evmi_database.ContractLogSourceType), string(evmi_database.FactoryLogSourceType):
		return p.serveIndexation(ctx, func(fromBlock, toBlock *big.Int) ethereum.FilterQuery {
			return ethereum.FilterQuery{
				FromBlock: fromBlock,
				ToBlock:   toBlock,
				Addresses: []common.Address{common.HexToAddress(p.source.Address.String)},
			}
		})
	case string(evmi_database.TopicLogSourceType):
		return p.serveIndexation(ctx, func(fromBlock, toBlock *big.Int) ethereum.FilterQuery {
			return ethereum.FilterQuery{
				FromBlock: fromBlock,
				ToBlock:   toBlock,
				Topics:    p.topicFilters(),
			}
		})
	case string(evmi_database.FullLogSourceType):
		return p.serveIndexation(ctx, func(fromBlock, toBlock *big.Int) ethereum.FilterQuery {
			return ethereum.FilterQuery{
				FromBlock: fromBlock,
				ToBlock:   toBlock,
			}
		})
	}

	return errors.New("config types invalid")
}

// topicFilters generates a positional topic request: topics[0] is the event
// signature (topic0); topics[1..] are per-indexed-argument filters in
// declaration order. An empty filter entry is a wildcard (match any value at
// that position), represented by a nil inner slice.
func (p *SourceIndexerService) topicFilters() [][]common.Hash {
	topics := [][]common.Hash{{common.HexToHash(p.source.Topic0.String)}}
	for _, topic := range p.source.TopicFilters {
		if len(topic) == 0 {
			topics = append(topics, nil)
		} else {
			topics = append(topics, []common.Hash{common.HexToHash(topic)})
		}
	}
	return topics
}

// serveIndexation is the poll loop shared by every source type: wait for the
// chain head to move, fetch the filtered logs in BlockRange windows, decode and
// store them, advance the SyncBlock cursor. Errors are returned (never fataled)
// so the supervisor restarts this source alone — the cursor was not advanced,
// so the failed range is replayed on restart.
func (p *SourceIndexerService) serveIndexation(ctx context.Context, filter func(fromBlock, toBlock *big.Int) ethereum.FilterQuery) error {
	client, err := w3.Dial(p.chain.RpcUrl)
	if err != nil {
		return err
	}
	defer client.Close()

	latestBlockNumberIndexed := p.source.SyncBlock
	if latestBlockNumberIndexed < p.source.StartBlock {
		latestBlockNumberIndexed = p.source.StartBlock
	}

	for {
		if err := p.waitPullInterval(ctx); err != nil {
			return p.markStopped()
		}

		var block *big.Int
		if err := p.timedRPC("eth_blockNumber", func() error {
			return client.Call(eth.BlockNumber().Returns(&block))
		}); err != nil {
			return err
		}

		p.metrics.SetChainHead(p.chain.ChainId, block.Uint64())
		p.metrics.SetSourceProgress(p.sourceLabels(), block.Uint64(), p.source.SyncBlock)

		currentBlock := block.Uint64()

		// The head must be at least BlockSlice ahead of the cursor. Compared
		// without subtracting first: a lagging RPC node can report a head below
		// the cursor and the unsigned difference would wrap around.
		if currentBlock <= latestBlockNumberIndexed || currentBlock-latestBlockNumberIndexed < p.chain.BlockSlice {
			continue
		}

		for i := latestBlockNumberIndexed + 1; i <= currentBlock; i += p.chain.BlockRange {
			if ctx.Err() != nil {
				return p.markStopped()
			}

			toBlock := i + p.chain.BlockRange - 1
			if toBlock > currentBlock {
				toBlock = currentBlock
			}

			if err := p.indexRange(client, filter, i, toBlock, currentBlock); err != nil {
				return err
			}
		}

		latestBlockNumberIndexed = currentBlock
	}
}

// indexRange fetches, decodes and stores the logs of [fromBlock, toBlock], then
// advances the source cursor. The cursor is only written after a successful
// store write, so a failure anywhere replays the whole range.
func (p *SourceIndexerService) indexRange(client *w3.Client, filter func(fromBlock, toBlock *big.Int) ethereum.FilterQuery, from, to, head uint64) error {
	rangeStart := time.Now()
	fromBlock := new(big.Int).SetUint64(from)
	toBlock := new(big.Int).SetUint64(to)

	source := p.source.Address.String
	if source == "" {
		source = p.source.Topic0.String
	}

	logParams := map[string]interface{}{
		"store":     p.storeInfo.Identifier,
		"source":    source,
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
	}

	p.logger.Info().Fields(logParams).Msg("Fetch logs")

	var logs []ethTypes.Log
	if err := p.timedRPC("eth_getLogs", func() error {
		return client.Call(eth.Logs(filter(fromBlock, toBlock)).Returns(&logs))
	}); err != nil {
		p.logger.Error().Fields(logParams).Msg(err.Error())
		return err
	}

	dbLogs, dbTxs, err := p.computeLogsAndTxs(client, logs)
	if err != nil {
		p.logger.Error().Fields(logParams).Msg(err.Error())
		return err
	}

	if len(dbLogs) > 0 {
		logsStart := time.Now()
		err = p.store.GetStorage().InsertLogs(dbLogs)
		p.metrics.ObserveStoreWrite(p.storeInfo.Identifier, "logs", time.Since(logsStart), err)
		if err != nil {
			p.logger.Error().Msg(err.Error())
			return err
		}

		p.bus.Emit(context.Background(), "logs.new", dbLogs)

		//If factory mode, check if there is new contract trigger
		if p.source.Type == string(evmi_database.FactoryLogSourceType) {
			if err := p.registerFactoryChildren(dbLogs); err != nil {
				return err
			}
		}

		p.metrics.AddLogsIndexed(p.sourceLabels(), uint64(len(dbLogs)))
	}

	if len(dbTxs) > 0 {
		txStart := time.Now()
		err = p.store.GetStorage().InsertTransactions(dbTxs)
		p.metrics.ObserveStoreWrite(p.storeInfo.Identifier, "transactions", time.Since(txStart), err)
		if err != nil {
			p.logger.Error().Msg(err.Error())
			return err
		}
		p.metrics.AddTransactionsIndexed(p.sourceLabels(), uint64(len(dbTxs)))
	}

	p.source.SyncBlock = to
	tx := p.db.Conn.Model(&p.source).Update("sync_block", to)
	if tx.Error != nil {
		p.logger.Error().Msg(tx.Error.Error())
		return tx.Error
	}

	p.emitSourceUpdate()

	p.metrics.SetSourceProgress(p.sourceLabels(), head, p.source.SyncBlock)
	p.metrics.ObserveBatchDuration(p.sourceLabels(), time.Since(rangeStart))
	return nil
}

// waitPullInterval sleeps one PullInterval, returning early with the context
// error when the supervisor stops this source, so disable/shutdown doesn't wait
// a full interval.
func (p *SourceIndexerService) waitPullInterval(ctx context.Context) error {
	timer := time.NewTimer(time.Duration(p.chain.PullInterval) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// markStopped persists the STOPPED status when the supervisor cancels this
// service (source disable or shutdown).
func (p *SourceIndexerService) markStopped() error {
	p.source.Status = string(evmi_database.StoppedLogSourceStatus)
	if tx := p.db.Conn.Model(&p.source).Update("status", p.source.Status); tx.Error != nil {
		return tx.Error
	}
	p.emitSourceUpdate()
	return nil
}

// emitSourceUpdate broadcasts the source's current state (sync block, status, …)
// on the bus so subscribers (e.g. the StreamEvmLogSourceUpdates gRPC stream) can
// observe indexing progress live.
func (p *SourceIndexerService) emitSourceUpdate() {
	p.bus.Emit(context.Background(), internal_bus.SourceUpdateTopic, p.source)
}

// sourceLabels is the consistent metric label set for this source. All per-source
// metrics go through it so pipeline/store/chain/id/type are always in sync.
func (p *SourceIndexerService) sourceLabels() metrics.SourceLabels {
	return metrics.SourceLabels{
		ChainID:    p.chain.ChainId,
		Pipeline:   p.pipeline.Name,
		Store:      p.storeInfo.Identifier,
		SourceID:   p.source.ID,
		SourceType: p.source.Type,
	}
}

// timedRPC runs one JSON-RPC call, recording its latency and outcome under the
// given method name.
func (p *SourceIndexerService) timedRPC(method string, fn func() error) error {
	start := time.Now()
	err := fn()
	p.metrics.RecordRPC(p.chain.ChainId, method, time.Since(start), err)
	return err
}

func (p *SourceIndexerService) GetLogMetadata(log ethTypes.Log) types.EvmMetadata {

	// FULL sources are not decoded; anonymous events (no topic0) cannot be
	// matched against the ABI.
	if p.source.Type == string(evmi_database.FullLogSourceType) || len(log.Topics) == 0 {
		return types.EvmMetadata{
			ContractName: "Unknown",
			EventName:    "Unknown",
			Data:         map[string]string{},
		}
	}

	var eventName string = ""
	event := map[string]any{}
	for _, evInfo := range p.abi.Events {
		if evInfo.ID.Hex() != log.Topics[0].Hex() {
			continue
		}

		eventName = evInfo.RawName

		indexed := make([]abi.Argument, 0)
		for _, input := range evInfo.Inputs {
			if input.Indexed {
				indexed = append(indexed, input)
			}
		}

		// The raw topics and data are stored on the log regardless, so a decode
		// failure must not stop the source: keep whatever decoded and move on.
		if err := abi.ParseTopicsIntoMap(event, indexed, log.Topics[1:]); err != nil {
			p.logger.Warn().Str("event", eventName).Msg("topics decode failed: " + err.Error())
		}
		if err := p.abi.UnpackIntoMap(event, evInfo.Name, log.Data); err != nil {
			p.logger.Warn().Str("event", eventName).Msg("data decode failed: " + err.Error())
		}

		break
	}

	formatedEvent := map[string]string{}
	for k, v := range event {
		formatedEvent[k] = formatArgValue(v)
	}

	return types.EvmMetadata{
		ContractName: p.contractName,
		EventName:    eventName,
		Data:         formatedEvent,
	}
}

// formatArgValue renders one decoded ABI argument as a string. Byte arrays and
// slices are hex-encoded; anything without a dedicated case falls back to
// fmt.Sprint so an exotic Solidity type degrades to a readable value instead of
// stopping the source.
func formatArgValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return fmt.Sprint(val)
	case common.Address:
		return val.Hex()
	case *big.Int:
		return val.String()
	case []byte:
		return hex.EncodeToString(val)
	}

	// [N]uint8 fixed-byte arrays (bytes1..bytes32, common.Hash, …).
	rv := reflect.ValueOf(v)
	if (rv.Kind() == reflect.Array || rv.Kind() == reflect.Slice) && rv.Type().Elem().Kind() == reflect.Uint8 {
		bytes := make([]byte, rv.Len())
		for i := range bytes {
			bytes[i] = byte(rv.Index(i).Uint())
		}
		return hex.EncodeToString(bytes)
	}

	return fmt.Sprint(v)
}

func (p *SourceIndexerService) computeLogsAndTxs(client *w3.Client, logs []ethTypes.Log) ([]types.EvmLog, []types.EvmTransaction, error) {
	dbLogs := []types.EvmLog{}
	dbTxs := []types.EvmTransaction{}

	if len(logs) == 0 {
		p.logger.Info().Msg("No logs found")

		return dbLogs, dbTxs, nil
	}

	p.logger.Info().Msg(fmt.Sprintf("%d logs found", len(logs)))

	alreadyIndexedTx := make(map[string]bool)
	transactionToLoad := []common.Hash{}
	alreadyIndexedBlock := make(map[string]bool)
	blockToLoad := []common.Hash{}
	for _, log := range logs {
		txHash := log.TxHash.Hex()
		if !alreadyIndexedTx[txHash] {
			transactionToLoad = append(transactionToLoad, log.TxHash)
			alreadyIndexedTx[txHash] = true
		}
		blockHash := log.BlockHash.Hex()
		if !alreadyIndexedBlock[blockHash] {
			blockToLoad = append(blockToLoad, log.BlockHash)
			alreadyIndexedBlock[blockHash] = true
		}
	}

	// Load the block headers of the range's blocks to get their timestamps
	// (block.timestamp, not on the transaction itself), keyed by block hash.
	blockTimestamps, err := p.loadBlockTimestamps(client, blockToLoad)
	if err != nil {
		return nil, nil, err
	}

	p.logger.Info().Msg(fmt.Sprintf("%d transactions to load", len(transactionToLoad)))

	// build rpc calls
	maxBatchRequest := p.chain.RpcMaxBatchSize
	transactions := make([]*ethTypes.Transaction, len(transactionToLoad))

	var isEnded bool = false
	var currentTransactionIndex uint64 = 0
	for !isEnded {
		remainingRequests := uint64(len(transactionToLoad)) - currentTransactionIndex
		p.logger.Info().Msg(fmt.Sprintf("%d remaining requests", remainingRequests))
		var requestSize uint64
		if remainingRequests > maxBatchRequest {
			requestSize = maxBatchRequest
		} else {
			requestSize = remainingRequests
		}

		request := make([]w3types.RPCCaller, requestSize)
		for i := 0; i < int(requestSize); i++ {
			if currentTransactionIndex < uint64(len(transactionToLoad)) {
				request[i] = eth.Tx(transactionToLoad[currentTransactionIndex]).Returns(&transactions[currentTransactionIndex])
				currentTransactionIndex += 1
				continue
			}
		}

		batchStart := time.Now()
		batchCallErr := client.Call(request...)
		p.metrics.RecordRPC(p.chain.ChainId, "eth_getTransactionByHash", time.Since(batchStart), batchCallErr)
		if batchCallErr != nil {
			// A partial batch failure (w3.CallErrors) leaves nil transactions
			// behind: fail the whole range so the supervisor replays it.
			p.logger.Error().Msg(batchCallErr.Error())
			return nil, nil, batchCallErr
		}

		if currentTransactionIndex == uint64(len(transactionToLoad)) {
			isEnded = true
		}
	}

	for _, log := range logs {

		logData := map[string]interface{}{
			"LogSourceId": p.source.ID,
			"ChainId":     p.chain.ChainId,
			"BlockNumber": log.BlockNumber,

			"Address":         log.Address.Hex(),
			"TransactionHash": log.TxHash.Hex(),
			"LogIndex":        uint64(log.Index),
		}

		p.logger.Info().Fields(logData).Msg("Log found")

		var (
			transaction      *ethTypes.Transaction
			transactionIndex int
		)

		for index, tx := range transactions {
			if tx != nil && tx.Hash().Hex() == log.TxHash.Hex() {
				transaction = tx
				transactionIndex = index
			}
		}

		if transaction == nil {
			return nil, nil, errors.New("transaction not found")
		}

		sender, err := getTxSender(big.NewInt(int64(p.chain.ChainId)), transaction)
		if err != nil {
			p.logger.Error().Msg("TX: " + transaction.Hash().Hex())
			p.logger.Error().Msg(err.Error())
			return nil, nil, err
		}

		var to string
		if transaction.To() == nil {
			to = "0x0000000000000000000000000000000000000000"
		} else {
			to = transaction.To().Hex()
		}

		evmTx := types.EvmTransaction{
			Id:               fmt.Sprintf("%d:%s", transaction.ChainId().Uint64(), log.TxHash.Hex()),
			SourceId:         p.source.ID,
			BlockNumber:      log.BlockNumber,
			BlockTimestamp:   blockTimestamps[log.BlockHash.Hex()],
			ChainId:          transaction.ChainId().Uint64(),
			From:             sender,
			Data:             common.Bytes2Hex(transaction.Data()),
			Value:            transaction.Value().String(),
			TransactionIndex: uint64(transactionIndex),
			Nonce:            transaction.Nonce(),
			To:               to,
			Hash:             log.TxHash.Hex(),
		}

		dbTxs = append(dbTxs, evmTx)

		logTopics := []string{}
		for _, topic := range log.Topics {
			logTopics = append(logTopics, topic.Hex())
		}

		l := types.EvmLog{
			Id:               fmt.Sprintf("%d:%d:%d", transaction.ChainId().Uint64(), log.BlockNumber, log.Index),
			SourceId:         p.source.ID,
			Address:          log.Address.Hex(),
			Topics:           logTopics,
			Data:             common.Bytes2Hex(log.Data),
			BlockNumber:      log.BlockNumber,
			BlockTimestamp:   blockTimestamps[log.BlockHash.Hex()],
			TransactionHash:  log.TxHash.Hex(),
			TransactionFrom:  sender,
			TransactionIndex: uint64(log.TxIndex),
			BlockHash:        log.BlockHash.Hex(),
			LogIndex:         uint64(log.Index),
			Removed:          log.Removed,
			Metadata:         p.GetLogMetadata(log),
		}

		dbLogs = append(dbLogs, l)
	}

	return dbLogs, dbTxs, nil
}

// loadBlockTimestamps batch-fetches the headers of the given block hashes and
// returns a blockHash(hex) -> unix timestamp map, honoring RpcMaxBatchSize.
func (p *SourceIndexerService) loadBlockTimestamps(client *w3.Client, blockHashes []common.Hash) (map[string]uint64, error) {
	timestamps := make(map[string]uint64, len(blockHashes))
	if len(blockHashes) == 0 {
		return timestamps, nil
	}

	maxBatchRequest := p.chain.RpcMaxBatchSize
	if maxBatchRequest == 0 {
		maxBatchRequest = uint64(len(blockHashes))
	}

	headers := make([]*ethTypes.Header, len(blockHashes))
	for start := 0; start < len(blockHashes); start += int(maxBatchRequest) {
		end := start + int(maxBatchRequest)
		if end > len(blockHashes) {
			end = len(blockHashes)
		}

		request := make([]w3types.RPCCaller, 0, end-start)
		for i := start; i < end; i++ {
			request = append(request, eth.HeaderByHash(blockHashes[i]).Returns(&headers[i]))
		}

		batchStart := time.Now()
		batchCallErr := client.Call(request...)
		p.metrics.RecordRPC(p.chain.ChainId, "eth_getBlockByHash", time.Since(batchStart), batchCallErr)
		if batchCallErr != nil {
			p.logger.Error().Msg(batchCallErr.Error())
			return nil, batchCallErr
		}
	}

	for i, header := range headers {
		if header == nil {
			return nil, errors.New("block header not found for " + blockHashes[i].Hex())
		}
		timestamps[blockHashes[i].Hex()] = header.Time
	}
	return timestamps, nil
}

func getTxSender(chainId *big.Int, tx *ethTypes.Transaction) (string, error) {
	sender, err := ethTypes.Sender(ethTypes.NewPragueSigner(chainId), tx)
	if err != nil {
		return "", err
	}

	return sender.Hex(), nil
}
