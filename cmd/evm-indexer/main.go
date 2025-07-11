package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc"
	"github.com/evmi-cloud/go-evm-indexer/internal/metrics"
	"github.com/evmi-cloud/go-evm-indexer/internal/pipeline"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Name:        "EVMI",
		Description: "EVM Indexer",
		Commands: []*cli.Command{
			{
				Name:  "start",
				Usage: "Start EVM indexer",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "instance",
						Aliases: []string{"i"},
						EnvVars: []string{"EVMI_INSTANCE_ID"},
						Value:   "EVMI_INSTANCE_1",
						Usage:   "Database location location",
					},
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						EnvVars: []string{"CONFIG_FILE_PATH"},
						Value:   "/tmp/evm-indexer/config.json",
						Usage:   "Database location location",
					},
				},
				Action: func(cCtx *cli.Context) error {

					logger := zerolog.New(
						zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
					).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

					configPath := cCtx.String("config")
					instanceId := cCtx.String("instance")

					configFile, err := os.ReadFile(configPath)
					if err != nil {
						logger.Fatal().Msg(err.Error())
					}

					var config types.Config
					err = json.Unmarshal(configFile, &config)
					if err != nil {
						logger.Fatal().Msg(err.Error())
					}

					logger.Info().Msg("Initialize Bus")
					internalBus := bus.InitializeBus()

					logger.Info().Msg("Initialize Metrics")
					metrics := metrics.NewMetricService(config.Metrics.Enabled, config.Metrics.Path, config.Metrics.Port)
					metrics.Start()

					logger.Info().Msg("Mount database")
					database, err := evmi_database.LoadDatabase(
						evmi_database.DatabaseType(config.Database.Type),
						config.Database.Config,
						logger,
					)

					if err != nil {
						return err
					}

					// if data.Backup.Enabled {
					// 	logger.Info().Msg("Mount backup service")
					// 	backups, err := backup.NewBackupService(database, data, logger)
					// 	if err != nil {
					// 		return err
					// 	}

					// 	logger.Info().Msg("Starting backup service")
					// 	backups.Start()
					// }

					// logger.Info().Msg("Mount hooks service")
					// hooks, err := hooks.NewHookService(internalBus, data, logger)
					// if err != nil {
					// 	return err
					// }

					// logger.Info().Msg("Starting hooks service")
					// hooks.Start()

					logger.Info().Msg("Mount pipeline service")
					pipelineService := pipeline.NewPipelineService(instanceId, database, internalBus, metrics, logger)

					logger.Info().Msg("Start pipeline service")
					err = pipelineService.Start()
					if err != nil {
						logger.Fatal().Msg(err.Error())
					}

					logger.Info().Msg("Start gRPC server")
					grpc.StartGrpcServer(database, internalBus, logger)
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger := zerolog.New(
			zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
		).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

		logger.Fatal().Msg(err.Error())
	}
}
