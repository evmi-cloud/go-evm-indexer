package pipeline

import (
	"bytes"
	"context"
	"encoding/hex"
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
	"github.com/evmi-cloud/go-evm-indexer/internal/database"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/lmittmann/w3/w3types"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
)

type IndexationPipelineSubprocess struct {
	db      *database.IndexerDatabase
	bus     *bus.Bus
	metrics *metrics.MetricService

	chainId uint64
	rpc     string
	abiPath string
	source  *types.LogSource
	store   *types.LogStore
	logger  *zerolog.Logger
	config  types.IndexerConfig

	abis map[string]abi.ABI

	running bool
	ended   bool
}

func NewPipelineSubrocess(
	db *database.IndexerDatabase,
	bus *bus.Bus,
	metrics *metrics.MetricService,
	rpc string,
	source *types.LogSource,
	store *types.LogStore,
	abiPath string,
	config types.IndexerConfig,
) *IndexationPipelineSubprocess {

	logger := zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	return &IndexationPipelineSubprocess{
		db:      db,
		bus:     bus,
		rpc:     rpc,
		metrics: metrics,
		source:  source,
		store:   store,
		logger:  &logger,
		running: false,
		ended:   true,
		abiPath: abiPath,
		config:  config,
	}
}

func (p *IndexationPipelineSubprocess) Serve(ctx context.Context) error {
	p.running = true

	p.abis = make(map[string]abi.ABI)

	logParams := map[string]interface{}{
		"type": p.source.Type,
	}

	for _, contract := range p.source.Contracts {
		filePath := p.abiPath + "/" + contract.ContractName + ".json"
		// path/to/abi exists
		abiFile, err := os.ReadFile(filePath)
		if err != nil {
			p.logger.Warn().Msg(filePath + " doesn't exists in abis folder")
			continue
		}

		contractAbi, err := abi.JSON(bytes.NewReader(abiFile))
		if err != nil {
			return err
		}

		p.abis[contract.ContractName] = contractAbi

		p.logger.Debug().Msg("successfully load " + contract.ContractName + " ABI")

	}

	p.logger.Info().Fields(logParams).Msg("starting subprocess")
	if p.source.Type == types.StaticPipelineConfigType {
		return p.serveStaticIndexation()
	}
	if p.source.Type == types.TopicPipelineConfigType {
		return p.serveTopicIndexation()
	}

	return errors.New("config types invalid")
}

func (p *IndexationPipelineSubprocess) Stop() {
	p.running = false
}

func (p *IndexationPipelineSubprocess) IsEnded() bool {
	return p.ended

}

func (p *IndexationPipelineSubprocess) GetLogMetadata(log ethTypes.Log) types.EvmMetadata {
	var contractName string = ""
	address := strings.ToLower(log.Address.Hex())
	for _, contract := range p.source.Contracts {
		if address == strings.ToLower(contract.Address) {
			contractName = contract.ContractName
		}
	}

	if contractName == "" {
		return types.EvmMetadata{
			ContractName: "Unknown",
			Data:         map[string]string{},
		}
	}

	contractAbi, ok := p.abis[contractName]
	if !ok {
		return types.EvmMetadata{
			ContractName: contractName,
			Data:         map[string]string{},
		}
	}

	var eventName string = ""
	event := map[string]any{}
	for _, evInfo := range contractAbi.Events {
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

		// parse topics without event name
		if err := abi.ParseTopicsIntoMap(event, indexed, log.Topics[1:]); err != nil {
			p.logger.Panic().Msg(err.Error())
		}
		// parse data
		if err := contractAbi.UnpackIntoMap(event, evInfo.Name, log.Data); err != nil {
			p.logger.Panic().Msg(err.Error())
		}

		break
	}

	formatedEvent := map[string]string{}
	for k, v := range event {
		if reflect.TypeOf(v).String() == "string" {
			formatedEvent[k] = v.(string)
			continue
		}
		if reflect.TypeOf(v).String() == "uint32" {
			formatedEvent[k] = fmt.Sprint(v.(uint32))
			continue
		}
		if reflect.TypeOf(v).String() == "uint64" {
			formatedEvent[k] = fmt.Sprint(v.(uint64))
			continue
		}
		if reflect.TypeOf(v).String() == "common.Address" {
			formatedEvent[k] = v.(common.Address).Hex()
			continue
		}
		if reflect.TypeOf(v).String() == "*big.Int" {
			formatedEvent[k] = v.(*big.Int).String()
			continue
		}
		if reflect.TypeOf(v).String() == "uint8" {
			formatedEvent[k] = fmt.Sprint(v.(uint8))
			continue
		}
		if reflect.TypeOf(v).String() == "bool" {
			formatedEvent[k] = fmt.Sprint(v.(bool))
			continue
		}

		if reflect.TypeOf(v).String() == "[4]uint8" {
			bytes := v.([4]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}
		if reflect.TypeOf(v).String() == "[8]uint8" {
			bytes := v.([8]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}
		if reflect.TypeOf(v).String() == "[12]uint8" {
			bytes := v.([12]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}
		if reflect.TypeOf(v).String() == "[16]uint8" {
			bytes := v.([16]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}
		if reflect.TypeOf(v).String() == "[20]uint8" {
			bytes := v.([20]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}
		if reflect.TypeOf(v).String() == "[24]uint8" {
			bytes := v.([24]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}
		if reflect.TypeOf(v).String() == "[28]uint8" {
			bytes := v.([28]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}

		if reflect.TypeOf(v).String() == "[32]uint8" {
			bytes := v.([32]byte)
			result := []byte{}
			for _, v := range bytes {
				result = append(result, v)
			}

			formatedEvent[k] = hex.EncodeToString(result)
			continue
		}

		if reflect.TypeOf(v).String() == "[]uint8" {
			formatedEvent[k] = hex.EncodeToString(v.([]byte))
			continue
		}

		p.logger.Panic().Msg(reflect.TypeOf(v).String() + " type not found on getLogMetadata")
	}

	return types.EvmMetadata{
		ContractName: contractName,
		EventName:    eventName,
		Data:         formatedEvent,
	}
}

func (p *IndexationPipelineSubprocess) serveStaticIndexation() error {

	// 1. Connect to an RPC endpoint
	client, err := w3.Dial(p.rpc)
	if err != nil {
		// handle error
	}
	defer client.Close()

	// 2. Make a batch request
	var (
		chainId uint64
	)

	if err := client.Call(
		eth.ChainID().Returns(&chainId),
	); err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	p.chainId = chainId
	p.metrics.EvmRpcCallMetricsInc(p.store.Identifier, p.chainId, "getChainId", 1)

	latestBlockNumberIndexed := p.source.LatestBlockIndexed
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	if latestBlockNumberIndexed < p.source.StartBlock {
		latestBlockNumberIndexed = p.source.StartBlock
	}

	addressList := []common.Address{}
	for _, c := range p.source.Contracts {
		addressList = append(addressList, common.HexToAddress(c.Address))
	}

	database, err := p.db.GetStoreDatabase()
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	for {
		if !p.running {
			p.ended = true
			return nil
		}

		time.Sleep(time.Duration(p.config.PullInterval) * time.Second)

		var (
			block *big.Int
		)
		if err := client.Call(
			eth.BlockNumber().Returns(&block),
		); err != nil {
			p.logger.Fatal().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.store.Identifier, p.chainId, "getBlockNumber", 1)
		p.metrics.LatestChainBlockMetricsSet(p.chainId, block.Uint64())
		p.metrics.LatestBlockIndexedMetricsSet(p.store.Identifier, p.source.Name, p.chainId, p.source.LatestBlockIndexed)

		currentBlock := block.Uint64()

		if currentBlock-p.source.LatestBlockIndexed < p.config.BlockSlice {
			continue
		}

		if currentBlock > latestBlockNumberIndexed {

			for i := latestBlockNumberIndexed + 1; i <= currentBlock; i += p.config.MaxBlockRange {
				fromBlock := big.NewInt(int64(i))

				var toBlock *big.Int
				if i+p.config.MaxBlockRange > currentBlock {
					toBlock = big.NewInt(int64(currentBlock))
				} else {
					toBlock = big.NewInt(int64(i + p.config.MaxBlockRange))
				}

				logParams := map[string]interface{}{
					"store":     p.source.Id,
					"source":    p.source.LogStoreId,
					"fromBlock": fromBlock,
					"toBlock":   toBlock,
				}

				p.logger.Info().Fields(logParams).Msg("Fetch logs")

				var (
					logs []ethTypes.Log
				)

				if err := client.Call(
					eth.Logs(ethereum.FilterQuery{
						FromBlock: fromBlock,
						ToBlock:   toBlock,
						Addresses: addressList,
					}).Returns(&logs),
				); err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				p.metrics.EvmRpcCallMetricsInc(p.store.Identifier, p.chainId, "getLogs", 1)

				if err != nil {
					p.logger.Fatal().Fields(logParams).Msg(err.Error())
				}

				dbLogs, dbTxs, err := p.computeLogsAndTxs(client, logs)
				if err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				if len(dbLogs) > 0 {
					err = database.InsertLogs(dbLogs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}

					p.bus.Emit(context.Background(), "logs.new", dbLogs)
					p.metrics.LogsCountMetricsAdd(p.store.Identifier, uint64(len(dbLogs)))
				}

				if len(dbTxs) > 0 {
					err = database.InsertTransactions(dbTxs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}
				}

				p.source.LatestBlockIndexed = toBlock.Uint64()
				err = database.UpdateSourceLatestBlock(p.source.Id, toBlock.Uint64())
				if err != nil {
					p.logger.Error().Msg(err.Error())
					return err
				}

				p.metrics.LatestBlockIndexedMetricsSet(p.store.Identifier, p.source.Name, p.chainId, p.source.LatestBlockIndexed)
			}

			latestBlockNumberIndexed = currentBlock
		}
	}
}

func (p *IndexationPipelineSubprocess) serveTopicIndexation() error {
	client, err := w3.Dial(p.rpc)
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}
	defer client.Close()

	var (
		chainId uint64
	)

	if err := client.Call(
		eth.ChainID().Returns(&chainId),
	); err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	p.chainId = chainId

	latestBlockNumberIndexed := p.source.LatestBlockIndexed
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	if latestBlockNumberIndexed < p.source.StartBlock {
		latestBlockNumberIndexed = p.source.StartBlock
	}

	database, err := p.db.GetStoreDatabase()
	if err != nil {
		return err
	}

	for {
		if !p.running {
			return nil
		}

		time.Sleep(time.Duration(p.config.PullInterval) * time.Second)

		var (
			block *big.Int
		)

		if err := client.Call(
			eth.BlockNumber().Returns(&block),
		); err != nil {
			p.logger.Fatal().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.store.Identifier, p.chainId, "getBlockNumber", 1)
		p.metrics.LatestChainBlockMetricsSet(p.chainId, block.Uint64())
		p.metrics.LatestBlockIndexedMetricsSet(p.store.Identifier, p.source.Name, p.chainId, p.source.LatestBlockIndexed)

		currentBlock := block.Uint64()
		if currentBlock-p.source.LatestBlockIndexed < p.config.BlockSlice {
			continue
		}

		if currentBlock > latestBlockNumberIndexed {

			for i := latestBlockNumberIndexed + 1; i <= currentBlock; i += p.config.MaxBlockRange {
				fromBlock := big.NewInt(int64(i))

				var toBlock *big.Int
				if i+p.config.MaxBlockRange > currentBlock {
					toBlock = big.NewInt(int64(currentBlock))
				} else {
					toBlock = big.NewInt(int64(i + p.config.MaxBlockRange))
				}

				logParams := map[string]interface{}{
					"store":     p.source.LogStoreId,
					"source":    p.source.Id,
					"fromBlock": fromBlock,
					"toBlock":   toBlock,
				}

				p.logger.Info().Fields(logParams).Msg("Fetch logs")

				//generate topic request
				topics := []common.Hash{common.HexToHash(p.source.Id)}
				if len(p.source.IndexedTopics) > 0 {
					for _, topic := range p.source.IndexedTopics {
						if len(topic) == 0 {
							topics = append(topics, common.Hash{})
						} else {
							topics = append(topics, common.HexToHash(topic))
						}
					}
				}

				var (
					logs []ethTypes.Log
				)

				if err := client.Call(
					eth.Logs(ethereum.FilterQuery{
						FromBlock: fromBlock,
						ToBlock:   toBlock,
						Topics:    [][]common.Hash{topics},
					}).Returns(&logs),
				); err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				p.metrics.EvmRpcCallMetricsInc(p.store.Identifier, p.chainId, "getLogs", 1)

				dbLogs, dbTxs, err := p.computeLogsAndTxs(client, logs)
				if err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				if len(dbLogs) > 0 {
					err = database.InsertLogs(dbLogs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}

					p.bus.Emit(context.Background(), "logs.new", dbLogs)
					p.metrics.LogsCountMetricsAdd(p.store.Identifier, uint64(len(dbLogs)))
					p.metrics.LogsScrapedMetricsInc(p.store.Identifier, p.chainId, uint64(len(dbLogs)))
				}

				if len(dbTxs) > 0 {
					err = database.InsertTransactions(dbTxs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}
				}

				p.source.LatestBlockIndexed = toBlock.Uint64()

				err = database.UpdateSourceLatestBlock(p.source.Id, toBlock.Uint64())
				if err != nil {
					return err
				}

				p.metrics.LatestBlockIndexedMetricsSet(p.store.Identifier, p.source.Name, p.chainId, p.source.LatestBlockIndexed)
			}

			latestBlockNumberIndexed = currentBlock
		}
	}
}

func (p *IndexationPipelineSubprocess) computeLogsAndTxs(client *w3.Client, logs []ethTypes.Log) ([]types.EvmLog, []types.EvmTransaction, error) {
	dbLogs := []types.EvmLog{}
	dbTxs := []types.EvmTransaction{}

	if len(logs) == 0 {
		return dbLogs, dbTxs, nil
	}

	alreadyIndexedTx := make(map[string]bool)
	alreadyIndexedBlockHeader := make(map[string]bool)
	transactionToLoad := []common.Hash{}
	blockHeaderToLoad := []common.Hash{}
	for _, log := range logs {
		blockHash := log.BlockHash.Hex()
		_, alreadyIndexedBlockHeader := alreadyIndexedBlockHeader[blockHash]
		if !alreadyIndexedBlockHeader {
			blockHeaderToLoad = append(blockHeaderToLoad, log.BlockHash)
		}

		txHash := log.TxHash.Hex()
		_, txAlreadyIndexed := alreadyIndexedTx[txHash]
		if !txAlreadyIndexed {
			transactionToLoad = append(transactionToLoad, log.TxHash)
		}
	}

	// build rpc calls
	maxBatchRequest := p.config.RpcMaxBatchSize
	transactions := make([]*ethTypes.Transaction, len(transactionToLoad))
	blockHeaders := make([]*ethTypes.Header, len(blockHeaderToLoad))

	var isEnded bool = false
	var currentTransactionIndex uint64 = 0
	var currentBlockIndex uint64 = 0
	for !isEnded {
		remainingRequests := uint64(len(transactionToLoad)) + uint64(len(blockHeaderToLoad)) - currentTransactionIndex - currentBlockIndex
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
			if currentBlockIndex < uint64(len(blockHeaderToLoad)) {
				request[i] = eth.HeaderByHash(blockHeaderToLoad[currentBlockIndex]).Returns(&blockHeaders[currentBlockIndex])
				currentBlockIndex += 1
				continue
			}
		}

		var batchErr w3.CallErrors
		if err := client.Call(request...); errors.As(err, &batchErr) {
			p.logger.Error().Msg(err.Error())
		} else if err != nil {
			p.logger.Error().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.store.Identifier, p.chainId, "getTransactionAndBlockHeader", 1)

		if currentTransactionIndex == uint64(len(transactionToLoad)) && currentBlockIndex == uint64(len(blockHeaderToLoad)) {
			isEnded = true
		}
	}

	for _, log := range logs {

		logData := map[string]interface{}{
			"LogSourceId": p.source.Id,
			"ChainId":     p.chainId,
			"BlockNumber": log.BlockNumber,

			"Address":         log.Address.Hex(),
			"TransactionHash": log.TxHash.Hex(),
			"LogIndex":        uint64(log.Index),
		}

		p.logger.Info().Fields(logData).Msg("Log found")

		var (
			blockHeader      *ethTypes.Header
			transaction      *ethTypes.Transaction
			transactionIndex int
		)

		for _, header := range blockHeaders {
			if header.Hash().Hex() == log.BlockHash.Hex() {
				blockHeader = header
			}
		}

		for index, tx := range transactions {
			if tx.Hash().Hex() == log.TxHash.Hex() {
				transaction = tx
				transactionIndex = index
			}
		}

		if blockHeader == nil {
			return nil, nil, errors.New("block header not found")
		}

		if transaction == nil {
			return nil, nil, errors.New("transaction not found")
		}

		sender, err := getTxSender(big.NewInt(int64(p.chainId)), transaction)
		if err != nil {
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
			StoreId:          p.source.LogStoreId,
			SourceId:         p.source.Id,
			BlockNumber:      log.BlockNumber,
			ChainId:          transaction.ChainId().Uint64(),
			From:             sender,
			Data:             common.Bytes2Hex(transaction.Data()),
			Value:            transaction.Value().String(),
			TransactionIndex: uint64(transactionIndex),
			Nonce:            transaction.Nonce(),
			To:               to,
			Hash:             log.TxHash.Hex(),
			MintedAt:         blockHeader.Time,
		}

		dbTxs = append(dbTxs, evmTx)

		logTopics := []string{}
		for _, topic := range log.Topics {
			logTopics = append(logTopics, topic.Hex())
		}

		l := types.EvmLog{
			StoreId:          p.source.LogStoreId,
			SourceId:         p.source.Id,
			Address:          log.Address.Hex(),
			Topics:           logTopics,
			Data:             common.Bytes2Hex(log.Data),
			BlockNumber:      log.BlockNumber,
			TransactionHash:  log.TxHash.Hex(),
			TransactionFrom:  sender,
			TransactionIndex: uint64(log.TxIndex),
			BlockHash:        log.BlockHash.Hex(),
			LogIndex:         uint64(log.Index),
			Removed:          log.Removed,
			MintedAt:         blockHeader.Time,
			Metadata:         p.GetLogMetadata(log),
		}

		dbLogs = append(dbLogs, l)
	}

	return dbLogs, dbTxs, nil
}

func getTxSender(chainId *big.Int, tx *ethTypes.Transaction) (string, error) {
	sender, err := ethTypes.Sender(ethTypes.NewLondonSigner(chainId), tx)
	if err != nil {
		return "", err
	}

	return sender.Hex(), nil
}
