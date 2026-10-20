package durable

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"runtime"
	"sync/atomic"
	"time"
)

// payload is the job body every subject carries through Redis.
type payload struct {
	ID         int64    `json:"id"`
	EnqueuedAt int64    `json:"t"`
	Pad        [48]byte `json:"pad"`
}

func newPayload(id int64) payload {
	return payload{ID: id, EnqueuedAt: time.Now().UnixNano()}
}

func (p payload) marshal() []byte {
	b, err := json.Marshal(p)
	if err != nil {
		panic(err) // a fixed-shape struct can't fail to marshal
	}

	return b
}

func unmarshalPayload(b []byte) (payload, error) {
	var p payload
	err := json.Unmarshal(b, &p)

	return p, err
}

// task is the work a handler does for each job.
type task struct {
	name  string
	sleep time.Duration
}

func (t task) run() {
	if t.sleep > 0 {
		time.Sleep(t.sleep)
	}
}

type handler func(p payload)
type enqueueFunc func(p payload) error

type producerFactory func(url string) (enqueue enqueueFunc, closeFn func())

// consumerFactory starts workers consuming from Redis. enqueue is nil for every
// subject except VarMQ, whose producer must be the consumer's bound queue.
type consumerFactory func(url string, workers int, h handler) (enqueue enqueueFunc, stop func())

type durableSubject struct {
	name         string
	maxInstances int
	producer     producerFactory
	consumer     consumerFactory
}

type workload struct {
	name                string
	producers, jobsEach int
}

func (w workload) total() int { return w.producers * w.jobsEach }

// shape is one worker/instance combination of the matrix.
type shape struct{ workers, instances int }

// label names the shape in benchmark names, e.g. "W100C4".
func (s shape) label() string {
	return fmt.Sprintf("W%dC%d", s.workers, s.instances)
}

// collector counts unique job IDs and runs the task for each. A redelivered
// ID is counted as a duplicate and skipped.
type collector struct {
	t        task
	n        int64
	seen     []atomic.Bool
	unique   atomic.Int64
	dupCount atomic.Int64
	doneCh   chan struct{}
}

func newCollector(n int, t task) *collector {
	return &collector{
		t:      t,
		n:      int64(n),
		seen:   make([]atomic.Bool, n),
		doneCh: make(chan struct{}),
	}
}

func (c *collector) handle(p payload) {
	if p.ID < 0 || p.ID >= c.n || !c.seen[p.ID].CompareAndSwap(false, true) {
		c.dupCount.Add(1)
		return
	}

	c.t.run()

	if c.unique.Add(1) == c.n {
		close(c.doneCh)
	}
}

// done is closed once n unique IDs have been handled.
func (c *collector) done() <-chan struct{} { return c.doneCh }

func (c *collector) wait(timeout time.Duration) error {
	select {
	case <-c.doneCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("got %d/%d jobs (%d duplicates) after %s",
			c.unique.Load(), c.n, c.dupCount.Load(), timeout)
	}
}

func (c *collector) dups() int64 { return c.dupCount.Load() }

// percentile returns the nearest-rank value of an ascending slice; q=1 is the max.
func percentile(sorted []int64, q float64) int64 {
	i := int(math.Ceil(q*float64(len(sorted)))) - 1

	return sorted[min(max(i, 0), len(sorted)-1)]
}

// poolSize is the Redis pool size per instance: the worker count, but never
// below 10 connections per CPU.
func poolSize(workers int) int {
	return max(workers, 10*runtime.GOMAXPROCS(0))
}

// withPoolSize adds or replaces the pool_size query param of a Redis URL.
func withPoolSize(rawURL string, n int) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	q := u.Query()
	q.Set("pool_size", fmt.Sprint(n))
	u.RawQuery = q.Encode()

	return u.String()
}
