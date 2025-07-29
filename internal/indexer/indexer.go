package indexer

import (
	"context"
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

	running bool
	ended   bool
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
		running: false,
		ended:   true,
	}
}

func (p *SourceIndexerService) Serve(ctx context.Context) error {
	p.running = true

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

	p.logger.Info().Fields(logParams).Msg("loading abi")
	var abiEntry evmi_database.EvmJsonAbi
	result = p.db.Conn.First(&abiEntry, p.source.EvmJsonAbiID)
	if result.Error != nil {
		return result.Error
	}

	abi, err := abi.JSON(strings.NewReader(abiEntry.Content))
	if err != nil {
		return result.Error
	}

	p.contractName = abiEntry.ContractName
	p.abi = abi

	p.logger.Info().Fields(logParams).Msg("loading store config")
	var storeConfig map[string]string
	err = json.Unmarshal(p.storeInfo.StoreConfig, &storeConfig)
	if err != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("connecting store")
	p.store, err = log_stores.LoadStore(p.storeInfo.StoreType, storeConfig, p.logger)
	if err != nil {
		p.logger.Error().Msg(err.Error())
		return err
	}

	p.logger.Info().Fields(logParams).Msg("update source")
	p.source.Status = string(evmi_database.RunningLogSourceStatus)
	result = p.db.Conn.Save(&p.source)
	if result.Error != nil {
		return result.Error
	}

	p.logger.Info().Fields(logParams).Msg("source updates")

	if p.source.Type == string(evmi_database.ContractLogSourceType) || p.source.Type == string(evmi_database.FactoryLogSourceType) {
		return p.serveStaticIndexation()
	}
	if p.source.Type == string(evmi_database.TopicLogSourceType) {
		return p.serveTopicIndexation()
	}
	if p.source.Type == string(evmi_database.FullLogSourceType) {
		return p.serveFullIndexation()
	}

	return errors.New("config types invalid")
}

func (p *SourceIndexerService) Stop() error {
	p.running = false

	for !p.IsEnded() {
		time.Sleep(time.Second)
	}

	return nil
}

func (p *SourceIndexerService) IsEnded() bool {
	return p.ended
}

func (p *SourceIndexerService) GetLogMetadata(log ethTypes.Log) types.EvmMetadata {

	if p.source.Type == string(evmi_database.FullLogSourceType) {
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

		// parse topics without event name
		if err := abi.ParseTopicsIntoMap(event, indexed, log.Topics[1:]); err != nil {
			p.logger.Panic().Msg(err.Error())
		}
		// parse data
		if err := p.abi.UnpackIntoMap(event, evInfo.Name, log.Data); err != nil {
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
		ContractName: p.contractName,
		EventName:    eventName,
		Data:         formatedEvent,
	}
}

func (p *SourceIndexerService) serveFullIndexation() error {

	// 1. Connect to an RPC endpoint
	client, err := w3.Dial(p.chain.RpcUrl)
	if err != nil {
		// handle error
	}

	defer client.Close()

	// 2. Make a batch request
	p.metrics.EvmRpcCallMetricsInc(p.pipeline.Name, p.chain.ChainId, "getChainId", 1)

	latestBlockNumberIndexed := p.source.SyncBlock
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	if latestBlockNumberIndexed < p.source.StartBlock {
		latestBlockNumberIndexed = p.source.StartBlock
	}

	for {
		if !p.running {
			p.source.Status = "STOPPED"
			result := p.db.Conn.Save(&p.source.Status)
			if result.Error != nil {
				return result.Error
			}

			p.ended = true
			return nil
		}

		time.Sleep(time.Duration(p.chain.PullInterval) * time.Second)

		var (
			block *big.Int
		)
		if err := client.Call(
			eth.BlockNumber().Returns(&block),
		); err != nil {
			p.logger.Fatal().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.pipeline.Name, p.chain.ChainId, "getBlockNumber", 1)
		p.metrics.LatestChainBlockMetricsSet(p.chain.ChainId, block.Uint64())
		p.metrics.LatestBlockIndexedMetricsSet(p.pipeline.Name, p.source.Address.String, p.chain.ChainId, p.source.SyncBlock)

		currentBlock := block.Uint64()

		if currentBlock-p.source.SyncBlock < p.chain.BlockSlice {
			continue
		}

		if currentBlock > latestBlockNumberIndexed {

			for i := latestBlockNumberIndexed + 1; i <= currentBlock; i += p.chain.BlockRange {
				fromBlock := big.NewInt(int64(i))

				var toBlock *big.Int
				if i+p.chain.BlockRange > currentBlock {
					toBlock = big.NewInt(int64(currentBlock))
				} else {
					toBlock = big.NewInt(int64(i + p.chain.BlockRange))
				}

				logParams := map[string]interface{}{
					"store":     p.storeInfo.Identifier,
					"source":    p.source.Address.String,
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
					}).Returns(&logs),
				); err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				p.metrics.EvmRpcCallMetricsInc(p.storeInfo.Identifier, p.chain.ChainId, "getLogs", 1)

				if err != nil {
					p.logger.Fatal().Fields(logParams).Msg(err.Error())
				}

				dbLogs, dbTxs, err := p.computeLogsAndTxs(client, logs)
				if err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				if len(dbLogs) > 0 {
					err = p.store.GetStorage().InsertLogs(dbLogs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}

					p.bus.Emit(context.Background(), "logs.new", dbLogs)

					//If factory mode, check if there is new contract trigger
					if p.source.Type == string(evmi_database.FactoryLogSourceType) {
						for _, log := range dbLogs {
							if log.Metadata.FunctionName == p.source.FactoryCreationFunctionName.String {
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

								event := ContractFromFactory{
									Type:             evmi_database.ContractLogSourceType,
									StartBlock:       log.BlockNumber,
									Address:          newContractAddress,
									EvmLogPipelineID: p.pipeline.ID,
									EvmJsonAbiID:     uint(p.source.FactoryChildEvmJsonABI.Int32),
								}

								p.bus.Emit(context.Background(), "logs.newFactoryItem", event)
							}
						}
					}

					p.metrics.LogsCountMetricsAdd(p.storeInfo.Identifier, uint64(len(dbLogs)))
				}

				if len(dbTxs) > 0 {
					err = p.store.GetStorage().InsertTransactions(dbTxs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}
				}

				p.source.SyncBlock = toBlock.Uint64()

				tx := p.db.Conn.Save(p.source)
				if tx.Error != nil {
					p.logger.Error().Msg(err.Error())
					return err
				}

				p.metrics.LatestBlockIndexedMetricsSet(p.pipeline.Name, p.source.Address.String, p.chain.ChainId, p.source.SyncBlock)
			}

			latestBlockNumberIndexed = currentBlock
		}
	}
}

func (p *SourceIndexerService) serveStaticIndexation() error {

	// 1. Connect to an RPC endpoint
	client, err := w3.Dial(p.chain.RpcUrl)
	if err != nil {
		// handle error
	}

	defer client.Close()

	// 2. Make a batch request
	p.metrics.EvmRpcCallMetricsInc(p.pipeline.Name, p.chain.ChainId, "getChainId", 1)

	latestBlockNumberIndexed := p.source.SyncBlock
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	if latestBlockNumberIndexed < p.source.StartBlock {
		latestBlockNumberIndexed = p.source.StartBlock
	}

	for {
		if !p.running {
			p.ended = true
			return nil
		}

		time.Sleep(time.Duration(p.chain.PullInterval) * time.Second)

		var (
			block *big.Int
		)
		if err := client.Call(
			eth.BlockNumber().Returns(&block),
		); err != nil {
			p.logger.Fatal().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.pipeline.Name, p.chain.ChainId, "getBlockNumber", 1)
		p.metrics.LatestChainBlockMetricsSet(p.chain.ChainId, block.Uint64())
		p.metrics.LatestBlockIndexedMetricsSet(p.pipeline.Name, p.source.Address.String, p.chain.ChainId, p.source.SyncBlock)

		currentBlock := block.Uint64()

		if currentBlock-p.source.SyncBlock < p.chain.BlockSlice {
			continue
		}

		if currentBlock > latestBlockNumberIndexed {

			for i := latestBlockNumberIndexed + 1; i <= currentBlock; i += p.chain.BlockRange {
				fromBlock := big.NewInt(int64(i))

				var toBlock *big.Int
				if i+p.chain.BlockRange > currentBlock {
					toBlock = big.NewInt(int64(currentBlock))
				} else {
					toBlock = big.NewInt(int64(i + p.chain.BlockRange))
				}

				logParams := map[string]interface{}{
					"store":     p.storeInfo.Identifier,
					"source":    p.source.Address.String,
					"fromBlock": fromBlock,
					"toBlock":   toBlock,
					"rpc":       p.chain.RpcUrl,
				}

				p.logger.Info().Fields(logParams).Msg("Fetch logs")

				var (
					logs []ethTypes.Log
				)

				if err := client.Call(
					eth.Logs(ethereum.FilterQuery{
						FromBlock: fromBlock,
						ToBlock:   toBlock,
						Addresses: []common.Address{common.HexToAddress(p.source.Address.String)},
					}).Returns(&logs),
				); err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				p.logger.Info().Fields(logParams).Msg("Log fetched")

				p.metrics.EvmRpcCallMetricsInc(p.storeInfo.Identifier, p.chain.ChainId, "getLogs", 1)

				if err != nil {
					p.logger.Fatal().Fields(logParams).Msg(err.Error())
				}

				p.logger.Info().Fields(logParams).Msg("Computing logs")
				dbLogs, dbTxs, err := p.computeLogsAndTxs(client, logs)
				if err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				if len(dbLogs) > 0 {
					err = p.store.GetStorage().InsertLogs(dbLogs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}

					p.bus.Emit(context.Background(), "logs.new", dbLogs)

					//If factory mode, check if there is new contract trigger
					if p.source.Type == string(evmi_database.FactoryLogSourceType) {
						for _, log := range dbLogs {
							if log.Metadata.FunctionName == p.source.FactoryCreationFunctionName.String {
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

								event := ContractFromFactory{
									Type:             evmi_database.ContractLogSourceType,
									StartBlock:       log.BlockNumber,
									Address:          newContractAddress,
									EvmLogPipelineID: p.pipeline.ID,
									EvmJsonAbiID:     uint(p.source.FactoryChildEvmJsonABI.Int32),
								}

								p.bus.Emit(context.Background(), "logs.newFactoryItem", event)
							}
						}
					}

					p.metrics.LogsCountMetricsAdd(p.storeInfo.Identifier, uint64(len(dbLogs)))
				}

				if len(dbTxs) > 0 {
					err = p.store.GetStorage().InsertTransactions(dbTxs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}
				}

				p.source.SyncBlock = toBlock.Uint64()

				tx := p.db.Conn.Save(p.source)
				if tx.Error != nil {
					p.logger.Error().Msg(err.Error())
					return err
				}

				p.metrics.LatestBlockIndexedMetricsSet(p.pipeline.Name, p.source.Address.String, p.chain.ChainId, p.source.SyncBlock)
			}

			latestBlockNumberIndexed = currentBlock
		}
	}
}

func (p *SourceIndexerService) serveTopicIndexation() error {
	client, err := w3.Dial(p.chain.RpcUrl)
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}
	defer client.Close()

	latestBlockNumberIndexed := p.source.SyncBlock
	if err != nil {
		p.logger.Fatal().Msg(err.Error())
	}

	if latestBlockNumberIndexed < p.source.StartBlock {
		latestBlockNumberIndexed = p.source.StartBlock
	}

	for {
		if !p.running {
			return nil
		}

		time.Sleep(time.Duration(p.chain.PullInterval) * time.Second)

		var (
			block *big.Int
		)

		if err := client.Call(
			eth.BlockNumber().Returns(&block),
		); err != nil {
			p.logger.Fatal().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.storeInfo.Identifier, p.chain.ChainId, "getBlockNumber", 1)
		p.metrics.LatestChainBlockMetricsSet(p.chain.ChainId, block.Uint64())
		p.metrics.LatestBlockIndexedMetricsSet(p.storeInfo.Identifier, p.source.Topic0.String, p.chain.ChainId, p.source.SyncBlock)

		currentBlock := block.Uint64()
		if currentBlock-latestBlockNumberIndexed < p.chain.BlockSlice {
			continue
		}

		if currentBlock > latestBlockNumberIndexed {

			for i := latestBlockNumberIndexed + 1; i <= currentBlock; i += p.chain.BlockRange {
				fromBlock := big.NewInt(int64(i))

				var toBlock *big.Int
				if i+p.chain.BlockRange > currentBlock {
					toBlock = big.NewInt(int64(currentBlock))
				} else {
					toBlock = big.NewInt(int64(i + p.chain.BlockRange))
				}

				logParams := map[string]interface{}{
					"store":     p.storeInfo.Identifier,
					"source":    p.source.Topic0.String,
					"fromBlock": fromBlock,
					"toBlock":   toBlock,
				}

				p.logger.Info().Fields(logParams).Msg("Fetch logs")

				//generate topic request
				topics := []common.Hash{common.HexToHash(p.source.Topic0.String)}
				if len(p.source.TopicFilters) > 0 {
					for _, topic := range p.source.TopicFilters {
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

				p.metrics.EvmRpcCallMetricsInc(p.storeInfo.Identifier, p.chain.ChainId, "getLogs", 1)

				dbLogs, dbTxs, err := p.computeLogsAndTxs(client, logs)
				if err != nil {
					p.logger.Fatal().Msg(err.Error())
				}

				if len(dbLogs) > 0 {
					err = p.store.GetStorage().InsertLogs(dbLogs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}

					p.bus.Emit(context.Background(), "logs.new", dbLogs)
					p.metrics.LogsCountMetricsAdd(p.storeInfo.Identifier, uint64(len(dbLogs)))
					p.metrics.LogsScrapedMetricsInc(p.storeInfo.Identifier, p.chain.ChainId, uint64(len(dbLogs)))
				}

				if len(dbTxs) > 0 {
					err = p.store.GetStorage().InsertTransactions(dbTxs)
					if err != nil {
						p.logger.Error().Msg(err.Error())
						return err
					}
				}

				p.source.SyncBlock = toBlock.Uint64()

				tx := p.db.Conn.Save(p.source)
				if tx.Error != nil {
					p.logger.Error().Msg(err.Error())
					return err
				}

				p.metrics.LatestBlockIndexedMetricsSet(p.pipeline.Name, p.source.Address.String, p.chain.ChainId, p.source.SyncBlock)
			}

			latestBlockNumberIndexed = currentBlock
		}
	}
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
	for _, log := range logs {
		txHash := log.TxHash.Hex()
		_, txAlreadyIndexed := alreadyIndexedTx[txHash]
		if !txAlreadyIndexed {
			transactionToLoad = append(transactionToLoad, log.TxHash)
			alreadyIndexedTx[txHash] = true
		}
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

		var batchErr w3.CallErrors
		if err := client.Call(request...); errors.As(err, &batchErr) {
			p.logger.Error().Msg(err.Error())
		} else if err != nil {
			p.logger.Error().Msg(err.Error())
		}

		p.metrics.EvmRpcCallMetricsInc(p.storeInfo.Identifier, p.chain.ChainId, "getTransactionAndBlockHeader", 1)

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
			if tx.Hash().Hex() == log.TxHash.Hex() {
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

func getTxSender(chainId *big.Int, tx *ethTypes.Transaction) (string, error) {
	sender, err := ethTypes.Sender(ethTypes.NewPragueSigner(chainId), tx)
	if err != nil {
		return "", err
	}

	return sender.Hex(), nil
}
