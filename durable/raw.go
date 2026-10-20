package durable

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const rawKey = "bench"

func rawClient(url string, pool int) *redis.Client {
	opt, err := redis.ParseURL(url)
	if err != nil {
		panic(err) // the URL was already used by redisOrSkip
	}

	if pool > 0 {
		opt.PoolSize = pool
	}

	return redis.NewClient(opt)
}

// rawProducer LPUSHes the marshaled payload onto a plain Redis list.
func rawProducer(url string) (enqueueFunc, func()) {
	c := rawClient(url, 0)

	return func(p payload) error {
		return c.LPush(context.Background(), rawKey, p.marshal()).Err()
	}, func() { _ = c.Close() }
}

// rawConsumer runs workers goroutines that BRPOP the list and call h.
func rawConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	c := rawClient(url, poolSize(workers))
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for ctx.Err() == nil {
				res, err := c.BRPop(ctx, time.Second, rawKey).Result()
				if err != nil {
					continue // redis.Nil timeout, or cancelled: the loop condition decides
				}

				if p, err := unmarshalPayload([]byte(res[1])); err == nil {
					h(p)
				}
			}
		})
	}

	return nil, func() {
		cancel()
		_ = c.Close() // aborts in-flight BRPOPs so stop doesn't wait out the 1s block
		wg.Wait()
	}
}
