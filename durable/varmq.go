package durable

import (
	"errors"

	"github.com/goptics/redisq"
	"github.com/goptics/varmq"
)

const varmqKey = "bench"

// varmqQueue binds a worker to a redisq-backed persistent queue. Extra worker
// options (like WithAutoRun) go after the concurrency. close releases the
// queue and its Redis client, and never waits on the worker.
func varmqQueue(url string, workers int, h handler, opts ...any) (varmq.PersistentQueue[payload], varmq.Worker, func()) {
	qs := redisq.New(withPoolSize(url, poolSize(workers)))
	rq := qs.NewQueue(varmqKey)

	w := varmq.NewWorker(func(j varmq.Job[payload]) { h(j.Data()) }, append([]any{workers}, opts...)...)
	pq := w.WithPersistentQueue(rq)

	return pq, pq.Worker(), func() {
		_ = rq.Close()
		_ = qs.Close()
	}
}

func varmqEnqueue(pq varmq.PersistentQueue[payload]) enqueueFunc {
	return func(p payload) error {
		if !pq.Add(p) {
			return errors.New("varmq: persistent queue refused the job")
		}

		return nil
	}
}

// varmqProducer only enqueues. The worker is built with WithAutoRun(false), so
// nothing is consumed.
func varmqProducer(url string) (enqueueFunc, func()) {
	pq, _, closeQueue := varmqQueue(url, 1, nil, varmq.WithAutoRun(false))

	return varmqEnqueue(pq), closeQueue
}

// varmqConsumer returns the bound queue's Add as enqueue: a persistent queue
// wakes its worker in process, so the producer must be this instance.
func varmqConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	pq, w, closeQueue := varmqQueue(url, workers, h)

	return varmqEnqueue(pq), func() {
		_ = w.WaitAndStop() // lets the last acks finish before the client closes
		closeQueue()
	}
}
