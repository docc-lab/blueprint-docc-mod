// Package redis implements a key-value [backend.Cache] client interface to a vanilla redis implementation.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	redis_impl "github.com/go-redis/redis/v8"
)

// A redis client wrapper that implements the [backend.Cache] interface
type RedisCache struct {
	client *redis_impl.Client
}

// Instantiates a new redis client to a memcached instance running at `serverAddress`
func NewRedisCacheClient(ctx context.Context, addr string) (*RedisCache, error) {
	conn_addr := addr
	opts := &redis_impl.Options{
		Addr:     conn_addr,
		Password: "",
		DB:       0,
	}
	client := redis_impl.NewClient(opts)

	// Log connection pool configuration
	// go-redis default PoolSize is 10 * runtime.NumCPU() if PoolSize is 0
	fmt.Fprintf(os.Stderr, "[REDIS] Client created for %s: PoolSize=%d (0 means default: 10*CPU), MinIdleConns=%d, MaxRetries=%d\n",
		addr, opts.PoolSize, opts.MinIdleConns, opts.MaxRetries)
	fmt.Printf("[REDIS] Client created for %s: PoolSize=%d (0 means default: 10*CPU), MinIdleConns=%d, MaxRetries=%d\n",
		addr, opts.PoolSize, opts.MinIdleConns, opts.MaxRetries)

	// Log actual pool stats (may show 0 initially as pool initializes lazily)
	stats := client.PoolStats()
	fmt.Fprintf(os.Stderr, "[REDIS] Pool stats for %s: TotalConns=%d, IdleConns=%d, StaleConns=%d, Hits=%d, Misses=%d\n",
		addr, stats.TotalConns, stats.IdleConns, stats.StaleConns, stats.Hits, stats.Misses)
	fmt.Printf("[REDIS] Pool stats for %s: TotalConns=%d, IdleConns=%d, StaleConns=%d, Hits=%d, Misses=%d\n",
		addr, stats.TotalConns, stats.IdleConns, stats.StaleConns, stats.Hits, stats.Misses)

	return &RedisCache{client: client}, nil
}

// Implements the backend.Cache interface
func (r *RedisCache) Put(ctx context.Context, key string, value interface{}) error {
	val, err := json.Marshal(value)
	if err != nil {
		return err
	}
	val_str := string(val)
	return r.client.Set(ctx, key, val_str, 0).Err()
}

// Implements the backend.Cache interface
func (r *RedisCache) Get(ctx context.Context, key string, value interface{}) (bool, error) {
	// Log on first Get to confirm Redis is being used and show pool stats
	stats := r.client.PoolStats()
	if stats.Hits+stats.Misses == 0 {
		fmt.Printf("[REDIS] First Get() call - Pool stats: TotalConns=%d, IdleConns=%d\n", stats.TotalConns, stats.IdleConns)
	}
	val, err := r.client.Get(ctx, key).Result()
	if err == redis_impl.Nil {
		// Key doesn't exist
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(val), value)
}

// Implements the backend.Cache interface
func (r *RedisCache) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// Implements the backend.Cache interface
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Implements the backend.Cache interface
func (r *RedisCache) Mget(ctx context.Context, keys []string, values []interface{}) error {
	result, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return err
	}
	for idx, res := range result {
		// MGet returns nil for keys that don't exist - skip those
		if res == nil {
			continue
		}
		// Convert to string (Redis MGet returns interface{} which could be string or nil)
		resStr, ok := res.(string)
		if !ok {
			// If it's not a string and not nil, try to convert it
			continue
		}
		err := json.Unmarshal([]byte(resStr), values[idx])
		if err != nil {
			return err
		}
	}
	return nil
}

// Implements the backend.Cache interface
func (r *RedisCache) Mset(ctx context.Context, keys []string, values []interface{}) error {
	kv_map := make(map[string]string)
	for idx, key := range keys {
		val, err := json.Marshal(values[idx])
		if err != nil {
			return err
		}
		kv_map[key] = string(val)
	}
	return r.client.MSet(ctx, kv_map).Err()
}

// ZAdd adds a member with score to a sorted set.
// This is a Redis-specific operation that enables efficient timeline operations.
// Key: the sorted set key (e.g., user ID)
// Score: the score for sorting (e.g., timestamp)
// Member: the value to store (e.g., post ID as string)
func (r *RedisCache) ZAdd(ctx context.Context, key string, score float64, member string) error {
	return r.client.ZAdd(ctx, key, &redis_impl.Z{
		Score:  score,
		Member: member,
	}).Err()
}

// ZRevRange returns a range of members from a sorted set, ordered from highest to lowest score.
// start and stop are zero-based indices. Use -1 for stop to get all remaining members.
// Returns the member strings (e.g., post IDs as strings).
func (r *RedisCache) ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.client.ZRevRange(ctx, key, start, stop).Result()
}

// ZAddBatch adds multiple members to sorted sets using Redis pipelining.
// This batches all ZAdd operations into a single network round-trip, much more efficient than sequential or parallel individual calls.
// operations is a slice of {key, score, member} tuples.
// Returns an error if any operation fails.
func (r *RedisCache) ZAddBatch(ctx context.Context, operations []struct {
	Key    string
	Score  float64
	Member string
}) error {
	pipe := r.client.Pipeline()
	for _, op := range operations {
		pipe.ZAdd(ctx, op.Key, &redis_impl.Z{
			Score:  op.Score,
			Member: op.Member,
		})
	}
	_, err := pipe.Exec(ctx)
	return err
}
