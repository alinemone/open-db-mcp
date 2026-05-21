package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// These helpers do the actual go-redis calls. They live in a separate file so
// redis_tools.go doesn't need to import go-redis (keeping the public surface
// of `tools` slim).

func castRedisClient(c any) (*redis.Client, error) {
	cli, ok := c.(*redis.Client)
	if !ok || cli == nil {
		return nil, fmt.Errorf("internal: redis client unavailable")
	}
	return cli, nil
}

func redisScan(ctx context.Context, c any, pattern string, limit int) ([]string, error) {
	cli, err := castRedisClient(c)
	if err != nil {
		return nil, err
	}
	var cursor uint64
	var keys []string
	for {
		batch, next, err := cli.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if next == 0 || len(keys) >= limit {
			break
		}
		cursor = next
	}
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys, nil
}

func redisGetAny(ctx context.Context, c any, key string) (string, any, error) {
	cli, err := castRedisClient(c)
	if err != nil {
		return "", nil, err
	}
	t, err := cli.Type(ctx, key).Result()
	if err != nil {
		return "", nil, err
	}
	switch t {
	case "string":
		v, err := cli.Get(ctx, key).Result()
		return t, v, err
	case "list":
		v, err := cli.LRange(ctx, key, 0, 50).Result()
		return t, v, err
	case "set":
		v, err := cli.SMembers(ctx, key).Result()
		return t, v, err
	case "hash":
		v, err := cli.HGetAll(ctx, key).Result()
		return t, v, err
	case "zset":
		v, err := cli.ZRangeWithScores(ctx, key, 0, 50).Result()
		return t, v, err
	case "none":
		return t, nil, fmt.Errorf("key %s does not exist", key)
	default:
		return t, nil, fmt.Errorf("unsupported type: %s", t)
	}
}

func redisInfo(ctx context.Context, c any, section string) (string, error) {
	cli, err := castRedisClient(c)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(section, "default") {
		return cli.Info(ctx).Result()
	}
	return cli.Info(ctx, section).Result()
}
