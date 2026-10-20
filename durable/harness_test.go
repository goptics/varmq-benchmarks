package durable

import (
	"context"
	"io"
	"log"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// nopLogger silences go-redis v9's internal logger.
type nopLogger struct{}

func (nopLogger) Printf(context.Context, string, ...any) {}

func TestMain(m *testing.M) {
	// redisq and gocraft/work log through the std logger, and `go test` sends
	// stderr to the same file as stdout, so it must stay quiet.
	log.SetOutput(io.Discard)
	redis.SetLogger(nopLogger{})

	os.Exit(m.Run())
}

func redisURL() string {
	if u := os.Getenv("REDIS_URL"); u != "" {
		return u
	}

	return "redis://localhost:6390/0"
}

// redisOrSkip returns the Redis URL, or skips tb when Redis isn't reachable.
func redisOrSkip(tb testing.TB) string {
	tb.Helper()

	url := redisURL()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := rawClient(url, 1)
	defer c.Close()

	if err := c.Ping(ctx).Err(); err != nil {
		tb.Skipf("redis not reachable at %s: run `task durable-up` or set REDIS_URL", url)
	}

	return url
}

func flush(tb testing.TB, url string) {
	tb.Helper()

	c := rawClient(url, 1)
	defer c.Close()

	if err := c.FlushDB(context.Background()).Err(); err != nil {
		tb.Fatalf("flushdb: %v", err)
	}
}

// produce spawns one goroutine per producer. Each enqueues jobsEach payloads
// until it finishes, fails or stop is called, and reports at most one error on
// errs. done closes once every producer goroutine has returned.
func produce(enqueue enqueueFunc, w workload) (errs <-chan error, done <-chan struct{}, stop func()) {
	errCh := make(chan error, w.producers)
	doneCh := make(chan struct{})

	var (
		wg      sync.WaitGroup
		stopped atomic.Bool
	)

	for i := range w.producers {
		wg.Go(func() {
			for j := range w.jobsEach {
				if stopped.Load() {
					return
				}

				if err := enqueue(newPayload(int64(i*w.jobsEach + j))); err != nil {
					errCh <- err

					return
				}
			}
		})
	}

	go func() {
		wg.Wait()
		close(doneCh)
	}()

	return errCh, doneCh, func() { stopped.Store(true) }
}

// quiesce stops the producers and waits for any in-flight enqueue to return,
// so the caller's deferred closeFn/stop never races a running enqueue. The
// wait is bounded so a wedged library can't hang the run.
func quiesce(stop func(), done <-chan struct{}) {
	stop()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

// runEndToEnd starts sh.instances consumers, enqueues the workload and waits
// until every job has been handled. It returns the duplicate-delivery count.
// tb is only touched from the calling goroutine.
func runEndToEnd(tb testing.TB, s durableSubject, url string, w workload, sh shape, t task) (dups int64) {
	tb.Helper()

	c := newCollector(w.total(), t)

	var enqueue enqueueFunc

	for i := range sh.instances {
		enq, stop := s.consumer(url, sh.workers, c.handle)
		defer stop()

		if i == 0 {
			enqueue = enq
		}
	}

	if enqueue == nil {
		enq, closeFn := s.producer(url)
		defer closeFn()

		enqueue = enq
	}

	errs, done, stopProducers := produce(enqueue, w)
	defer quiesce(stopProducers, done) // runs before the closeFn/stop defers above

	select {
	case <-c.done():
	case err := <-errs:
		tb.Fatalf("%s: enqueue: %v", s.name, err)
	case <-time.After(5 * time.Minute):
		if err := c.wait(0); err != nil {
			tb.Fatalf("%s: %v", s.name, err)
		}
	}

	return c.dups()
}

// runEnqueue measures producing only: nothing consumes the jobs.
func runEnqueue(tb testing.TB, s durableSubject, url string, w workload) {
	tb.Helper()

	enqueue, closeFn := s.producer(url)
	defer closeFn()

	errs, done, stopProducers := produce(enqueue, w)
	defer quiesce(stopProducers, done) // runs before closeFn

	select {
	case <-done:
	case err := <-errs:
		tb.Fatalf("%s: enqueue: %v", s.name, err)
	}

	// done can win the select over a pending error; drain it.
	select {
	case err := <-errs:
		tb.Fatalf("%s: enqueue: %v", s.name, err)
	default:
	}
}

// runLatency enqueues n jobs one at a time, waiting for each to be handled,
// and returns the enqueue-to-handled latencies in ns, sorted.
func runLatency(tb testing.TB, s durableSubject, url string, n int) []int64 {
	tb.Helper()

	// Buffered so a late or duplicate delivery never blocks a worker (and
	// with it, stop).
	got := make(chan int64, 1)
	h := func(p payload) {
		select {
		case got <- time.Now().UnixNano() - p.EnqueuedAt:
		default:
		}
	}

	enqueue, stop := s.consumer(url, 10, h)
	defer stop()

	if enqueue == nil {
		enq, closeFn := s.producer(url)
		defer closeFn()

		enqueue = enq
	}

	out := make([]int64, 0, n)

	for i := range n {
		if err := enqueue(newPayload(int64(i))); err != nil {
			tb.Fatalf("%s: enqueue %d: %v", s.name, i, err)
		}

		select {
		case d := <-got:
			out = append(out, d)
		case <-time.After(10 * time.Second):
			tb.Fatalf("%s: job %d not handled within 10s", s.name, i)
		}
	}

	slices.Sort(out)

	return out
}
