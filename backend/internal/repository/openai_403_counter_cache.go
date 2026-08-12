package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	openAI403CounterPrefix = "openai_403_count:account:"
	openAI403ModelsPrefix  = "openai_403_models:account:"
)

var openAI403CounterIncrScript = redis.NewScript(`
	local key = KEYS[1]
	local ttl = tonumber(ARGV[1])

	local count = redis.call('INCR', key)
	if count == 1 then
		redis.call('EXPIRE', key, ttl)
	end

	return count
`)

var openAI403ModelRecordScript = redis.NewScript(`
	local key = KEYS[1]
	local ttl = tonumber(ARGV[1])
	local model = ARGV[2]

	local added = redis.call('SADD', key, model)
	local currentTTL = redis.call('TTL', key)
	if currentTTL < 0 then
		redis.call('EXPIRE', key, ttl)
	end

	return {redis.call('SCARD', key), added}
`)

type openAI403CounterCache struct {
	rdb *redis.Client
}

func NewOpenAI403CounterCache(rdb *redis.Client) service.OpenAI403CounterCache {
	return &openAI403CounterCache{rdb: rdb}
}

func (c *openAI403CounterCache) IncrementOpenAI403Count(ctx context.Context, accountID int64, windowMinutes int) (int64, error) {
	key := fmt.Sprintf("%s%d", openAI403CounterPrefix, accountID)

	ttlSeconds := windowMinutes * 60
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}

	result, err := openAI403CounterIncrScript.Run(ctx, c.rdb, []string{key}, ttlSeconds).Int64()
	if err != nil {
		return 0, fmt.Errorf("increment openai 403 count: %w", err)
	}
	return result, nil
}

func (c *openAI403CounterCache) RecordOpenAI403Model(ctx context.Context, accountID int64, model string, windowMinutes int) (int64, bool, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if accountID <= 0 || model == "" {
		return 0, false, nil
	}

	key := fmt.Sprintf("%s%d", openAI403ModelsPrefix, accountID)
	ttlSeconds := windowMinutes * 60
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}

	result, err := openAI403ModelRecordScript.Run(ctx, c.rdb, []string{key}, ttlSeconds, model).Slice()
	if err != nil {
		return 0, false, fmt.Errorf("record openai 403 model: %w", err)
	}
	if len(result) != 2 {
		return 0, false, fmt.Errorf("record openai 403 model: unexpected result length %d", len(result))
	}

	distinctModels, err := redisScriptInt64(result[0])
	if err != nil {
		return 0, false, fmt.Errorf("record openai 403 model count: %w", err)
	}
	added, err := redisScriptInt64(result[1])
	if err != nil {
		return 0, false, fmt.Errorf("record openai 403 model added: %w", err)
	}
	return distinctModels, added > 0, nil
}

func (c *openAI403CounterCache) ResetOpenAI403Count(ctx context.Context, accountID int64) error {
	counterKey := fmt.Sprintf("%s%d", openAI403CounterPrefix, accountID)
	modelsKey := fmt.Sprintf("%s%d", openAI403ModelsPrefix, accountID)
	return c.rdb.Del(ctx, counterKey, modelsKey).Err()
}

func redisScriptInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	case []byte:
		return strconv.ParseInt(strings.TrimSpace(string(typed)), 10, 64)
	default:
		return strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
	}
}
