# Durable Queue Benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A separate Go module, `durable/`, that benchmarks Redis-backed job queues (VarMQ, VarMQDistributed, Asynq, Rmq, Taskq, GocraftWork) against a raw go-redis floor. It covers throughput, enqueue-only throughput and pickup latency, and publishes the results as their own chart page.

**Architecture:** A subject is a producer factory plus a consumer factory, driven by one harness. That harness serves `BenchmarkDurable`, `BenchmarkEnqueue`, `BenchmarkLatency` and a Redis-backed smoke test, `TestSubjects`. A `collector` dedupes deliveries by job ID and times out instead of hanging. The module has its own `go.mod`, so the root pool benchmark stays dependency-free.

**Tech Stack:** Go 1.26, Redis 7.2 (docker compose), go-redis v9, varmq + redisq, asynq, rmq v5, taskq v3, gocraft/work (redigo), Task, vizb, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-20-durable-queue-bench-design.md`

## Global Constraints

- Module path `github.com/goptics/varmq-benchmarks/durable`, `go 1.26.0`, in `durable/go.mod`. The root `go.mod` gains no dependencies.
- Pinned versions: `github.com/goptics/varmq v1.9.0`, `github.com/goptics/redisq v1.4.0`, `github.com/hibiken/asynq v0.26.0`, `github.com/adjust/rmq/v5 v5.2.0`, `github.com/vmihailenco/taskq/v3 v3.2.9` (v4 is still beta; v3 brings go-redis v8, which is fine here), `github.com/gocraft/work v0.5.1`, `github.com/gomodule/redigo v1.9.3`, `github.com/redis/go-redis/v9 v9.23.0`.
- Redis: `redis:7.2-alpine`, host port `6390`, no volume. The harness reads `REDIS_URL`, default `redis://localhost:6390/0`.
- Queue/key name for every subject: `bench`.
- Jobs per run: `100_000`. Workloads: `1u-100Kt` (1 × 100,000) and `100u-1Kt` (100 × 1,000). Workers per instance: `10, 100, 1000`. Instances: `1, 4`. Tasks: `Noop` and `Sleep1ms`.
- VarMQ (persistent) has `maxInstances: 1`. Every other subject has `4`. Never create a sub-benchmark for a shape a subject can't run.
- Names: `BenchmarkDurable/<Task>W<workers>C<instances>/<workload>/<subject>`, `BenchmarkEnqueue/<workload>/<subject>`, `BenchmarkLatency/NoopW10C1/1u-10Kt/<subject>`.
- Latency run: 1 producer, 1 instance, 10 workers, `Noop`, `10_000` jobs, enqueue-then-wait for each job.
- Metrics: custom `jobs/s` on Durable and Enqueue. Custom `p50-ms`, `p99-ms`, `max-ms` on Latency. Durable rows carry no latency metrics.
- `FLUSHDB` runs before every iteration, outside the timer. Client setup, enqueue, drain and stop are inside the timer.
- Redis pool size: `max(workers, 10*runtime.GOMAXPROCS(0))` per instance, wherever the library lets the caller set it. Where it can't be set, keep the default.
- Default `-benchtime=1x`. `durable-txt` uses `-benchtime=2x`. Default CPU count only.
- Nothing but `go test` output may reach stdout during benchmarks. Silence or redirect every library logger.
- Each library's settings stay at their defaults, except worker concurrency and pool size. Any value a library forces us to choose (for example, rmq's poll duration) goes in the README work table with its source.

## Review Focus

- **Duplicate delivery.** At-least-once libraries can redeliver. The collector must count a duplicate instead of panicking or double-counting. Pinned by `TestCollectorDuplicates` (Task 1), and by `TestSubjects`, which asserts `dups == 0` (Task 2).
- **Leftover consumers from the previous iteration** can steal the next iteration's jobs, and the run then hangs. Pinned by `TestSubjects` running two rounds back-to-back, each needing exactly N (Task 2).
- **A lost job or a stalled library** must fail with counts, not hang forever. Pinned by `TestCollectorTimeout` (Task 1).
- **Redis down** must skip cleanly, with a hint, rather than panic or hang. Pinned by `TestRedisOrSkipSkipsWhenDown` (Task 2).
- **Enqueue failures**, such as pool timeouts at W1000C4, must fail the run, not vanish. `enqueueFunc` returns `error`, and the harness calls `tb.Fatal` on the first one. Pinned by `TestRunEndToEndFailsOnEnqueueError` (Task 2).
- **Library log lines in `bench/durable.txt`** would break vizb parsing. Pinned by a grep check in Task 9.

---

### Task 1: Module skeleton and core types

**Files:**
- Create: `durable/go.mod`, `durable/compose.yml`, `durable/types.go`, `durable/types_test.go`

**Interfaces:**
- Produces (all in package `durable`, file `types.go`):
  - `type payload struct { ID int64 \`json:"id"\`; EnqueuedAt int64 \`json:"t"\`; Pad [48]byte \`json:"pad"\` }`
  - `func newPayload(id int64) payload` sets `EnqueuedAt = time.Now().UnixNano()`
  - `func (p payload) marshal() []byte` (JSON) and `func unmarshalPayload(b []byte) (payload, error)`
  - `type task struct { name string; sleep time.Duration }` and `func (t task) run()`
  - `type handler func(p payload)` and `type enqueueFunc func(p payload) error`
  - `type producerFactory func(url string) (enqueue enqueueFunc, closeFn func())`
  - `type consumerFactory func(url string, workers int, h handler) (enqueue enqueueFunc, stop func())`. `enqueue` is nil for every subject except VarMQ, whose producer must be the consumer's bound queue.
  - `type durableSubject struct { name string; maxInstances int; producer producerFactory; consumer consumerFactory }`
  - `type workload struct { name string; producers, jobsEach int }` and `func (w workload) total() int`
  - `type shape struct { workers, instances int }` and `func (s shape) label() string`, which returns `"W100C4"`
  - `type collector`, `func newCollector(n int, t task) *collector`, `func (c *collector) handle(p payload)`, `func (c *collector) done() <-chan struct{}` (closed when n unique IDs are seen), `func (c *collector) wait(timeout time.Duration) error`, `func (c *collector) dups() int64`
  - `func percentile(sorted []int64, q float64) int64` (nearest rank: `sorted[ceil(q*n)-1]`, with q=1 giving the max)
  - `func poolSize(workers int) int`, which returns `max(workers, 10*runtime.GOMAXPROCS(0))`
  - `func withPoolSize(url string, n int) string`, which adds or replaces the `pool_size` query param

- [ ] **Step 1: Create `durable/go.mod`** (`module github.com/goptics/varmq-benchmarks/durable`, `go 1.26.0`) and `durable/compose.yml` (one `redis` service, `redis:7.2-alpine`, ports `"6390:6379"`, a `redis-cli ping` healthcheck, no volumes).

- [ ] **Step 2: Write the failing tests in `durable/types_test.go`**

```go
func TestShapeLabel(t *testing.T) {
    if got := (shape{workers: 100, instances: 4}).label(); got != "W100C4" { t.Fatal(got) }
}
func TestPercentile(t *testing.T) {
    s := make([]int64, 100); for i := range s { s[i] = int64(i + 1) }
    // want p50=50, p99=99, max(q=1)=100; single-element slice []int64{7} → 7 for any q
}
func TestPayloadRoundTrip(t *testing.T) // marshal → unmarshalPayload equals original
func TestWithPoolSize(t *testing.T) {
    // "redis://localhost:6390/0", 100 → "redis://localhost:6390/0?pool_size=100"
    // "redis://localhost:6390/0?protocol=3", 5 → query has protocol=3 AND pool_size=5
    // "redis://localhost:6390/0?pool_size=1", 5 → pool_size=5 (replaced, not duplicated)
}
func TestCollectorDuplicates(t *testing.T) {
    c := newCollector(3, task{name: "Noop"})
    for _, id := range []int64{0, 1, 1, 2} { c.handle(payload{ID: id}) }
    // wait(time.Second) == nil; dups() == 1
}
func TestCollectorTimeout(t *testing.T) {
    c := newCollector(3, task{name: "Noop"}); c.handle(payload{ID: 0})
    // err := c.wait(50*time.Millisecond); err != nil and err.Error() contains "1/3"
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd durable && go test -run 'TestShapeLabel|TestPercentile|TestPayloadRoundTrip|TestWithPoolSize|TestCollector' .`
Expected: FAIL (undefined identifiers).

- [ ] **Step 4: Implement `types.go`.** Collector: a preallocated `[]atomic.Bool` indexed by ID. An ID that is out of range or already seen counts as a duplicate and returns without running the task. Otherwise run the task, then `close(done)` when the unique count reaches n. `wait` selects on `done` or `time.After`, and on timeout returns `fmt.Errorf("got %d/%d jobs (%d duplicates) after %s", ...)`. `withPoolSize` uses `net/url`.

- [ ] **Step 5: Run the tests to verify they pass.** Same command. Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add durable/go.mod durable/compose.yml durable/types.go durable/types_test.go
git commit -m "feat(durable): add module, compose, payload and collector"
```

### Task 2: Harness, RawRedis baseline, smoke test, run tasks

**Files:**
- Create: `durable/harness_test.go`, `durable/benchmark_test.go`, `durable/subjects.go`, `durable/raw.go`, `durable/subjects_test.go`, `durable/README.md`
- Modify: `Taskfile.yml` (add `durable-up`, `durable-down`, `durable-bench-quick`, `durable-test`)

**Interfaces:**
- Consumes: everything from Task 1.
- Produces:
  - `subjects.go`: `var workloads []workload`, `var shapes []shape` (the 6 products of workers × instances), `var tasks []task` (`Noop`, `Sleep1ms`), `var latencyWorkload = workload{"1u-10Kt", 1, 10_000}`, `var latencyShape = shape{10, 1}`, `var subjects []durableSubject`
  - `raw.go`: `func rawProducer(url string) (enqueueFunc, func())`, `func rawConsumer(url string, workers int, h handler) (enqueueFunc, func())`
  - `harness_test.go`: `func TestMain(m *testing.M)`, `func redisOrSkip(tb testing.TB) string`, `func flush(tb testing.TB, url string)`, `func runEndToEnd(tb testing.TB, s durableSubject, url string, w workload, sh shape, t task) (dups int64)`, `func runEnqueue(tb testing.TB, s durableSubject, url string, w workload)`, `func runLatency(tb testing.TB, s durableSubject, url string, n int) []int64` (returned sorted)
  - Later tasks append to `subjects`, add one adapter file each, and add one row to the README work table.

- [ ] **Step 1: Write the failing tests in `durable/subjects_test.go`**

```go
var wantSubjects = []string{"RawRedis"} // each later task appends its name

func TestSubjectNames(t *testing.T) // names of `subjects`, in order, equal wantSubjects

func TestRedisOrSkipSkipsWhenDown(t *testing.T) {
    t.Setenv("REDIS_URL", "redis://localhost:1/0")
    var inner *testing.T
    t.Run("inner", func(t *testing.T) { inner = t; redisOrSkip(t) })
    // inner.Skipped() == true
}

func TestRunEndToEndFailsOnEnqueueError(t *testing.T) {
    url := redisOrSkip(t)
    bad := durableSubject{name: "Bad", maxInstances: 1,
        producer: func(string) (enqueueFunc, func()) { return func(payload) error { return errors.New("boom") }, func() {} },
        consumer: func(string, int, handler) (enqueueFunc, func()) { return nil, func() {} }}
    // run runEndToEnd in a subtest with workload{"1u-10t",1,10}, shape{1,1}; the subtest must be Failed()
}

// TestSubjects: for each subject, for each instances in {1, s.maxInstances} (deduped),
// run TWO rounds back-to-back (flush before each) of workload{"4u-250t", 4, 250} with
// shape{workers: 10, instances}, task Noop; each round must return dups == 0 and
// complete (runEndToEnd fails the test otherwise).
// Also runEnqueue once and runLatency(..., 50), asserting len == 50 and every value > 0.
func TestSubjects(t *testing.T)
```

- [ ] **Step 2: Run the tests to verify they fail.** Run (with Redis up through `docker compose -f durable/compose.yml up -d --wait`): `cd durable && go test -run 'TestSubjectNames|TestRedisOrSkip|TestRunEndToEnd|TestSubjects' .`. Expected: FAIL (undefined harness and subjects).

- [ ] **Step 3: Implement `harness_test.go`.**
  - `TestMain`: `log.SetOutput(io.Discard)` (redisq and gocraft/work log through the std logger, and `go test` sends stderr to the same file as stdout), plus `redis.SetLogger` with a no-op logger for go-redis v9, then `os.Exit(m.Run())`. Per-library loggers (asynq, taskq/go-redis v8) are silenced in their adapter tasks.
  - `redisOrSkip`: reads `REDIS_URL` (default `redis://localhost:6390/0`), then sends `PING` with a 2 s timeout. If that fails, it calls `tb.Skipf("redis not reachable at %s: run `task durable-up` or set REDIS_URL", url)`.
  - `runEndToEnd`: start `sh.instances` consumers sharing one collector. Use instance 0's `enqueue` when it is non-nil, otherwise call `s.producer`. Producer goroutine `i` sends IDs `i*jobsEach … i*jobsEach+jobsEach-1`. On the first enqueue error, `tb.Fatalf` from the test goroutine (collect errors through a channel, not `Fatal` inside a goroutine). Register every consumer `stop` and the producer `closeFn` with a deferred call as soon as each is created, so a `Fatalf` path can't leak consumers into the next round. Then `select` on `collector.done()`, the enqueue error channel (`tb.Fatalf` with the error), and `time.After(5*time.Minute)` (`tb.Fatalf` with `wait(0)`'s message). Return `dups()`.
  - `runLatency`: one consumer with 10 workers. Its handler sends `time.Now().UnixNano()-p.EnqueuedAt` on an unbuffered channel. The producer loop enqueues job `i`, then waits on that channel with a 10 s timeout, calling `tb.Fatalf` with `i` on timeout.

- [ ] **Step 4: Implement `raw.go`.** go-redis v9 `LPUSH bench` of `p.marshal()`. Each consumer instance has its own client (`PoolSize: poolSize(workers)`) and `workers` goroutines looping `BRPOP bench 1s`. Stop cancels a context, then waits for the goroutines. A `redis.Nil` timeout loops, and any other error after cancel exits. Register `{name: "RawRedis", maxInstances: 4, producer: rawProducer, consumer: rawConsumer}` in `subjects.go`, along with the dimension vars.

- [ ] **Step 5: Implement `benchmark_test.go`.** Three benchmarks with the names from Global Constraints. Use the explicit `for range b.N` form, not `b.Loop()`, because each iteration stops the timer. Each iteration does `b.StopTimer(); flush(b, url); b.StartTimer()`, then the run. Report `jobs/s = total*b.N / b.Elapsed().Seconds()`, following the root `benchmark_test.go`. For `BenchmarkLatency`, gather the samples from all iterations, sort them, and report `p50-ms`, `p99-ms` and `max-ms` (ns / 1e6). Skip shapes where `sh.instances > s.maxInstances` by not calling `b.Run`.

- [ ] **Step 6: Run the tests to verify they pass.** Same command as Step 2. Expected: PASS. Then run a quick bench: `cd durable && go test -run xxx -bench 'Durable/NoopW10C1/100u-1Kt/RawRedis$|Enqueue/1u-100Kt/RawRedis$|Latency/.*/RawRedis$' -benchtime=1x -benchmem`. Expected: three result lines with `jobs/s` or `p50-ms`.

- [ ] **Step 7: Add Taskfile tasks** with `dir: durable`. `durable-up` runs `docker compose -f compose.yml up -d --wait`. `durable-down` runs `docker compose -f compose.yml down`. `durable-test` runs `go test ./...`. `durable-bench-quick` runs `go test -run xxx -bench . -benchtime=1x -benchmem`.

- [ ] **Step 8: Write `durable/README.md`.** It covers what this benchmarks and why it's separate, how to run (`task durable-up`, `task durable-bench-quick`), the dimensions and names, and the work table. Columns: Subject | Ack on hot path | Wake mechanism | Idle wait (poll interval / block timeout) | Round trips per job | Serialization | Pool size | Forced settings. Add the RawRedis row: no ack, blocking pop, `BRPOP` 1 s timeout, 1 round trip, raw JSON bytes, `poolSize(workers)`, none.

- [ ] **Step 9: Commit**

```bash
git add durable Taskfile.yml
git commit -m "feat(durable): add harness, RawRedis baseline and smoke test"
```

### Tasks 3–8: one adapter each

Every adapter task follows the same steps:

1. **Write the failing test.** Append the subject name to `wantSubjects` in `subjects_test.go`.
2. **Run** `cd durable && go test -run 'TestSubjectNames' .`. Expected: FAIL (name missing).
3. **Implement** the adapter file listed below and append its entry to `subjects`. Silence its logger.
4. **Run** `cd durable && go test -run 'TestSubjectNames|TestSubjects/<Name>' -v .`. Expected: PASS. Then run one quick row: `go test -run xxx -bench 'Durable/NoopW100C1/100u-1Kt/<Name>$' -benchtime=1x`.
5. **Fill in the README work table row from the library's source.** Record the ack and dequeue commands, the wake mechanism, the exact idle poll interval or block timeout, the round trips, and the forced settings, each citing a file and function.
6. **Commit** with `feat(durable): add <Name> subject`.

| Task | Name | File | maxInstances | Producer / consumer notes |
|------|------|------|--------------|---------------------------|
| 3 | `VarMQ` | `durable/varmq.go` | 1 | Consumer: `redisq.New(withPoolSize(url, poolSize(workers)))`, `.NewQueue("bench")`, then `varmq.NewWorker(func(j varmq.Job[payload]){ h(j.Data()) }, workers).WithPersistentQueue(q)`. Return the bound queue's `Add` (where `false` becomes an error) as `enqueue`. Stop with `w.WaitAndStop()` (so the last acks finish), then close the queue and the client. Producer (used only by `BenchmarkEnqueue`): the same setup with `varmq.WithAutoRun(false)`, so nothing is consumed. Its `closeFn` closes only the queue and the client, with no `WaitAndStop` on a worker that never ran. Known row: ack is `LPOP`, then `HSET`(+`ZADD`), then `HDEL`+`ZREM`; in-process wake; an extra `LLEN` per dispatch pass; one per-queue mutex. |
| 4 | `Asynq` | `durable/asynq.go` | 4 | Producer: `asynq.NewClient(asynq.RedisClientOpt{Addr, DB, PoolSize})`, enqueue `asynq.NewTask("bench", p.marshal())` with `asynq.Queue("bench")`. Consumer: `asynq.NewServer` with `Config{Concurrency: workers, Queues: map[string]int{"bench": 1}, Logger: silent, LogLevel: asynq.FatalLevel}` and a `ServeMux` handler. Start it with `srv.Start(mux)` and stop it with `srv.Shutdown()`. Parse `Addr`/`DB` from the URL with `redis.ParseURL`. Record the server's empty-queue sleep and `TaskCheckInterval` default from source. |
| 5 | `VarMQDistributed` | `durable/varmq_distributed.go` | 4 | Producer: `varmq.NewDistributedQueue[payload](redisq.New(...).NewDistributedQueue("bench"))`, `.Add`. Consumer: worker `.WithDistributedQueue(dq)`, with its own `redisq.New` client per instance. Known row: same ack path as VarMQ, `PUBLISH` on enqueue only, pub/sub wake. |
| 6 | `Rmq` | `durable/rmq.go` | 4 | `rmq.OpenConnectionWithRedisClient("bench", goRedisV9Client, errChan)` (rmq v5.2.0 depends on go-redis v9, so it shares the module's client import), `OpenQueue("bench")`, `Publish(string(p.marshal()))`. Consumer: `StartConsuming(int64(workers), 10*time.Millisecond)`, then `workers` `AddConsumerFunc`s that `Ack()` after `h`. Stop with `<-conn.StopAllConsuming()`. The 10 ms poll is a forced setting (the value from rmq's README); record it. Drain `errChan` to nowhere. |
| 7 | `Taskq` | `durable/taskq.go` | 4 | Import `github.com/vmihailenco/taskq/v3/redisq` as `taskqredis` (a name clash with goptics redisq) and go-redis **v8**. Register the task once at package init with `taskq.RegisterTask` (the registry is global; a second register panics), and route it to the current collector through an `atomic.Pointer[handler]`. Queue: `QueueOptions{Name: "bench", Redis: v8Client, MinNumWorker: int32(workers), MaxNumWorker: int32(workers)}`. Consumer: `q.Consumer().Start(ctx)` / `Stop()`. Producer: `q.Add(task.WithArgs(ctx, p.marshal()))`. Factory queue names are also global: create the factory per instance and close it on stop. |
| 8 | `GocraftWork` | `durable/gocraft.go` | 4 | redigo `&redis.Pool{MaxActive: poolSize(workers), MaxIdle: poolSize(workers), Wait: true, Dial: redis.DialURL(url)}`. Producer: `work.NewEnqueuer("bench", pool).Enqueue("bench", work.Q{"p": string(p.marshal())})`. Consumer: `work.NewWorkerPool(ctx{}, uint(workers), "bench", pool)`, `.Job("bench", fn)`, `.Start()` / `.Stop()`. Record its fetch sleep backoff from source. |

### Task 9: Raw output, charts, CI and READMEs

**Files:**
- Modify: `Taskfile.yml`, `.github/workflows/bench.yml`, `README.md`, `durable/README.md`
- Create: `bench/durable.txt`

**Interfaces:**
- Consumes: the full `subjects` list from Tasks 2–8.

- [ ] **Step 1: Add Taskfile tasks.**
  - `durable-txt` (`dir: durable`): `go test -run xxx -bench . -benchtime=2x -benchmem > ../bench/durable.txt`
  - `durable-vizb`: `vizb bar bench/durable.txt -p /n/y/x -T s -M MB -o jsons/durable.json -l --round 2 --border-radius="8,8,0,0" -n "Durable queues (Redis)"`
  - `durable-report`: `durable-up`, `durable-txt`, `durable-vizb`, `vizb ui jsons/durable.json -o dist/durable/index.html`, then open it, with the same platform split as `report`

- [ ] **Step 2: Generate the results.** Run `task durable-up && task durable-txt` in the background. It is long: about 7 subjects × 24 rows × 2 iterations.

- [ ] **Step 3: Check the output is clean.** Run `grep -vE '^(Benchmark|goos:|goarch:|pkg:|cpu:|PASS|ok )' bench/durable.txt`. Expected: no output. If anything appears, silence that logger and rerun Step 2.

- [ ] **Step 4: Check the chart builds locally.** Run `task durable-vizb && vizb ui jsons/durable.json -o dist/durable/index.html`. Expected: the HTML is written, and its groups are named like `NoopW100C4`.

- [ ] **Step 5: Edit `bench.yml`.**
  - Add a matrix entry `{file: durable.txt, output: durable, name: "Durable queues (Redis)"}`.
  - In `merge-deploy`, keep the existing explicit `merge-files` list unchanged (it already leaves out `durable.json`).
  - Add a vizb step with `merge-files: /tmp/jsons/durable.json` and `output-html: public/durable/index.html` before the single `actions-gh-pages` step. If the action rejects a one-file merge, install vizb and run `vizb ui /tmp/jsons/durable.json -o public/durable/index.html` instead.

- [ ] **Step 6: Update the READMEs.**
  - Root `README.md`: a short "Durable queues" section and a badge/link to `https://goptics.github.io/varmq-benchmarks/durable/`. Add `durable/` and `bench/durable.txt` to the Layout table.
  - `durable/README.md`: confirm every work-table row is filled. State plainly that VarMQ (persistent) runs at C1 only, and why.

- [ ] **Step 7: Commit**

```bash
git add Taskfile.yml .github/workflows/bench.yml README.md durable/README.md bench/durable.txt
git commit -m "feat(durable): publish durable benchmark results and chart page"
```
