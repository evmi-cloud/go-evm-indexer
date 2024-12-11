package redispubsub

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/evmi-cloud/go-evm-indexer/internal/database/models"
	"github.com/redis/go-redis/v9"
)

type RedisPubsubHook struct {
	redis   *redis.Client
	url     string
	channel string
}

func (h RedisPubsubHook) Init(config models.Config, index uint64) error {

	conf := config.Hooks[index]
	if conf.Type != "redis-pubsub" {
		return errors.New("bad configuration type")
	}

	url, ok := conf.Config["url"]
	if !ok {
		return errors.New("url not specified in config")
	}

	opt, err := redis.ParseURL(url)
	if err != nil {
		return err
	}

	h.redis = redis.NewClient(opt)

	return nil
}

func (h RedisPubsubHook) PublishNewLogs(logs []models.EvmLog) error {

	jsonPayload, err := json.Marshal(logs)
	if err != nil {
		return err
	}

	err = h.redis.Publish(context.Background(), h.channel, jsonPayload).Err()
	if err != nil {
		return err
	}

	return nil
}

func NewRedisPubsubHook() RedisPubsubHook {
	return RedisPubsubHook{}
}
