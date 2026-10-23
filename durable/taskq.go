package durable

import (
	"context"
	"io"
	"log"
	"sync/atomic"

	redis8 "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/taskq/v3"
	taskqredis "github.com/vmihailenco/taskq/v3/redisq"
)

const (
	taskqQueue    = "bench"
	taskqTaskName = "bench"
)

// redis8Silent drops go-redis v8's internal log lines (pool and connection
// errors after a client is closed).
type redis8Silent struct{}

func (redis8Silent) Printf(context.Context, string, ...any) {}

func init() {
	// taskq and go-redis v8 each log to stderr from package-level loggers.
	taskq.SetLogger(log.New(io.Discard, "", 0))
	redis8.SetLogger(redis8Silent{})
}

// taskqHandler is the handler of the current consumer round. taskq's task
// registry is global and a second RegisterTask of the same name panics, so the
// task is registered once below and routes to whatever handler is current. All
// instances of one run share one handler, and the harness stops a round's
// instances before the next round starts.
var taskqHandler atomic.Pointer[handler]

var _ = taskq.RegisterTask(&taskq.TaskOptions{
	Name: taskqTaskName,
	Handler: func(b []byte) error {
		// Always nil: a returned error would make taskq retry the job.
		if h := taskqHandler.Load(); h != nil {
			if p, err := unmarshalPayload(b); err == nil {
				(*h)(p)
			}
		}

		return nil
	},
})

// taskqClient builds the go-redis v8 client taskq needs, on its own pool.
func taskqClient(url string, pool int) *redis8.Client {
	opt, err := redis8.ParseURL(url)
	if err != nil {
		panic(err) // the URL was already used by redisOrSkip
	}

	if pool > 0 {
		opt.PoolSize = pool
	}

	return redis8.NewClient(opt)
}

// taskqOpen registers the "bench" queue on a factory of its own (queue names
// are unique per factory), so each instance closes only its own queue.
// NewQueue starts three scheduler goroutines (redisq/queue.go); they are not
// waited on by Close, but they exit at their next wake once the queue is
// closed, and every Redis call they make after the client closes just fails.
func taskqOpen(url string, pool int, workers int32) (taskq.Queue, taskq.Factory, *redis8.Client) {
	client := taskqClient(url, pool)
	f := taskqredis.NewFactory()
	q := f.RegisterQueue(&taskq.QueueOptions{
		Name:         taskqQueue,
		Redis:        client,
		MinNumWorker: workers,
		MaxNumWorker: workers,
	})

	return q, f, client
}

// taskqProducer adds one message per payload, args being the marshaled JSON.
func taskqProducer(url string) (enqueueFunc, func()) {
	q, f, client := taskqOpen(url, 0, 0)
	task := taskq.Tasks.Get(taskqTaskName)

	return func(p payload) error {
			return q.Add(task.WithArgs(context.Background(), p.marshal()))
		}, func() {
			_ = f.Close()
			_ = client.Close()
		}
}

// taskqConsumer starts one taskq consumer with workers goroutines: min and max
// worker counts are both pinned to workers, which also turns off taskq's
// autotuning (consumer.go, Start).
func taskqConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	taskqHandler.Store(&h)

	q, f, client := taskqOpen(url, poolSize(workers), int32(workers))

	ctx, cancel := context.WithCancel(context.Background())
	if err := q.Consumer().Start(ctx); err != nil {
		panic(err)
	}

	return nil, func() {
		// Stop waits for the fetcher (up to one WaitTimeout block) and for the
		// workers. Closing the factory then closes the queue, and only after
		// that the client.
		_ = q.Consumer().Stop()
		_ = f.Close()
		cancel()
		_ = client.Close()
	}
}
