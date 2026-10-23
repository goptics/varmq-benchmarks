# Durable queue benchmark: design

Date: 2026-10-20. Status: draft, waiting for approval.

## Intent

varmq can store jobs in Redis through the `redisq` adapter. Its persistent queue gives one process durable jobs. Its distributed queue spreads jobs across several processes, which pub/sub wakes up. That puts varmq up against Go job queues such as asynq, not only against in-process worker pools.

The reader is a Go developer choosing a Redis-backed background job queue. The page should answer: **how much does varmq cost per job, moving jobs through Redis, compared with dedicated Go job queues and a raw Redis floor?**

This repo's vision still holds: one harness, realistic shapes, honest baselines, raw output committed, charts built in CI.

## Goals

- A separate benchmark for Redis-backed queues, kept apart from the pool benchmark in code, dependencies, raw output and chart page.
- Same subject-as-adapter idea as `subjects.go`: one small adapter per library, driven by one harness.
- Three benchmarks: end-to-end throughput, enqueue-only throughput, and pickup latency with no backlog (p50, p99, max).
- A raw go-redis baseline, so each library's overhead over plain Redis is visible.

## Non-goals (v1)

- Crash, recovery or exactly-once tests. They test correctness, not speed. If added later they go in a feature table, not a chart.
- Consumers in separate OS processes or on separate machines. In v1 a "consumer instance" means its own client and worker inside the benchmark process (see Dimensions).
- Running benchmarks in CI. Same reason as the pool benchmark: runner hardware is noisy.
- Priority queues, scheduled or delayed jobs, retries, result storage.
- Tuning any library past its defaults, other than the worker-concurrency axis.

## Anti-goals

- A chart that looks rigged. Every subject's per-job Redis work (acks, wake-up, serialization) is listed next to the chart, so a fast number can't hide skipped guarantees and a slow one can't hide extra work.
- Touching the pool benchmark's zero-setup run. `go test -bench=.` at the repo root must still need no Redis and pull no Redis deps.
- Wiping someone's real Redis. The harness runs `FLUSHDB`, so it must point at a dedicated instance by default.

## Layout (decided)

| Path | Purpose |
|------|---------|
| `durable/go.mod` | Separate module, `github.com/goptics/varmq-benchmarks/durable`. Holds the Redis, asynq, rmq, taskq and gocraft/work deps. |
| `durable/subjects.go` | Adapters, dimensions, payload |
| `durable/types.go` | Harness types |
| `durable/benchmark_test.go` | `BenchmarkDurable` (end-to-end), `BenchmarkEnqueue`, `BenchmarkLatency` |
| `durable/compose.yml` | Dedicated Redis for benchmarks |
| `bench/durable.txt` | Committed raw output, source of truth for the durable chart |
| `Taskfile.yml` | New `durable-*` tasks with `dir: durable` |
| `.github/workflows/bench.yml` | Also builds the durable page |
| `README.md` | Short section linking to the durable page and `durable/README.md` |
| `durable/README.md` | Subjects, per-subject work table, how to run |

## Subjects (decided)

| Name | What it is |
|------|------------|
| **VarMQ** | varmq worker + `WithPersistentQueue(redisq.NewQueue(...))`. Single consumer instance only (see below). |
| **VarMQDistributed** | Producer: `varmq.NewDistributedQueue` over `redisq.NewDistributedQueue`. Consumer: worker + `WithDistributedQueue`, woken by pub/sub. |
| **Asynq** | `hibiken/asynq` client + server, one queue, default options except concurrency. |
| **Rmq** | `adjust/rmq`, one queue, consumers added to match the concurrency axis. |
| **Taskq** | `vmihailenco/taskq` with its Redis backend. |
| **GocraftWork** | `gocraft/work` (uses redigo, a second Redis client, which is fine in this separate module). |
| **RawRedis** | Baseline. Producer `LPUSH`, consumers `BRPOP` in a loop on go-redis. **No ack, no notify.** This is the floor for one Redis list round trip per job. |

**VarMQ (persistent) runs at 1 consumer instance only.** `persistentQueue.Add` wakes the worker with an in-process call (`notifyToPullNextJobs`), and there is no polling. A second instance would never wake for jobs another process added. Only VarMQDistributed, which subscribes to an "enqueued" pub/sub message, runs at 4 instances. The page says this in so many words.

### Per-subject Redis work, published next to the chart

The builder fills this from source while writing each adapter. The VarMQ rows are already known from reading `varmq` and `redisq`:

| Subject | Ack on hot path | Wake mechanism | Round trips per job (consumer side) | Serialization |
|---------|-----------------|----------------|--------------------------------------|---------------|
| VarMQ | yes: `LPOP`, then `HSET`+`ZADD` pipeline, then `HDEL`+`ZREM` pipeline on ack | in-process signal | 3, plus an `LLEN` per dispatch-loop pass (`w.pending()` calls `Len()`) | job JSON |
| VarMQDistributed | same as VarMQ. The `PUBLISH` happens on enqueue only: `ackSource.Next` calls `DequeueWithAckId` on the embedded `*redisq.Queue`, which skips the dequeue notification | pub/sub | same as VarMQ (producer side: `RPUSH` + `PUBLISH`) | job JSON |
| Asynq | builder fills | builder fills | builder fills | builder fills |
| Rmq | builder fills | | | |
| Taskq | builder fills | | | |
| GocraftWork | builder fills | | | |
| RawRedis | no | blocking pop | 1 | none (raw bytes) |

Also known: every `redisq.Queue` operation holds one per-queue `sync.Mutex`, so within one process all of that queue's Redis calls run one at a time, whatever the worker concurrency. Expect VarMQ to flatten as concurrency rises. That is a real finding, and it is published as measured.

## Harness

### Adapter shape

A subject is a factory, in the same spirit as `subjects.go`:

```go
type handler func(p payload)

type durableSubject struct {
    name          string
    maxConsumers  int // 1 for VarMQ persistent; 4 for the rest
    // producer returns an enqueue func and its teardown.
    producer func(rdb redisCfg) (enqueue func(payload), closeProducer func())
    // consumer starts one consumer instance with `workers` concurrency that calls h per job.
    consumer func(rdb redisCfg, workers int, h handler) (stop func())
}
```

The exact signatures are the builder's call. What must hold: producer and consumer are separate, so `BenchmarkEnqueue` can run producers alone, and consumer instances are independent (own client, own worker).

**What a producer is (my call).** `producer(...)` is called **once per iteration**, which models one app process. The workload's N producers are goroutines sharing that one enqueue func and its client, just as the pool benchmark's users share one pool. This matches how a web service uses a job client. For VarMQ it means 100 producers contend on one `redisq.Queue` mutex. That is how varmq is used, so it is measured.

**VarMQ persistent breaks the split (my call).** Its `Add` is what wakes its worker, so in `BenchmarkDurable` the enqueue func is the consumer instance's own bound `PersistentQueue.Add`. In `BenchmarkEnqueue` it is the same bound queue, with the worker created but not started (`WithAutoRun(false)` or the equivalent), so `Add` pays its real cost and nothing is consumed. VarMQDistributed has no such problem: its producer is `varmq.NewDistributedQueue`, with no worker.

**Redis connection pools (my call).** Connection-pool size counts as part of the concurrency axis, not as tuning. Where the caller builds or configures the client (RawRedis, rmq, gocraft/work's redigo pool, asynq's `RedisClientOpt.PoolSize`, `redisq.New` through URL params such as `?pool_size=`), set the pool size to the instance's worker count, with the library default as the floor. Where a library gives no way to set it, keep the default. Either way, the work table gets a "pool size" column. Without this, go-redis's default (10 × GOMAXPROCS) would starve the RawRedis floor at W1000.

### Payload (decided)

One fixed struct for every subject, so the "same payload" claim is true and latency can be measured:

```go
type payload struct {
    ID         int64
    EnqueuedAt int64    // time.Now().UnixNano() at enqueue
    Pad        [48]byte // brings the struct to 64 B
}
```

Each adapter encodes it however its library expects (JSON for varmq, which encodes the job itself; JSON for the others unless a library forces otherwise). Encoding cost is part of each library's cost.

### Handler and completion

The shared handler:
1. runs the task (no-op, or 1 ms sleep);
2. calls `wg.Done()`.

`BenchmarkLatency` uses its own handler, which records `time.Now().UnixNano() - p.EnqueuedAt` and signals the producer (see Metrics).

The benchmark waits on the `WaitGroup` for all N jobs.

### Timed region (my call)

- The Redis client used for `FLUSHDB` is created once, outside the timer.
- `FLUSHDB` runs before each iteration, outside the timer (`b.StopTimer`/`b.StartTimer`).
- Inside the timer: create producers and consumers, enqueue N jobs, wait for all handlers, stop everything. This matches the pool benchmark, which also times construction and teardown.

### Metrics

`BenchmarkDurable` (throughput, end to end):
- `ns/op`: wall time per iteration (N jobs, end to end)
- `jobs/s`: custom metric, N / elapsed seconds, higher is better
- `B/op`, `allocs/op`: as usual. Client-side allocations only; Redis memory is not measured.
- No latency here. Under a flood of enqueues, latency measures how long the backlog takes to drain, not the library.

`BenchmarkEnqueue`: producers enqueue N jobs with no consumers running. Reports `ns/op` and `jobs/s`. `FLUSHDB` follows, outside the timer.

`BenchmarkLatency` (decided): **pickup latency**, from enqueue to handler start, with no backlog.
- One producer and one consumer instance, with 10 workers and the `Noop` task.
- The producer enqueues a job, waits until its handler signals (an unbuffered channel), then sends the next. 10,000 jobs per iteration.
- The handler records `time.Now().UnixNano() - p.EnqueuedAt` into a preallocated `[]int64`.
- Reports `p50-ms`, `p99-ms` and `max-ms` as custom metrics. Sort the slice and index it. No histogram library.
- This is where wake mechanisms show: pub/sub, blocking pop or polling an empty queue. The builder records each library's actual idle-wait behaviour (poll interval, blocking timeout) in the work table, taken from source, before trusting the numbers.
- VarMQ persistent runs here too, since one producer and one instance fits it.

## Dimensions (decided, with my sizing calls)

| Axis | Values |
|------|--------|
| Jobs per run | 100,000 total |
| Producers | `1u-100Kt` (1 producer x 100K jobs), `100u-1Kt` (100 producers x 1K jobs) |
| Workers per consumer instance | 10, 100, 1000 |
| Consumer instances | 1, 4 (VarMQ persistent: 1 only) |
| Task | `Noop`, `Sleep1ms` |

Workers are per instance, so `W100C4` means 4 instances with 100 workers each, 400 in total. The chart says this.

### Benchmark names

To stay compatible with vizb's `/n/y/x` grouping and with the pool benchmark's style:

```
BenchmarkDurable/<Task>W<workers>C<instances>/<workload>/<subject>
BenchmarkDurable/NoopW100C4/100u-1Kt/Asynq
BenchmarkEnqueue/<workload>/<subject>
BenchmarkEnqueue/1u-100Kt/VarMQ
BenchmarkLatency/NoopW10C1/1u-10Kt/<subject>
```

### Defaults

- `-benchtime=1x` is the default for durable tasks, because each iteration is 100K+ network round trips. `durable-txt` uses `-benchtime=2x` to match `bench-txt`.
- Only the machine's default CPU count. Skip the 8 and 4 CPU runs in v1, since Redis is likely the bottleneck. (My call.)

## Redis setup (decided)

- `durable/compose.yml`: `redis:7.2-alpine`, host port **6390** (my call, picked to avoid 6379 and varmq's 6375), no volume, so nothing persists between runs.
- The harness reads `REDIS_URL`, defaulting to `redis://localhost:6390/0`.
- If Redis can't be reached, the benchmark calls `b.Skip` with a message saying how to start it. A plain `go test ./...` in `durable/` without Redis skips cleanly.

## Taskfile (decided)

- `durable-up` / `durable-down`: `docker compose -f durable/compose.yml up -d --wait` / `down`
- `durable-bench-quick`: `go test -run xxx -bench . -benchtime=1x -benchmem` in `durable/`
- `durable-txt`: writes `bench/durable.txt`
- `durable-vizb`: writes `jsons/durable.json`
- `report` stays as it is. A new `durable-report` runs up, txt, vizb, then renders `dist/durable/index.html`.

## Publishing pipeline (decided)

`peaceiris/actions-gh-pages` uses `force_orphan: true`, so each deploy replaces the whole branch. Both pages must be published in one deploy:

1. Add `durable.txt` to the `convert` matrix, producing `durable.json`.
2. In `merge-deploy`, keep the existing merge into `public/index.html`, but give it an explicit file list that leaves out `durable.json`. Add a second vizb step that renders `durable.json` alone into `public/durable/index.html`.
3. One `actions-gh-pages` step publishes `public/`.

The main page links to `/durable/` through the README badge row. Vizb has no cross-page links, so the README carries them.

## Build order (my call; scope unchanged)

Ship in steps that each run end to end:

1. Module, compose, harness, payload, metrics, **RawRedis**. This proves the harness with the fewest moving parts.
2. **VarMQ** (persistent) and **Asynq**, plus `BenchmarkLatency`. This gives the headline comparison.
3. **VarMQDistributed**, plus the 4-instance axis.
4. **Rmq**, **Taskq**, **GocraftWork**.
5. Taskfile tasks, `bench/durable.txt`, CI changes, READMEs, and the per-subject work table.

## Left to the builder

- Exact adapter signatures and file split inside `durable/`.
- How each library's consumer concurrency maps to "workers". Use the library's own concurrency setting, and note the mapping in the work table.
- Silencing each library's logger during runs.
- Library versions: latest tagged release of each at build time, recorded in `durable/go.mod`.
- Whether a library that can't express one shape (for example, no multi-instance support) skips that row. Skip with `b.Skip` and a reason, and never fake it.
