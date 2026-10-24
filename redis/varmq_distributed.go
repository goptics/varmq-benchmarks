package durable

import (
	"errors"

	"github.com/goptics/redisq"
	"github.com/goptics/varmq"
)

// varmqDistributedProducer only enqueues: a redisq distributed queue wrapped by
// varmq.NewDistributedQueue, with no worker bound. Each Add is one Lua script
// that RPUSHes and then wakes up to one idle consumer per waiting item, each on
// its own wake channel (redisq v1.5.0, listPushWake in redisq/distributed.go).
func varmqDistributedProducer(url string) (enqueueFunc, func()) {
	qs := redisq.New(withPoolSize(url, poolSize(0)))
	rq := qs.NewDistributedQueue(varmqKey)
	dq := varmq.NewDistributedQueue[payload](rq)

	return func(p payload) error {
			if !dq.Add(p) {
				return errors.New("varmq: distributed queue refused the job")
			}

			return nil
		}, func() {
			_ = rq.Close() // stops the pub/sub subscription
			_ = qs.Close()
		}
}

// varmqDistributedConsumer binds a worker to the distributed queue and returns
// no enqueue: the producer is separate, and a wake on this instance's own
// pub/sub channel (<key>:wake:<id>), or redisq's 1 s nudge, is what wakes it.
// WaitAndStop lets the last acks finish before the client closes.
func varmqDistributedConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	qs := redisq.New(withPoolSize(url, poolSize(workers)))
	rq := qs.NewDistributedQueue(varmqKey)

	w := varmq.NewWorker(func(j varmq.Job[payload]) { h(j.Data()) }, workers)
	w.WithDistributedQueue(rq)

	return nil, func() {
		_ = w.WaitAndStop()
		_ = rq.Close()
		_ = qs.Close()
	}
}
