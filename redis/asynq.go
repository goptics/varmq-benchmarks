package durable

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	asynqQueue    = "bench"
	asynqTaskType = "bench"
)

// asynqSilent drops every asynq log line. The server also gets LogLevel
// FatalLevel, so most calls never reach it.
type asynqSilent struct{}

func (asynqSilent) Debug(...any) {}
func (asynqSilent) Info(...any)  {}
func (asynqSilent) Warn(...any)  {}
func (asynqSilent) Error(...any) {}
func (asynqSilent) Fatal(...any) {} // must not exit the process

func asynqOpt(url string, workers int) asynq.RedisClientOpt {
	o, err := redis.ParseURL(url)
	if err != nil {
		panic(err) // the URL was already used by redisOrSkip
	}

	return asynq.RedisClientOpt{
		Addr:      o.Addr,
		DB:        o.DB,
		Username:  o.Username,
		Password:  o.Password,
		TLSConfig: o.TLSConfig,
		PoolSize:  poolSize(workers),
	}
}

// asynqProducer enqueues one task per payload on the "bench" queue.
func asynqProducer(url string) (enqueueFunc, func()) {
	c := asynq.NewClient(asynqOpt(url, 0))

	return func(p payload) error {
		_, err := c.Enqueue(asynq.NewTask(asynqTaskType, p.marshal()), asynq.Queue(asynqQueue))

		return err
	}, func() { _ = c.Close() }
}

// asynqConsumer runs an asynq server with workers as its concurrency. Shutdown
// waits for in-flight handlers and closes the server's Redis client.
func asynqConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	srv := asynq.NewServer(asynqOpt(url, workers), asynq.Config{
		Concurrency: workers,
		Queues:      map[string]int{asynqQueue: 1},
		Logger:      asynqSilent{},
		LogLevel:    asynq.FatalLevel,
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(asynqTaskType, func(_ context.Context, t *asynq.Task) error {
		p, err := unmarshalPayload(t.Payload())
		if err != nil {
			return err
		}

		h(p)

		return nil
	})

	if err := srv.Start(mux); err != nil {
		panic(err) // only fails on a nil handler or a reused server
	}

	return nil, srv.Shutdown
}
