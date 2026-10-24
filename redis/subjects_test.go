package durable

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var wantSubjects = []string{"VarMQ", "Asynq", "VarMQDistributed", "Rmq", "Taskq", "GocraftWork"} // each later task appends its name

func TestSubjectNames(t *testing.T) {
	var got []string
	for _, s := range subjects {
		got = append(got, s.name)
	}

	if !slices.Equal(got, wantSubjects) {
		t.Fatalf("subjects = %v, want %v", got, wantSubjects)
	}
}

func TestRedisOrSkipSkipsWhenDown(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:1/0")

	var inner *testing.T
	t.Run("inner", func(t *testing.T) {
		inner = t
		redisOrSkip(t)
	})

	if !inner.Skipped() {
		t.Fatal("redisOrSkip did not skip with Redis unreachable")
	}
}

// recordingTB records failures instead of failing the real test. Stopping
// calls end the goroutine, like the real testing.TB does.
type recordingTB struct {
	testing.TB
	mu   sync.Mutex
	msgs []string
}

func (r *recordingTB) record(msg string) {
	r.mu.Lock()
	r.msgs = append(r.msgs, msg)
	r.mu.Unlock()
}

func (r *recordingTB) Helper()                   {}
func (r *recordingTB) Logf(f string, a ...any)   {}
func (r *recordingTB) Log(a ...any)              {}
func (r *recordingTB) Errorf(f string, a ...any) { r.record(fmt.Sprintf(f, a...)) }
func (r *recordingTB) Error(a ...any)            { r.record(fmt.Sprint(a...)) }
func (r *recordingTB) Fatalf(f string, a ...any) { r.record(fmt.Sprintf(f, a...)); runtime.Goexit() }
func (r *recordingTB) Fatal(a ...any)            { r.record(fmt.Sprint(a...)); runtime.Goexit() }
func (r *recordingTB) FailNow()                  { runtime.Goexit() }
func (r *recordingTB) Skipf(f string, a ...any) {
	r.record("skip: " + fmt.Sprintf(f, a...))
	runtime.Goexit()
}
func (r *recordingTB) Skip(a ...any) { r.record("skip: " + fmt.Sprint(a...)); runtime.Goexit() }
func (r *recordingTB) SkipNow()      { runtime.Goexit() }

func TestRunEndToEndFailsOnEnqueueError(t *testing.T) {
	url := redisOrSkip(t)
	bad := durableSubject{
		name: "Bad", maxInstances: 1,
		producer: func(string) (enqueueFunc, func()) {
			return func(payload) error { return errors.New("boom") }, func() {}
		},
		consumer: func(string, int, handler) (enqueueFunc, func()) { return nil, func() {} },
	}

	fake := &recordingTB{TB: t}
	done := make(chan struct{})

	go func() {
		defer close(done)
		runEndToEnd(fake, bad, url, workload{"1u-10t", 1, 10}, shape{1, 1}, tasks[0])
	}()
	<-done

	fake.mu.Lock()
	defer fake.mu.Unlock()

	if len(fake.msgs) != 1 || !strings.Contains(fake.msgs[0], "boom") {
		t.Fatalf("want one recorded failure mentioning boom, got %q", fake.msgs)
	}
}

// slowFailProducer fails its first enqueue and makes every other one slow, so
// the remaining producers are mid-enqueue when the failure lands. closeFn
// records whether any enqueue was still running.
func slowFailProducer() (producerFactory, *atomic.Int64, *atomic.Int64) {
	var calls, inflight, inflightAtClose atomic.Int64

	return func(string) (enqueueFunc, func()) {
		return func(payload) error {
				inflight.Add(1)
				defer inflight.Add(-1)

				if calls.Add(1) == 1 {
					return errors.New("boom")
				}

				time.Sleep(20 * time.Millisecond)

				return nil
			}, func() {
				inflightAtClose.Store(inflight.Load())
			}
	}, &calls, &inflightAtClose
}

func TestRunQuiescesProducersBeforeClose(t *testing.T) {
	url := redisOrSkip(t)
	w := workload{"4u-100t", 4, 100}
	idle := func(string, int, handler) (enqueueFunc, func()) { return nil, func() {} }

	runs := map[string]func(tb testing.TB, s durableSubject){
		"EndToEnd": func(tb testing.TB, s durableSubject) { runEndToEnd(tb, s, url, w, shape{1, 1}, tasks[0]) },
		"Enqueue":  func(tb testing.TB, s durableSubject) { runEnqueue(tb, s, url, w) },
	}

	for name, run := range runs {
		t.Run(name, func(t *testing.T) {
			producer, calls, inflightAtClose := slowFailProducer()
			s := durableSubject{name: "Slow", maxInstances: 1, producer: producer, consumer: idle}

			fake := &recordingTB{TB: t}
			done := make(chan struct{})

			go func() {
				defer close(done)
				run(fake, s)
			}()
			<-done

			if len(fake.msgs) != 1 || !strings.Contains(fake.msgs[0], "boom") {
				t.Fatalf("want one failure mentioning boom, got %q", fake.msgs)
			}

			if n := inflightAtClose.Load(); n != 0 {
				t.Fatalf("%d enqueue calls still in flight when closeFn ran", n)
			}

			// stop took effect: far fewer calls than the 400 jobs.
			if n := calls.Load(); n >= int64(w.total()) {
				t.Fatalf("producers kept enqueuing after the failure: %d calls", n)
			}
		})
	}
}

func TestSubjects(t *testing.T) {
	url := redisOrSkip(t)
	w := workload{"4u-250t", 4, 250}

	for _, s := range subjects {
		t.Run(s.name, func(t *testing.T) {
			for _, instances := range slices.Compact([]int{1, s.maxInstances}) {
				t.Run(fmt.Sprintf("C%d", instances), func(t *testing.T) {
					// Two back-to-back rounds: leftover consumers from the first
					// would steal jobs from the second.
					for round := range 2 {
						flush(t, url)

						if dups := runEndToEnd(t, s, url, w, shape{10, instances}, tasks[0]); dups != 0 {
							t.Fatalf("round %d: %d duplicates", round, dups)
						}
					}
				})
			}

			flush(t, url)
			runEnqueue(t, s, url, w)

			flush(t, url)

			lat := runLatency(t, s, url, 50)
			if len(lat) != 50 {
				t.Fatalf("len(latencies) = %d, want 50", len(lat))
			}

			for _, v := range lat {
				if v <= 0 {
					t.Fatalf("non-positive latency %d", v)
				}
			}
		})
	}
}
