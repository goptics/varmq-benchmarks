package durable

import (
	"github.com/gocraft/work"
	"github.com/gomodule/redigo/redis"
)

const (
	gocraftNamespace = "bench"
	gocraftJob       = "bench"
)

// gocraftCtx is the context type NewWorkerPool requires; handlers don't use it.
type gocraftCtx struct{}

// gocraftPool builds the redigo pool gocraft/work needs, on its own
// connections. Wait makes Get block at MaxActive instead of failing (the
// default, which would turn into logged errors).
func gocraftPool(url string, size int) *redis.Pool {
	return &redis.Pool{
		MaxActive: size,
		MaxIdle:   size,
		Wait:      true,
		Dial:      func() (redis.Conn, error) { return redis.DialURL(url) },
	}
}

// gocraftProducer pushes one job per payload; the marshaled JSON rides as the
// string argument "p". gocraft/work logs errors with fmt.Printf to stdout
// (log.go, logError) and has no hook for it, but the enqueue path never calls
// it: errors are returned.
func gocraftProducer(url string) (enqueueFunc, func()) {
	pool := gocraftPool(url, poolSize(0))
	e := work.NewEnqueuer(gocraftNamespace, pool)

	return func(p payload) error {
			_, err := e.Enqueue(gocraftJob, work.Q{"p": string(p.marshal())})

			return err
		}, func() {
			_ = pool.Close()
		}
}

// gocraftConsumer starts one worker pool with workers worker goroutines. The
// handler returns nil, so a job is never retried.
func gocraftConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	pool := gocraftPool(url, poolSize(workers))

	wp := work.NewWorkerPool(gocraftCtx{}, uint(workers), gocraftNamespace, pool)
	wp.Job(gocraftJob, func(job *work.Job) error {
		if p, err := unmarshalPayload([]byte(job.ArgString("p"))); err == nil {
			h(p)
		}

		return nil
	})
	wp.Start()

	return nil, func() {
		// Stop hands a stop message to every worker, observer, the heartbeater,
		// both requeuers, the dead pool reaper and the periodic enqueuer, and
		// waits for each reply (worker_pool.go, Stop), so none is left running.
		// A worker finishes its current job first. The one goroutine it does not
		// wait for is the known-jobs SADD that Start launches.
		wp.Stop()
		_ = pool.Close()
	}
}
