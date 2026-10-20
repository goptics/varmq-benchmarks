# Durable queue benchmarks

Benchmarks for Redis-backed job queues: VarMQ's persistent and distributed
queues against Asynq, rmq, taskq and gocraft/work, with a raw Redis list as the
floor. It lives in its own Go module so the other libraries' dependencies (some
pull in go-redis v8) stay out of the root `go.mod`.

## Run

```sh
task durable-up           # Redis 7.2 on localhost:6390, no volume
task durable-test         # smoke tests
task durable-bench-quick  # every benchmark once (-benchtime=1x)
task durable-down
```

Set `REDIS_URL` to use another Redis (default `redis://localhost:6390/0`). The
tests and benchmarks skip when it isn't reachable. `FLUSHDB` runs before every
iteration, so don't point it at a Redis you care about.

## Dimensions

- Workloads: `1u-100Kt` (1 producer x 100,000 jobs), `100u-1Kt` (100 x 1,000)
- Workers per instance: 10, 100, 1000. Instances: 1, 4 (VarMQ's persistent
  queue supports 1 only, so it has no `C4` rows)
- Tasks: `Noop`, `Sleep1ms`
- Names: `BenchmarkDurable/<Task>W<workers>C<instances>/<workload>/<subject>`,
  `BenchmarkEnqueue/<workload>/<subject>`,
  `BenchmarkLatency/NoopW10C1/1u-10Kt/<subject>`
- Metrics: `jobs/s` (Durable, Enqueue); `p50-ms`, `p99-ms`, `max-ms` (Latency)

Each subject runs at its library defaults, apart from worker concurrency and
the Redis pool size (`max(workers, 10*GOMAXPROCS)`, where the library lets us
set it). Anything else a library forces us to choose is listed under
"Forced settings".

## Work per job

Each row of the work table is read from the library source. The notes per subject
come first and cite the file and function for each claim; the table is last, so
a new row is added at the end of the file.

### VarMQ (persistent)

Library versions `varmq v1.9.0` and `redisq v1.4.0`.

- Dequeue: `ackSource.Next` (`varmq/source.go`) calls `DequeueWithAckId`;
  `Queue.DequeueWithAckId` (`redisq/queue.go`) runs `Dequeue` (`LPOP`), then
  `PrepareForFutureAck` (one pipeline of `HSET` on the pending hash and `ZADD`
  on the timeout set, since the visibility timeout is above 0). `Enqueue`
  (`redisq/queue.go`) is a one-command `RPUSH` pipeline.
- Ack: `job.closeOnce` calls `job.acknowledge` (`varmq/job.go`), which calls
  `Queue.Acknowledge` (`redisq/queue.go`): one `HDEL`+`ZREM` pipeline.
- Wake: no Redis polling or blocking wait. Wakes are non-blocking sends on a
  channel read by `goEventLoop` (`varmq/worker_eventloop.go`,
  `notifyToPullNextJobs`), sent by in-process `persistentQueue.Add`
  (`varmq/persistent.go`), by `Start` (`varmq/worker_lifecycle.go`) and when a
  batch of jobs finishes (`varmq/worker_pool.go`). An idle instance has no
  timer or blocking read on Redis, so another process's adds do not wake it.
  This subject therefore runs 1 instance.
- Extra `LLEN`: the `goEventLoop` loop condition and `planBatch` both call
  `worker.pending` (`varmq/worker_park.go`), which calls `ackSource.Len`, and
  `directManager.Next` (`varmq/source_manager.go`) checks `Len` again before
  `Next`. Each is `Queue.Len`, one `LLEN` (`redisq/queue.go`).
- One mutex: `Enqueue`, `Len`, `PrepareForFutureAck`, `Acknowledge` and
  `Dequeue` each hold the queue's `sync.Mutex` for their whole round trip
  (`redisq/queue.go`), so one `redisq.Queue` runs one Redis round trip at a time
  whatever the pool size. Adds and dispatch share that lock.
- Visibility timeout: `newQueueBase` sets 5 minutes (`redisq/queue.go`). The
  adapter does not change it. `ackTimeout` stays 0, so no `EXPIRE` is sent.
- Pool size: `redisq.New` parses the URL with `redis.ParseURL`, which reads
  `pool_size` (`go-redis/v9/options.go`), so the adapter appends it with
  `withPoolSize`.
- Batch size: `ackSource.maxBatch` is 1 (`varmq/source.go`), so jobs are
  dispatched one at a time.
- Logging: varmq prints nothing (its default error handler is a no-op).
  `redisq` logs through the std `log` package; `TestMain` discards it.

### Work table

| Subject | Ack on hot path | Wake mechanism | Idle wait | Round trips per job | Serialization | Pool size | Forced settings |
|---------|-----------------|----------------|-----------|---------------------|---------------|-----------|-----------------|
| RawRedis | none | blocking pop | `BRPOP` 1 s block timeout | 1 | raw JSON bytes | `poolSize(workers)` | none |
| VarMQ (persistent) | `LPOP`, then `HSET`+`ZADD` pipeline, then `HDEL`+`ZREM` pipeline (5 commands, 3 round trips) | in-process: `Add` signals the worker's event loop, no polling | none: no Redis polling or blocking wait; wakes come from in-process `Add`, `Start` and job completion, so an idle second instance is not woken by another process's adds | at least 6: `LLEN` x3, `LPOP`, `HSET`+`ZADD`, `HDEL`+`ZREM` | `varmq.Job` JSON wrapping the payload | `poolSize(workers)` via `pool_size` in the URL | none; visibility timeout stays at its 5 min default |
