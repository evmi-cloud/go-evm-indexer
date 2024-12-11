package hooks

import (
	"context"

	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
	redispubsub "github.com/evmi-cloud/go-evm-indexer/internal/hooks/redis-pubsub"
	"github.com/google/uuid"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/zerolog"
)

type HookService struct {
	logger zerolog.Logger
	bus    *bus.Bus

	hooks []EvmIndexerHook
}

func (h *HookService) Start() {
	handlerId := uuid.New()
	h.bus.RegisterHandler(handlerId.String(), bus.Handler{
		Handle: func(ctx context.Context, e bus.Event) {
			logs := e.Data.([]models.EvmLog)

			for _, hook := range h.hooks {
				hook.PublishNewLogs(logs)
			}
		},
		Matcher: "logs.new",
	})

	h.logger.Info().Msg("Hook service started")
}

func NewHookService(bus *bus.Bus, config models.Config, logger zerolog.Logger) (*HookService, error) {

	service := &HookService{
		bus: bus,
	}

	service.hooks = make([]EvmIndexerHook, len(config.Hooks))
	for i, hookConfig := range config.Hooks {
		if hookConfig.Type == "redis-pubsub" {
			service.hooks[i] = redispubsub.NewRedisPubsubHook()
			err := service.hooks[i].Init(config, uint64(i))
			if err != nil {
				return nil, err
			}
		}
	}

	return service, nil
}
