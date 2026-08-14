package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOpenAI403CounterCache_RecordModelsAndReset(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	cache := &openAI403CounterCache{rdb: client}
	ctx := context.Background()
	accountID := int64(42)

	distinct, added, err := cache.RecordOpenAI403Model(ctx, accountID, "GPT-5.6-TERRA", 180)
	require.NoError(t, err)
	require.Equal(t, int64(1), distinct)
	require.True(t, added)

	distinct, added, err = cache.RecordOpenAI403Model(ctx, accountID, "gpt-5.6-terra", 180)
	require.NoError(t, err)
	require.Equal(t, int64(1), distinct)
	require.False(t, added)

	distinct, added, err = cache.RecordOpenAI403Model(ctx, accountID, "gpt-5.5", 180)
	require.NoError(t, err)
	require.Equal(t, int64(2), distinct)
	require.True(t, added)

	_, err = cache.IncrementOpenAI403Count(ctx, accountID, 180)
	require.NoError(t, err)
	require.NoError(t, cache.ResetOpenAI403Count(ctx, accountID))
	require.False(t, server.Exists(fmt.Sprintf("%s%d", openAI403CounterPrefix, accountID)))
	require.False(t, server.Exists(fmt.Sprintf("%s%d", openAI403ModelsPrefix, accountID)))
}
