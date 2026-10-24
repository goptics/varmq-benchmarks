# Redis queue benchmarks

Benchmarks for Redis-backed job queues: VarMQ's persistent and distributed
queues against Asynq, rmq, taskq and gocraft/work. It lives in its own Go
module so the other libraries' dependencies (some pull in go-redis v8) stay out
of the other modules.

## Run

```sh
task redis:up      # Redis 7.2 (Debian image) on localhost:6390, pinned to CPU 14, no volume
task redis:test    # smoke tests
task redis:bench   # every benchmark once (-benchtime=1x)
task redis:txt     # full run into bench/redis.txt (hours)
task redis:down
```

`task redis:txt` runs the client with `taskset -c 0-13,16-23`, so Redis keeps
core 14 and its sibling thread to itself. Compare runs on this setup only: the
alpine image, unpinned, measured about 2.5x lower on the same code.

Set `REDIS_URL` to use another Redis (default `redis://localhost:6390/0`). The
tests and benchmarks skip when it isn't reachable. `FLUSHDB` runs before every
iteration, so don't point it at a Redis you care about.

## Dimensions

- Workloads: `1u-100Kt` (1 producer x 100,000 jobs), `100u-1Kt` (100 x 1,000)
- Workers per instance: 10, 100, 1000. Instances: 1, 4 (VarMQ's persistent
  queue supports 1 only, so it has no `C4` rows)
- Tasks: `Noop`, `Sleep1ms`
- Names: `BenchmarkDurable/<Task>W<workers>C<instances>/<workload>/<subject>`,
  `BenchmarkEnqueue/Enqueue/<workload>/<subject>`,
  `BenchmarkLatency/LatencyW10C1/1u-1Kt/<subject>`
- Metrics: `jobs/s` (Durable, Enqueue); `p50-ms`, `p99-ms`, `max-ms` (Latency)

Each subject runs at its library defaults, apart from worker concurrency and
the Redis pool size (`max(workers, 10*GOMAXPROCS)`, where the library lets us
set it). Anything else a library forces us to choose is listed under
"Forced settings".

The Enqueue and Latency names carry an extra segment: they were
`BenchmarkEnqueue/<workload>/<subject>` and `BenchmarkLatency/NoopW10C1/...`.
The dashboard's group pattern needs three segments after the function name, and
the old Latency name collided with the `NoopW10C1` end-to-end group.

## Datasets on the site

- Redis (Persistent): the `C1` rows of `BenchmarkDurable`, plus Enqueue and
  Latency, for VarMQ, Asynq, Rmq, Taskq and GocraftWork. VarMQ's persistent
  queue is single-instance, so it only races at one consumer process.
- Redis (Distributed): the `C4` rows, four consumer processes, VarMQDistributed
  included.

Latency is folded into the persistent dataset, so its group shows an empty
`jobs/s` card, and the end-to-end groups show empty `p50-ms`, `p99-ms` and
`max-ms` cards. Those are expected.

Adding a subject: also add it to `SUBJECTS` in the root Taskfile, or the
persistent dataset drops it.

## Results

`bench/redis.txt` is one full run from 2026-10-24: varmq 415ad73 (unreleased,
through the `replace` directive in `go.mod`), redisq v1.5.0, `-benchtime=2x`,
Redis 7.2 (Debian image) pinned to CPU 14, the client on the other cores as
`task redis:txt` sets up. 150 rows: 132 `BenchmarkDurable` (2 tasks x 3 worker
counts x 2 workloads x 11 subject and instance shapes), 12 `BenchmarkEnqueue`,
6 `BenchmarkLatency`. No row failed and no run stalled. jobs/s, higher is
better.

End to end, one instance (`C1`, VarMQ persistent against the four
libraries): VarMQ is first in 4 of 12 rows, all with 100 producers
(`100u-1Kt` at W100 and W1000, 60.6k and 59.2k against GocraftWork's 45.1k and
40.7k). With a single producer (`1u-100Kt`) GocraftWork leads every row at
about 25k and VarMQ is second at about 21k; those rows are bound by the one
producer's enqueue round trip. Against Asynq and Taskq, VarMQ is ahead in every
row: 1.1 to 1.4x with one producer, 2 to 3x with 100.

End to end, four instances (`C4`, VarMQDistributed): first in 6 of 12 rows,
every `100u-1Kt` row, by 1.3 to 2.1x over the best rival (55k to 66k against
GocraftWork's 33k to 49k). With a single producer GocraftWork or Rmq lead and
VarMQDistributed sits at 0.80 to 0.90x of the best.

`Sleep1ms` at W10 is bound by 10 workers x 1 ms, so every subject lands near
5k to 6k there.

Enqueue only (`BenchmarkEnqueue`): VarMQ 42.9k at one producer, the best of
the six; at 100 producers Rmq, GocraftWork and VarMQ tie near 201k to 202k and
VarMQDistributed trails at 143k, the cost of its wake script per enqueue.

Pickup latency (`BenchmarkLatency`, 1,000 jobs one at a time, ms):

| Subject | p50 | p99 | max |
|---|---|---|---|
| Taskq | 0.063 | 0.190 | 0.61 |
| GocraftWork | 0.092 | 0.474 | 10.80 |
| VarMQ | 0.142 | 0.384 | 3.58 |
| VarMQDistributed | 0.146 | 0.541 | 2.18 |
| Rmq | 102.2 | 112.3 | 113.3 |
| Asynq | 995.3 | 1492.0 | 1507.0 |

Rmq's 100 ms poll and Asynq's 0.5 to 1.5 s idle sleep show directly. VarMQ's
batch pop costs about 0.05 ms over Taskq's blocking `XREADGROUP` at p50.

In `C4` rows the 4 consumer instances are stopped one after another inside the
timed region, so subjects whose stop waits out an idle poll (Asynq about
0.5 to 1.5 s, Taskq up to 1 s per instance) pay that up to 4 times; a later run
will stop them concurrently.

A `Sleep1msW1000C4/1u-100Kt/VarMQDistributed` row stalled in an earlier full
run on redisq v1.4.0, described under "Known issue" in the VarMQDistributed
section. This run completed that row (and every other) on v1.5.0.

## Work per job

Each row of the work table is read from the library source. The notes per subject
come first and cite the file and function for each claim; the table is last, so
a new row is added at the end of the file.

### VarMQ (persistent)

Library versions `varmq 415ad73` (unreleased, `replace` directive) and
`redisq v1.5.0`.

- Dequeue: `bind` (`varmq/source.go`) sees that `*redisq.Queue` implements
  `IBatchAcknowledgeable` (`varmq/interface.go`; `DequeueBatchWithAckIds` in
  `redisq/queue.go`), so the worker's source is a `batchAckSource` and its
  manager a `creditManager` (`varmq/source_manager.go`). On every wake the
  event loop (`goEventLoop`, `varmq/worker_eventloop.go`) runs
  `dispatchCredit` (`varmq/worker_credit.go`), which asks the source for
  `min(free workers, maxCredit)` jobs, with `maxCredit` 64 and free workers
  `concurrency - busyWorkers`, in one `batchAckSource.fetch`, which is one
  `DequeueBatchWithAckIds` call. It pops nothing when the hint is dry or no
  worker is free. `dequeueBatch` (`redisq/queue.go`) runs one Lua script
  with `EVALSHA` (`Script.Run` in `go-redis/v9/script.go`, `EVAL` once on
  `NOSCRIPT`): `listAckPop` when it wants 2 or more items (`HMGET` of the
  reservation's ack ids as a re-send guard, `LRANGE`+`LTRIM`, `HSET` on
  `bench:nacked`, `ZADD` on `bench:timeouts` with a deadline from Redis
  `TIME`, `ZADD` on `bench:heartbeats`, `LLEN` for the reply), or the leaner
  `listAckPopOneLen` (`HGET` guard, `LPOP`, `HSET`, `ZADD`, `ZADD`, `LLEN`)
  when it wants one. How many it wants is `min(free workers, 64, popLen)`
  capped at `maxPop` 1000: redisq sizes each call by what the last one left
  in the queue or, once the queue emptied, by the run taken since it was
  last seen empty (`popLen` and `popRun` in `dequeueBatch`), so 64 is the
  cap that binds here. Each popped job goes straight to its own pool goroutine
  (`node.Value.Send` in `dispatchCredit`); nothing is buffered. One pop is
  in flight at a time per worker, since the event loop runs `dispatchCredit`
  serially; acks run from the pool goroutines concurrently. A pop that
  comes up short, or a full one that empties the queue, marks the source dry
  (`fetch`, `varmq/source.go`), and the next `Add` or resync pokes it.
- Enqueue: `persistentQueue.Add` (`varmq/persistent.go`) calls `enqueue`
  (`varmq/interface.go`), which takes the plain `Enqueue` path because the
  default `jobIdGenerator` returns `""` (`defaultConfig`, `varmq/config.go`),
  so `EnqueueOnce` and the dedup window are never used. `Queue.Enqueue`
  (`redisq/queue.go`) is one `RPUSH`, not a pipeline: the queue expiration
  is 0. `Add` then pokes the source's ready hint and signals the event loop
  (`poke`, `notifyToPullNextJobs`).
- Ack: `job.closeOnce` calls `job.acknowledge` (`varmq/job.go`), which calls
  `Queue.Acknowledge` (`redisq/queue.go`): one `HDEL`+`ZREM` pipeline, 1
  round trip per job, run by the pool goroutine right after the handler
  returns (`initPoolNode`, `varmq/worker_pool.go`). The ack runs synchronously
  in that goroutine, and the worker slot is freed only after it returns
  (`busyWorkers` decremented, then `finishJobs` and `notifyToPullNextJobs`),
  which is why the next pop's size tracks completions. Acks are not batched.
  The ack id is `<prefix>:<reservation>:<index>`, the prefix 128 random bits
  per `Queue` object (`newID`, `ackIDOf`).
- Wake: in process, no polling on the hot path. `Add` pokes and signals,
  `Start` (`varmq/worker_lifecycle.go`) signals, `bind` signals once when
  the queue is bound, and a pool goroutine signals after each job
  (`initPoolNode`). The signal is a non-blocking send on a channel of capacity
  1 (`notifyToPullNextJobs`, `eventLoopSignalCap` in `varmq/worker.go`), so
  signals coalesce. No per-job `LLEN`: `creditManager.Len` returns the ready
  hint (`batchAckSource.ready`, no I/O), and the credit branch of the event
  loop never calls `worker.pending`.
- No mutex across Redis calls on the hot path: the v1.5.0 `Queue` keeps its
  config in atomics (the `Queue` struct, `redisq/queue.go`) and holds no
  lock around a command, script or pipeline, so adds, pops and acks from
  many goroutines run concurrently. The Redis calls under a lock are at
  Close only: varmq's `Wait` (`varmq/worker_wait.go`) holds `w.mx` while
  `NumPending` runs its `LLEN`, and `Queue.Close` runs `releaseScript`
  inside its `closeOnce` `sync.Once` (`redisq/queue.go`).
- Idle wait: when the source is dry it arms a 1 s resync (`markDry`,
  `armResync`, `resync` in `varmq/source.go`): one `Queue.Len` (`LLEN`) per
  second while the worker is active, the hint is dry and this is its only
  source; it pokes if items wait. That is how a second process's adds would
  be found, up to 1 s late; the harness keeps this subject at 1 instance,
  where the producer is the consumer's own `Add` (`varmqConsumer`,
  `redis/varmq.go`).
- Round trips per job, consumer side: 1 ack pipeline per job plus 1 pop
  script per k jobs, where k is the free slots at each wake, at most
  `min(workers, 64)` and further limited by `popLen`. The first pass, with
  every worker idle, asks for the full credit (up to 10 at W10, up to 64 at
  W100 and W1000) and pops what is queued, one or a few jobs with the `1u`
  workload; later passes ask for what finished since the last one, because
  signals coalesce and a slot is freed only after the handler and its ack
  return (`dispatchCredit`, `initPoolNode`). On `Sleep1ms` at W10 k is
  likely nearer 1 than 10, an estimate that assumes pop and ack round-trip
  times, not only the code; with a trickle k is 1, 2 round trips per job.
  Not measured. Producer side: 1 per job (`RPUSH`). Not per job: the background
  work below.
- Not per job: `newQueue` (`redisq/queue.go`) runs `RequeueNackedItems`
  once and starts `sweep`, one `tickScript` per second per `Queue` object,
  which requeues items past their visibility deadline, renews this object's
  heartbeat and requeues the items of owners whose heartbeat ran out
  (`tickBody`). It also calls `PUBSUB NUMSUB` for each live owner's wake
  channel and ignores the result here, since only a distributed owner has a
  presence row. In the end-to-end runs the producer is the consumer's own
  `Queue` object (`varmqConsumer`, `redis/varmq.go`), so one sweep runs;
  `BenchmarkEnqueue`'s producer (`varmqProducer`) is a separate object with
  its own. Heartbeats are on by default: `SetHeartbeatTimeout` defaults to
  30 s (`newQueueBase`), and each pop that hands out items beats inside its
  script (`beatFn`), so no extra round trip. The adapter sets neither the
  heartbeat nor the visibility timeout, which stays at 5 min (`newQueueBase`).
- Pool size: `redisq.New` (`redisq/queues.go`) parses the URL with
  `redis.ParseURL`, which reads `pool_size` (`setupConnParams`,
  `go-redis/v9/options.go`), so the adapter appends it with `withPoolSize`
  (`redis/types.go`). `New` also sets a 3 s read timeout and a 512 ms max
  retry backoff unless the URL sets them, and one dial attempt, which a URL
  cannot change (`DialerRetries`). The ack pipelines go through go-redis's
  separate pipeline pool, up to 10 at a time (`DefaultPipelinePoolSize`,
  `go-redis/v9/options.go`); the rest spill to the main pool
  (`withPipelineConn`, `go-redis/v9/redis.go`), so with more than 10 acks
  in flight, as at W100 and W1000, the others run on `pool_size` connections.
  `pipeline_pool_size` is a URL option the adapter does not set.
- Batch size: `maxCredit` is 64 (`varmq/worker_credit.go`), the most jobs
  one pop takes. `ackSource.maxBatch` stays 1 (`varmq/source.go`); it only
  applies to the classic loop that drains jobs parked by a stop or pause.
- Serialization: `job.Json` (`varmq/job.go`) marshals `{id, status, data}`
  with the payload as `data` (`id` is empty here); the consumer parses it
  back in `toJob` (`varmq/source.go`).
- Close: the adapter calls `WaitAndStop` (`varmq/worker_wait.go`), which
  polls `NumPending` (`varmq/worker.go`; one `LLEN` per check, re-checked
  every 10 ms while nothing runs, `waitRecheck`) until the queue and the
  processing count are 0, then `Stop`. `Queue.Close` (`redisq/queue.go`)
  then cancels the sweep and runs `releaseScript` once if the object ever
  popped: an `HKEYS` scan of `bench:nacked` that requeues this object's
  un-acked items and drops its heartbeat (`releaseBody`, 3 s timeout). After
  `Wait` it finds nothing to move.
- Logging: varmq prints nothing (its default error handler is a no-op,
  `varmq/config.go`). `redisq` logs through the std `log` package; `TestMain`
  discards it.

### Asynq

Library version `asynq v0.26.0`. Paths are inside the module (`go env GOMODCACHE`).

- Dequeue: `processor.exec` (`processor.go`) calls `RDB.Dequeue`
  (`internal/rdb/rdb.go`), which runs `dequeueCmd`, a Lua script, with
  `EVALSHA` (falling back to `EVAL` on `NOSCRIPT`, `Script.Run` in
  `go-redis/v9/script.go`). Keys: `asynq:{bench}:pending`, `:paused`, `:active`,
  `:lease`. The script checks `EXISTS` on the paused key, `RPOPLPUSH` pending to
  active, `HSET`+`HDEL` on the task hash `asynq:{bench}:t:<id>`, `ZADD` on the
  lease zset, then `HGET`s the message. Enqueue is `RDB.Enqueue`: one
  `enqueueCmd` script (`EXISTS`, `HSET`, `LPUSH`), plus one `SADD` of the queue
  name on a client's first enqueue (`Client.EnqueueContext`, `client.go`).
- Dequeue concurrency: one goroutine (`processor.start`) loops `exec`, so one
  `Dequeue` is in flight per server. The handler and its ack run in a goroutine
  per task, up to `Concurrency` (the `sema` channel).
- Ack: a handler that returns nil goes to `processor.handleSucceededMessage`.
  `Retention` is 0 unless the caller sets it (`composeOptions`, `client.go`),
  so it calls `markAsDone` and `RDB.Done`: one `doneCmd` script (`LREM` active,
  `ZREM` lease, `DEL` task hash, `INCR` the daily processed counter plus
  `EXPIREAT` on its first hit, `GET` then `INCR`/`SET` of the total). Completed
  tasks are not kept.
- Wake: none. Polling only. The comment in `processor.exec` says it polls the
  queues instead of using a blocking pop. An empty
  `Dequeue` makes the processor sleep. Pub/sub exists (`subscriber.go`,
  `RDB.CancelationPubSub`), but its use here is task cancelation, not job wake
  (read from the file name and calls; not traced further).
- Idle wait: when `Dequeue` returns `ErrNoProcessableTask`, `processor.exec`
  runs `time.Sleep(taskCheckInterval/2 + rand.N(taskCheckInterval))`.
  `TaskCheckInterval` defaults to 1 s (`defaultTaskCheckInterval`,
  `server.go`; `NewServerFromRedisClient`), so each idle sleep is uniform in
  [0.5 s, 1.5 s). The sleep is not interruptible and holds a worker token.
  Jobs enqueued during it wait it out. Measured in a separate 40-job probe, not
  the `Latency` benchmark: median enqueue-to-handled latency about 990 ms and
  max 1.4 s over 40 sequential jobs, so the `Latency` benchmark runs 1,000
  jobs, one at a time, for every subject: Asynq's default idle poll would make
  a 10,000-job paced run take about 2.75 hours per iteration.
- Shutdown: `Server.Shutdown` (`server.go`) calls `processor.shutdown`, whose
  `stop` does `p.done <- struct{}{}`. That send is received only after the
  processor goroutine finishes its current `exec`, including the sleep above.
  So an idle `Shutdown` inside the timed region blocks for the rest of a
  0.5 to 1.5 s sleep. Measured on an idle server: 41 ms, 296 ms, 432 ms,
  795 ms, 1.07 s over 5 runs; 6 ms when stopped right after `Start`. The
  8 s `ShutdownTimeout` (`defaultShutdownTimeout`) only bounds in-flight
  handlers, and idle shutdown does not wait for it. We do not tune either.
- Round trips per job, consumer side: 2 (`Dequeue` script, `Done` script).
  Not per job: `heartbeater.beat` (`heartbeat.go`) writes server state and
  extends leases for active tasks every 5 s. Producer side: 1 per job (one
  `EVALSHA`), plus 1 `SADD` per client.
- Serialization: `base.EncodeMessage` (`internal/base/base.go`) marshals a
  protobuf `TaskMessage` (`internal/proto/asynq.proto`) carrying type, payload
  (our JSON bytes), id (UUID), queue, retry, timeout and the other fields. It
  is stored in the task hash field `msg`; the pending list holds only the id.
- Pool size: `RedisClientOpt.PoolSize` goes to `redis.Options.PoolSize`
  (`asynq.go`, `MakeRedisClient`). The server uses one pool for the processor,
  heartbeater and the other background jobs. The adapter sets `poolSize(workers)`
  on the server and on the client; the client passes 0 workers, which gives
  `10*GOMAXPROCS`, the go-redis default.
- Forced settings: queue `bench` with priority 1 (`Config.Queues`; asynq falls
  back to a `default` queue if none is set, `NewServerFromRedisClient`),
  `Concurrency = workers`, `Logger` no-op and `LogLevel = FatalLevel`. Task
  options are the defaults: `MaxRetry` 25 and `Timeout` 30 min (`client.go`),
  lease 30 s (`rdb.LeaseDuration`), no `Retention`, no unique key.
- Logging: `internal/log` writes to stderr when no logger is set
  (`NewLogger`). The adapter passes a no-op `asynq.Logger`. Nothing else in
  the library's non-test code prints to stdout or stderr: no startup banner,
  and the only std `log.Printf` is a bad-call path in `internal/errors`, which
  `TestMain` discards.

### VarMQDistributed

Library versions `varmq 415ad73` (unreleased, `replace` directive) and
`redisq v1.5.0`.

- Dequeue and ack: the same credit path as VarMQ (persistent).
  `redisq.DistributedQueue` embeds `*Queue` (`redisq/distributed.go`) and
  its own `DequeueBatchWithAckIds` passes the consumer id to the same
  `dequeueBatch` scripts (`redisq/queue.go`), so `dispatchCredit`
  (`varmq/worker_credit.go`) pops up to `min(free workers, 64)` jobs per
  script, sized by `popLen`, and each job is acked with one `HDEL`+`ZREM`
  pipeline (`job.acknowledge` in `varmq/job.go`, `Queue.Acknowledge`). With
  a consumer id the pop script also joins the idle set in the same step:
  `reply` in `popHead` runs `SADD bench:idle <id>` when the queue is empty
  after the pop and the pop took fewer items than the caller had slots.
  varmq's `fetch` (`varmq/source.go`) treats a full pop as not dry for a
  subscribed source, so a full pop that empties the queue is followed by
  one more pop, which returns nothing and joins the idle set.
- No mutex across Redis calls, and the ack synchronous in the pool goroutine
  before the slot is freed: as for VarMQ (persistent), with one `redisq.Queue`
  per instance.
- Enqueue: `distributedQueue.Add` (`varmq/distributed.go`) calls `enqueue`
  (`varmq/interface.go`), the plain path as for VarMQ (no job id), so
  `DistributedQueue.Enqueue` (`redisq/distributed.go`) runs one `listPushWake`
  script: `RPUSH`, then `wake` (`wakeFn`, `redisq/notification.go`), which
  `SPOP`s `bench:idle` and `PUBLISH`es `enqueued` on `bench:wake:<id>`, up to
  once per waiting item, stopping when the set runs out; an id whose `PUBLISH`
  reaches no subscriber does not count. One round trip per job whatever it
  wakes. `Notification.Send` and the shared `bench:notifications` broadcast
  are gone in v1.5.0.
- Wake: push to one consumer per item, plus a nudge. `newDistributedQueue`
  (`redisq/distributed.go`) builds the `Notification`, whose constructor
  calls `Client.Subscribe` on `bench:wake:<ackPrefix>` (`newNotification`)
  and whose `Start` runs the forwarding goroutine (`PubSub.Channel`) and
  `nudgeLoop`. `WithDistributedQueue` (`varmq/worker_binder.go`) binds the
  queue as a `batchAckSource`, which signals the event loop once at bind
  (`bind`, `varmq/source.go`), then `subscribe` marks the source subscribed
  and registers a handler that pokes the ready hint and calls
  `handleQueueSubscription`, a non-blocking signal to the event loop
  (`notifyToPullNextJobs`, `varmq/worker_eventloop.go`).
  `Notification.Subscribe` makes the instance a consumer and `SADD`s it to
  the idle set (`markIdle`). A `PUBLISH` reaches one idle consumer, not
  every instance. A job finishing signals the event loop as for VarMQ. Once
  a second `nudgeLoop` runs the `nudge` script (`LLEN`, then `SREM` of this
  consumer from the idle set if items wait, else `SADD` if its own last
  look found nothing) and, when items wait, delivers `enqueued` to the
  handlers itself, so a lost wake costs at most about 1 s. The sweep and
  `Close` of a distributed queue wake up to one idle consumer per item they
  requeue (`setOnRequeue(q.wake)`, `wakeIdle`). There is no resync timer:
  `armResync` (`varmq/source.go`) returns at once for a subscribed source.
  The go-redis pub/sub channel health check still sends a `PING` on the
  pub/sub connection when no message has arrived for 3 s (`newChannel`,
  `initHealthCheck` in `go-redis/v9/pubsub.go`; ping timeout 5 s, reconnect
  timeout 10 s). It has no effect on job pickup.
- Start-up: `Client.Subscribe` (`go-redis/v9/redis.go`) writes `SUBSCRIBE`
  on the pub/sub connection before it returns (`PubSub.subscribe` and
  `_subscribe` call `writeCmd` and do not read the reply), so the `SUBSCRIBE`
  has been written when `NewDistributedQueue` returns, though go-redis does
  not wait for Redis to confirm it. `NewWorker` starts the worker
  (`WithAutoRun` default) before `WithDistributedQueue` binds the queue; the
  bind signals a first pop, `Subscribe` joins the idle set, and jobs already
  waiting are found by that pop or by the nudge. That first pop can run before
  `Subscribe` sets the consumer id, in which case it does not join the idle set
  in-script (`Subscribe` does right after) and, if it comes up short, arms one
  1 s resync (`armResync`): it fires once, runs one `lenIdle` script if the
  hint is still dry (`resync`), and is not re-armed, since the source is
  subscribed by then.
- No `Listen` is needed: `Queue.Listen` (`redisq/queue.go`) only waits for
  SIGINT or SIGTERM. The adapter does not call it.
- Round trips per job, consumer side: 1 ack pipeline per job plus 1 pop
  script per k jobs, k as for VarMQ (persistent). Producer side: 1 script
  per job (`RPUSH` plus the wakes inside it).
- Not per job: per consumer, the `nudge` script once a second; per `Queue`
  object (the producer's included), the `sweep` script once a second
  (`tickScript`). In both subjects the sweep counts the subscribers of each
  live owner's wake channel with `PUBSUB NUMSUB`; only a distributed owner
  has a presence row, so only it can be taken for gone after 5 s without a
  subscriber (`tickBody`, `presenceGrace`). The producer's `Notification`
  subscribes to its own wake channel too, but its nudge loop does nothing,
  since the producer never calls `Subscribe` (`consumer` stays false).
  Heartbeat timeout 30 s, visibility timeout 5 min and dedup window 5 min
  stay at their defaults (`newQueueBase`). Each pop that hands out items
  beats inside its script, as for VarMQ.
- Pool size: `redisq.New` parses `pool_size` from the URL, as for VarMQ
  (persistent). The producer keeps `poolSize(0)`, which is `10*GOMAXPROCS`.
  The pub/sub connection comes from go-redis's separate pub/sub pool
  (`Client.pubSub` in `go-redis/v9/redis.go`), so it never counts against
  `pool_size`. The ack pipelines use its pipeline pool, up to 10 at a time
  (`DefaultPipelinePoolSize`, `go-redis/v9/options.go`); the rest spill to
  the main pool (`withPipelineConn`, `go-redis/v9/redis.go`), as for VarMQ.
- Close: the consumer calls `WaitAndStop` first (its `NumPending` runs the
  `lenIdle` script per check: `LLEN`, plus `SADD` to the idle set when 0),
  then `DistributedQueue.Close` (`redisq/distributed.go`): `leave` (`SREM`
  from the idle set), `Queue.Close` (cancel the sweep, `releaseScript` once
  if the object ever popped) and `Notification.Stop` (`SREM` again, cancel
  the nudge loop, close the pub/sub). Then the client.
- Serialization: as for VarMQ (persistent).
- Logging: `Notification` and `Queue` use the std `log`, which `TestMain`
  discards.

- Known issue (observed on redisq v1.4.0, not rerun on v1.5.0): the row
  `Sleep1msW1000C4/1u-100Kt/VarMQDistributed` (4 instances x 1000 workers,
  `Sleep1ms`, 1 producer x 100,000 jobs) stalled with 99,819/100,000 jobs
  handled, 0 duplicates, until the harness's 5-minute limit (`runEndToEnd`)
  failed it. That happened in an earlier full run on redisq v1.4.0 that is not
  committed; `bench/redis.txt` is regenerated at the v1.10.0 run. The stall is
  intermittent: an isolated rerun of 1 iteration passed, an isolated rerun of 8
  iterations stalled the same way, and isolated reruns of 10 and 40 iterations
  passed. The cause is not diagnosed (Redis was not inspected during a stall).
  On v1.4.0 an idle instance was woken only by a later `enqueued` broadcast, so
  a missed wake-up was the suspect, unverified; v1.5.0 replaced that broadcast
  with the per-consumer wake channels and the 1 s nudge above, so that
  mechanism no longer exists. The v1.4.0 start-up probes (5 pre-filled jobs
  drained in 150 of 150 rounds, `TestSubjects` at 4 instances passing 30 runs
  in a row) were measured against that broadcast too and have not been rerun.
  `go test` carried on after the FAIL and completed
  `Sleep1msW1000C4/1u-100Kt/Rmq`, then the run was stopped by hand, so that run
  has no VarMQDistributed `Sleep1msW1000C4/1u-100Kt` numbers, and no
  `Sleep1msW1000C4/100u-1Kt` rows for any subject. Treat them as unavailable
  until the v1.5.0 run shows otherwise.

### Rmq

Library version `rmq/v5 v5.2.0`. Paths are inside the module (`go env GOMODCACHE`).
The adapter opens one `rmq.OpenConnectionWithRedisClient` connection per
consumer instance and one for the producer, each on its own go-redis v9
client.

- Enqueue: `Queue.Publish` (`queue.go`) calls `RedisWrapper.LPush`
  (`redis_wrapper.go`): one `LPUSH` of the payload onto
  `rmq::queue::[bench]::ready`. One round trip per job. The producer's
  `Publish` takes no lock of its own.
- Fetch: `StartConsuming` (`queue.go`) starts one goroutine, `redisQueue.consume`,
  per queue. Each pass runs `consumeBatch`: one `LLEN` of this connection's
  unacked list (`unackedCount`), then, for `prefetchLimit - unacked` deliveries,
  one `RPOPLPUSH` ready to `rmq::connection::<conn>::queue::[bench]::unacked`
  per delivery (`RedisWrapper.RPopLPush`, not pipelined), each pushed into a
  channel of size `prefetchLimit`. An empty list ends the pass.
- Ack: `redisDelivery.Ack` (`delivery.go`) runs one `LREM unacked 1 <payload>`
  per job, one round trip. Nothing else is written per job. An ack that finds
  nothing to remove returns `ErrorNotFound`; the adapter ignores it.
- Round trips per job, consumer side: 2 (`RPOPLPUSH` and `LREM`), plus one
  `LLEN` per fetch pass, plus one `RPOPLPUSH` that returns nil when a pass
  drains the list.
- Wake and idle wait: polling only, no blocking pop and no push.
  `consume` calls `time.Sleep(jitteredDuration(pollDuration))` after every pass,
  including a pass that fetched jobs, not only an empty one. `jitteredDuration`
  (`queue.go`) is +/-10%, so 90 to 110 ms at our 100 ms. No back-off was found in
  `consume`: the sleep is the same on an empty queue. Redis errors only add an
  `errChan` send.
- Throughput cap: `consume` sleeps after every pass and a pass fetches at most
  `prefetchLimit - unacked` deliveries (`consumeBatch`), so one connection moves
  at most about `prefetchLimit / pollDuration` jobs per second: 1000 / 0.1 s =
  10,000 jobs/s per instance at `workers <= 500` (about 11,000 at the shortest
  jittered sleep of 0.09 s), and `2*workers / 0.1 s` above that. The cap is
  derived from the code, not measured. A pass also costs one serial
  `RPOPLPUSH` per delivery, which may bind before the cap.
- Heartbeat: `openConnection` (`connection.go`) starts a goroutine that runs
  `SET rmq::connection::<name>::heartbeat 1` with a 1 minute TTL every second
  (`heartbeatInterval`, `heartbeatDuration`). It is per connection, not per
  job. `openConnection` also runs `SADD rmq::connections`, `OpenQueue` runs
  `SADD rmq::queues`, `StartConsuming` runs one `SADD` for the connection's
  queue set, and each `AddConsumerFunc` (`addConsumer`) runs one `SADD`, serially. At 1000 workers that is
  1000 setup round trips, inside the timed region.
- Cleaner: `NewCleaner` (`cleaner.go`) has no caller inside rmq, so no cleaner
  runs unless the application starts one. The adapter does not. Unacked
  deliveries left by a stopped connection stay in its unacked list.
- Tag: `openConnection` names the connection `<tag>-<random 6>`, so the tag
  `bench` need not be unique. Consumers get `<tag>-<random 6>` too.
- Stop: `StopAllConsuming` (`connection.go`) calls `Queue.StopConsuming` for
  each opened queue. Its channel closes after `stopWg.Wait`, which waits for
  every consumer goroutine, but not for the fetch goroutine or the heartbeat
  goroutine. The fetch goroutine exits at its next check, so it can run up to
  one sleep longer; the adapter closes the go-redis client right after, so
  anything it still sends fails. `TestRmqStopEndsPollers` checks that the
  fetch and consumer goroutines end. rmq has no exported way to stop the
  heartbeat goroutine (`stopHeartbeat` is unexported). After the client closes,
  its `SET` fails once a second, and at `HeartbeatErrorLimit` (45) it sends one
  blocking error to `errChan` and exits. The adapter gives `errChan` a
  buffer of 1024 so that send completes with no reader. Net effect: each
  stopped connection leaves one idle goroutine for about 45 s.
- Serialization: `Publish` takes `...string` (`queue.go`), so the adapter sends
  `string(payload.marshal())`, the same JSON bytes as the other subjects.
  `newDelivery` calls `ExtractHeaderAndPayload` (`header.go`), which only
  strips a header when the payload starts with the signature `\xFF\x00\xBE\xBEJ`.
  Our JSON starts with `{`, so it passes through.
- Pool size: with `OpenConnectionWithRedisClient` rmq does not create the
  client, so the adapter sets `PoolSize` on its own go-redis client:
  `poolSize(workers)` per consumer instance, `poolSize(0)` for the producer.
- Logging: rmq does not log. Errors go to `errChan`, which a goroutine drains
  until stop.

### Taskq

Library version `taskq/v3 v3.2.9`, redis backend `redisq` imported as
`taskqredis`, which uses go-redis **v8** (`github.com/go-redis/redis/v8 v8.11.5`)
rather than the v9 the other subjects use. Paths are inside the module (`go env
GOMODCACHE`). The adapter builds one go-redis v8 client, one `redisq` factory and
one queue named `bench` per consumer instance, and the same per producer.

- Enqueue: `Queue.Add` (`redisq/queue.go`) calls `Queue.add`, which runs one
  `XADD` on the stream `taskq:{bench}:stream` with a single field `body`. One
  round trip per job, not pipelined. `add` also fills in a random UUID as the
  message ID, unrelated to the stream entry ID. `isDuplicate` only touches Redis
  when the message has a `Name`; ours has none.
- Serialization: `Message.MarshalBinary` (`message.go`) msgpack-encodes the
  message (task name, args bytes, reserved count, ID). The args are
  `msgpack.Marshal([]interface{}{payloadJSON})` (`Message.MarshalArgs`), so the
  adapter's JSON bytes ride as one msgpack `bin` inside a msgpack message
  inside the stream field. `MarshalBinary` compresses the args with s2 only at
  512 bytes or more; our JSON is about 140 bytes (computed, not measured on the
  wire), so no compression applies.
- Fetch: `Consumer.fetchMessages` (`consumer.go`) calls `Queue.ReserveN`, which
  runs `XREADGROUP GROUP taskq <host:pid:rand> COUNT <n> BLOCK <WaitTimeout>
  STREAMS taskq:{bench}:stream >`. `n` is `ReservationSize`. On `NOGROUP` it
  runs `XGROUP CREATE ... MKSTREAM` from `0` and retries.
- Fetchers: with `MinNumWorker == MaxNumWorker`, `Consumer.Start` does not
  start the autotuner (`consumer.go`, `Start`: it only runs when min < max). It
  calls `replaceConfig` with `NumFetcher: 0`, and `ensureFetcher`
  (called from `waitMessage`) then starts one fetcher, id 0. Nothing raises
  the count afterwards, so each consumer instance has one fetcher, regardless
  of worker count, feeding a buffered channel of `BufferSize` (default =
  `ReservationSize`, 10). The fetcher takes up to 10 jobs per `XREADGROUP`
  and blocks on the channel when it is full.
- Ack: `Consumer.Put` calls `Consumer.delete`, which calls `Queue.Delete`
  (`redisq/queue.go`): `XACK` and then `XDEL`, two separate commands, two
  round trips, run by the worker goroutine after the handler returns. The
  handler always returns nil, because a returned error makes taskq `Release`
  the message for a retry.
- Round trips per job: 2 consumer-side (`XACK`, `XDEL`) plus one `XREADGROUP`
  per up to 10 jobs; 1 producer-side (`XADD`).
- Idle wait: the fetcher blocks in `XREADGROUP` for `WaitTimeout`. The default
  is 10 s in `QueueOptions.Init` (`queue.go`), but `redisq.NewQueue` sets it to
  1 s first when it is zero (`redisq/queue.go`), so it is 1 s here. A worker
  with an empty buffer waits on a 1 s timer (`waitMessage`,
  `workerIdleTimeout`) and then loops. Redis wakes the fetcher as soon as an
  `XADD` arrives, so blocking is a push wake for a single consumer.
- Scheduler loops: `NewQueue` starts three goroutines per queue, producers
  included (`scheduler`: `delayed`, `pending`, `clean_zombie_consumers`).
  Each pass takes a Redis lock with `redislock.Obtain` and releases it
  (`withRedisLock`), runs one query, and sleeps `1000 + rand(500)` ms when it
  found nothing (`schedulerBackoff`). They are idle work, not job work, but they
  issue a few Redis commands per second per queue and the producer's one
  queue runs them too (the harness calls the producer factory once per
  iteration; the N producer goroutines share its one enqueue function), so each
  iteration runs 3 loops for the producer plus 3 per consumer instance. `pending`
  (`schedulePending`) re-queues entries unacked for `ReservationTimeout`
  (5 min), so a run longer than 5 min with slow acks could redeliver; the
  collector counts duplicates, and none were seen in `TestSubjects`.
- Stop: `Consumer.Stop` (`consumer.go`, `StopTimeout`) stops the fetchers first
  and waits for them, which means waiting out the fetcher's current
  `XREADGROUP` block (up to 1 s); idle workers return immediately on `stopCh`.
  The adapter then calls `factory.Close`, which closes the queue
  (`Queue.CloseTimeout` sets `_closed` and runs `XGROUP DELCONSUMER`), and
  then closes the client. The scheduler goroutines are not waited on
  (`CloseTimeout` never touches the queue's `WaitGroup`): they exit at their
  next wake because `scheduler` checks `closed()` first, and any call they have
  in flight when the client closes fails. So every stop costs up to about
  1 s, inside the timed region. `TestTaskqStopLeavesNothingWriting` checks
  that nothing writes to Redis after stop returns (a mutant that skips the
  close makes it fail: a scheduler recreates the stream group).
- Registry: `taskq.RegisterTask` is global and panics on a second register of
  the same name (`task.go`, `RegisterTask`, which wraps `registry.go`
  `TaskMap.Register`, the function that returns the error), so the adapter registers
  the task `bench` once at package level and routes to the current handler
  through an `atomic.Pointer`. All instances of one run share one handler, and
  the harness stops a round before the next starts. The queue registry is per
  factory (`internal/base/factory.go`, `Factory.Register`), so each instance
  has its own factory.
- Pool size: `poolSize(workers)` on our go-redis v8 `Options.PoolSize` per
  consumer instance, `poolSize(0)` per producer (v8's own default is
  `10 * GOMAXPROCS` as well).
- Logging: taskq logs through `internal.Logger`, set with `taskq.SetLogger`,
  and go-redis v8 through `redis.SetLogger`; the adapter sets both to no-ops
  in `init`.

### GocraftWork

Library version `github.com/gocraft/work v0.5.1`, which imports redigo as
`github.com/gomodule/redigo/redis` (`worker_pool.go`, `enqueue.go`), so the
adapter uses redigo `v1.9.3` and no old `garyburd` import is involved. Paths are
inside the module (`go env GOMODCACHE`). The adapter builds one redigo pool per
consumer instance (one `WorkerPool`), and one per producer (one `Enqueuer`).
Namespace `bench`, job name `bench`, so the job list is `bench:jobs:bench`.

- Enqueue: `Enqueuer.Enqueue` (`enqueue.go`) runs one `LPUSH bench:jobs:bench
  <job JSON>`, then `addToKnownJobs`, which runs `SADD bench:known_jobs bench`
  at least once per `Enqueuer` and again after 300 s. The check is not atomic
  (read under `RLock`, then `SADD`, then write under `Lock`), so concurrent first
  enqueues each issue their own `SADD`: 8 and 10 seen in two runs with 100 producers (`INFO
  commandstats` after `BenchmarkEnqueue/100u-1Kt`, against 100,000 `LPUSH`). So
  one round trip per job, not pipelined, plus a few extra at the start. The adapter
  holds one `Enqueuer` per producer factory call; the N producer goroutines of
  a run share it and its pool.
- Serialization: `Job.serialize` (`job.go`) is `json.Marshal` of a `Job`
  (`name`, `id`, `t`, `args`). The adapter's JSON rides as the string argument
  `p` inside `args`, so it is JSON-escaped inside the job JSON. The `id` is 12
  bytes from `crypto/rand` as hex (`identifier.go`, `makeIdentifier`), made on
  every `Enqueue`. The consumer side runs `json.Unmarshal` into a `Job`
  (`newJob`, `job.go`) and the adapter then unmarshals `p` a second time.
- Fetch: each worker is one goroutine running `worker.loop` (`worker.go`),
  which calls `fetchJob` and `processJob` in turn, so `concurrency` workers mean
  `concurrency` jobs in flight. `fetchJob` runs one Lua script through
  `redis.Script.Do` (`redisLuaFetchJob`, `redis.go`), which sends `EVALSHA` and
  falls back to `EVAL` once on `NOSCRIPT` (redigo `redis/script.go`, `Do`). For a
  non-empty queue the script runs `GET max_concurrency`, `LLEN`, `GET paused`,
  `GET lock`, `INCR lock`, `HINCRBY lock_info` and `RPOPLPUSH
  bench:jobs:bench bench:jobs:bench:<poolID>:inprogress`: 1 round trip and 7
  commands inside the script. On an empty queue it stops after the first two
  (`GET`, `LLEN`) and returns nil. The in-progress list is per `WorkerPool`.
- Priority sampling: `prioritySampler.sample` (`priority_sampler.go`) runs on
  every fetch, but its loop only runs while more than one job type is
  registered; we register one, so it does nothing. The default priority is 1
  (`applyDefaultsAndValidate`, `worker_pool.go`).
- Ack: after the handler returns, `removeJobFromInProgress` (`worker.go`) sends
  `MULTI`, `LREM <inprogress> 1 <raw job JSON>`, `DECR lock` and `HINCRBY
  lock_info <poolID> -1` with `conn.Send`, then runs `EXEC` with `conn.Do`,
  which flushes all five commands and reads all five replies (redigo
  `redis/conn.go`, `DoWithTimeout`): 1 round trip. The handler returns nil, so `jobFate` is not
  reached: no retry or dead-queue write.
- Per job, consumer side: 2 round trips (the fetch script, then the
  `MULTI`/`EXEC` pipeline). Idle polls and the background loops below come on
  top. `Job.Unique` is false, so `deleteUniqueJob` (an extra `DEL`) is skipped.
- Idle wait: polling only, no blocking pop and no push wake-up.
  `sleepBackoffsInMilliseconds = {0, 10, 100, 1000, 5000}` (`worker.go`).
  `loop` counts consecutive empty fetches; the first empty fetch sleeps 10 ms
  (index 1; index 0 is never used after an empty fetch), the second 100 ms, the
  third 1 s, and the fourth and every later one 5 s. A fetched job resets the
  count and the timer to 0. The sleep is a timer in the same `select` as the
  stop channel, so `Stop` does not wait out a sleep; it waits for a worker's
  current job to finish.
- Background loops, per `WorkerPool` (`WorkerPool.Start`, `worker_pool.go`):
  - observer, one per worker (`observer.loop`, `observer.go`): 1 s ticker,
    writes `HMSET`+`EXPIRE` (or `DEL` when idle) only when something changed
    since the last write, and writes immediately on its first observation. The
    worker hands it a start and a done observation per job over a 1,024-slot
    channel without touching Redis. So up to one 2-command pipeline per worker
    per second.
  - heartbeater (`heartbeater.go`, `beatPeriod`): 5 s, `SADD` + `HMSET`
    pipelined, and once right at start; `SREM` + `DEL` on stop.
  - retry and scheduled requeuers (`requeuer.loop`, `requeuer.go`): 1 s ticker
    each, one Lua `ZRANGEBYSCORE` per tick on `bench:retry` and
    `bench:scheduled`, which stay empty.
  - dead pool reaper (`dead_pool_reaper.go`): first pass after `deadTime`
    10 s, then every `reapPeriod` 10 min plus up to 29 s of jitter. It treats a
    pool as dead when its `heartbeat_at` is 10 s old or its heartbeat key is
    gone (`findDeadPools`), and then requeues that pool's in-progress jobs
    (`requeueInProgressJobs`). Every instance runs one. With the 5 s heartbeat a
    live pool is not reaped unless its heartbeat is late by more than 5 s; if it
    were, its in-flight jobs would be redelivered. Four probe runs of about 10 to 17 s
    (W10C1 `Sleep1ms` on both workloads, W10C4 `Sleep1ms` and W1000C4 `Noop` on
    `1u-100Kt`) saw no duplicates; that was not kept as a test.
  - periodic enqueuer (`periodic_enqueuer.go`): `GET last_periodic_enqueue`, and a
    `SET` if the key is absent or more than 2 s old (`shouldEnqueue` compares
    against `int64(periodicEnqueuerSleep/time.Minute)`, which is 2 seconds), at
    start and then every 2 min plus up to 29 s of jitter. We register no
    periodic jobs.
  - `Start` also runs `SET max_concurrency 0` for the job synchronously and
    launches `SADD bench:known_jobs bench` in a goroutine it never waits for
    (`writeConcurrencyControlsToRedis`, `writeKnownJobsToRedis`).
  So each instance runs 2 goroutines per worker (loop and observer) plus 5
  background goroutines.
- Stop: `WorkerPool.Stop` (`worker_pool.go`) sends a stop message to every
  worker (which also drains and stops its observer), then to the heartbeater,
  both requeuers, the reaper and the periodic enqueuer, and waits for each
  reply, so all of them have returned when it returns. The only goroutine it
  does not wait for is the known-jobs `SADD` above, which normally finishes
  right after `Start`. The adapter then closes the pool.
  `TestGocraftStopLeavesNothingRunning` checks that no goroutine inside
  `gocraft/work` remains after stop, that nothing writes after `FLUSHDB` (waits
  6 s, longer than the 5 s heartbeat), and that stdout stays empty (a mutant that
  skips `wp.Stop()` fails it).
- Pool size: `poolSize(workers)` for `MaxActive` and `MaxIdle` per consumer
  instance, `poolSize(0)` per producer. `Wait: true` is our choice, not a
  default: without it `Pool.Get` returns `ErrPoolExhausted` at `MaxActive`
  (redigo `redis/pool.go`), which gocraft/work would log instead of waiting.
- Logging: `logError` (`log.go`) is `fmt.Printf` to **stdout**, not the std
  `log` package, and has no hook, so `log.SetOutput` does not silence it. It is
  only called when a Redis call or a job fails (fetch, ack, heartbeat, requeue
  and observer errors, handler panics), so a healthy run prints nothing; the
  stop test above pins that for a short run. A line there would break the
  `vizb` parse of `redis.txt` and is a run failure to investigate.

### Work table

| Subject | Ack on hot path | Wake mechanism | Idle wait | Round trips per job | Serialization | Pool size | Forced settings |
|---------|-----------------|----------------|-----------|---------------------|---------------|-----------|-----------------|
| VarMQ (persistent) | one pop script per up to k jobs (`listAckPop`, `redisq/queue.go`: `HMGET` guard, `LRANGE`+`LTRIM`, `HSET`, `ZADD` deadline, `ZADD` heartbeat, `LLEN`; `LPOP` variant at k = 1), then one `HDEL`+`ZREM` pipeline per job (`Queue.Acknowledge`): 2 round trips per job at k = 1, falling toward 1 as k grows; k = 64 is the upper bound, not the expected value | in-process: `Add` pokes the ready hint and signals the event loop (`persistentQueue.Add`, `varmq/persistent.go`); no polling on the hot path | 1 `LLEN` per second while the hint is dry (`resync`, `varmq/source.go`), so a second process's adds would be seen up to 1 s late; the subject runs 1 instance | consumer side 1 ack pipeline per job plus 1 pop script per k jobs, k = the free slots at each wake, at most min(workers, 64), sized by `popLen`; the first pass asks for the full credit and pops what is queued, later passes ask for what finished since (not measured); producer side 1 (`RPUSH`); not per job: 1 sweep script per second per queue object | `varmq.Job` JSON (`{id, status, data}`) wrapping the payload | `poolSize(workers)` via `pool_size` in the URL; acks use go-redis's pipeline pool, up to 10 at a time, the rest spill to the main pool | `concurrency = workers` (`varmq.NewWorker(fn, workers)`, `redis/varmq.go`), queue key `bench`, `WithAutoRun(false)` on the Enqueue-only producer; defaults kept: visibility timeout 5 min, heartbeat timeout 30 s, dedup window 5 min |
| Asynq | `EVALSHA` Lua scripts: dequeue (`RPOPLPUSH` pending to active, `HSET`/`HDEL`, `ZADD` lease, `HGET`), then done (`LREM`, `ZREM`, `DEL`, `INCR`/`GET`/`INCR`), 2 round trips | polling: no blocking pop, no push wake | `time.Sleep(TaskCheckInterval/2 + rand.N(TaskCheckInterval))` on an empty queue, `TaskCheckInterval` default 1 s, so 0.5 to 1.5 s; idle `Shutdown` waits out the current sleep | 2 consumer-side, 1 producer-side (plus 1 `SADD` per client) | protobuf `TaskMessage` wrapping the JSON payload, in a task hash | `poolSize(workers)` via `RedisClientOpt.PoolSize` | queue `bench`; defaults kept: retry 25, timeout 30 min, lease 30 s, no retention |
| VarMQDistributed | same pop script and ack pipeline as VarMQ, with the consumer id so a short pop on an empty queue joins `bench:idle` in the same script | push to one consumer per item: the enqueue script `SPOP`s `bench:idle` and `PUBLISH`es `enqueued` on `bench:wake:<id>` (`listPushWake`, `redisq/distributed.go`); a 1 s `nudge` script per consumer re-delivers `enqueued` while items wait; sweep and `Close` wake for what they requeue | no polling on the hot path; the 1 s `nudge` script per consumer is a safety net for lost wakes; sweep script every 1 s per queue object; go-redis pub/sub `PING` every 3 s idle (`pubsub.go`, `initHealthCheck`) | consumer side 1 ack pipeline per job plus 1 pop script per k jobs, as VarMQ; producer side 1 script per job (`RPUSH` plus the wakes) | `varmq.Job` JSON wrapping the payload | `poolSize(workers)` via `pool_size` in the URL, `poolSize(0)` for the producer; pub/sub uses go-redis's own pub/sub pool; acks use its pipeline pool, up to 10 at a time, the rest spill to the main pool | `concurrency = workers` (`varmq.NewWorker(fn, workers)`, `redis/varmq_distributed.go`), queue key `bench`; defaults kept: visibility timeout 5 min, heartbeat timeout 30 s, dedup window 5 min |
| Rmq | `LREM unacked 1 <payload>` per job (`redisDelivery.Ack`, `delivery.go`), 1 round trip; fetch is one `RPOPLPUSH` ready to unacked per job (`consumeBatch`, `queue.go`) | polling only: no blocking pop, no push | `time.Sleep(jitteredDuration(pollDuration))` after every fetch pass, +/-10% (90 to 110 ms at our setting), not only when empty; no back-off found in `consume` | 2 consumer-side (`RPOPLPUSH`, `LREM`) plus 1 `LLEN` per pass, 1 producer-side (`LPUSH`) | plain string payload (the JSON) | `poolSize(workers)` on our go-redis client (rmq does not create it) | poll duration 100 ms and prefetch limit `max(1000, 2*workers)`: rmq has no default for either; both follow `example/consumer/main.go` in rmq v5.2.0 (`prefetchLimit = 1000`, `pollDuration = 100 * time.Millisecond`), with the prefetch raised to 2 x workers because rmq's `README.md` says the prefetch limit should always be greater than the number of consumers; `consume` sleeps after every batch, so a connection is capped near prefetch / poll (about 10,000 jobs/s at 1000 / 100 ms; derived from `consume`, not measured); queue `bench`, tag `bench`; no cleaner |
| Taskq | `XACK`, then `XDEL`, run separately by the worker after the handler (`Queue.Delete`, `redisq/queue.go`), 2 round trips | blocking: `XREADGROUP ... BLOCK` by one fetcher per instance, woken by `XADD` | `WaitTimeout` 1 s: `redisq.NewQueue` sets it before the 10 s default in `QueueOptions.Init`; workers re-check an empty buffer every 1 s; 3 scheduler loops per queue sleep 1 to 1.5 s between passes; `Stop` waits out the fetcher's current block (up to 1 s) | 2 consumer-side (`XACK`, `XDEL`) plus 1 `XREADGROUP` per up to 10 jobs, 1 producer-side (`XADD`) | msgpack `Message` holding msgpack args holding the JSON payload as bytes, in the stream field `body` | `poolSize(workers)` on our go-redis v8 client (`Options.PoolSize`) | `MinNumWorker = MaxNumWorker = workers` (disables autotune, so one fetcher); defaults kept (all in `QueueOptions.Init`, `queue.go`, except as noted): `ReservationSize` 10, `BufferSize` = `ReservationSize`, `ReservationTimeout` 5 min, `WaitTimeout` 1 s (`redisq.NewQueue`, `redisq/queue.go`), retry limit 64 (`TaskOptions.init`, `task.go`); queue `bench`, task `bench`; handler always returns nil |
| GocraftWork | `LREM <inprogress> 1 <raw job>` in a `MULTI`/`EXEC` with `DECR lock` and `HINCRBY lock_info` (`removeJobFromInProgress`, `worker.go`), after the handler, 1 round trip; fetch is one Lua script (`redisLuaFetchJob`, `redis.go`) doing `RPOPLPUSH` to a per-pool in-progress list, 1 round trip | polling only: no blocking pop, no push | per-worker backoff `{0, 10, 100, 1000, 5000}` ms after consecutive empty fetches, reset to 0 by a job (`sleepBackoffsInMilliseconds`, `worker.loop`, `worker.go`); `Stop` does not wait out a sleep; other loops: observer 1 s per worker, heartbeat 5 s, requeuers 1 s, reaper first at 10 s then every 10 min, periodic enqueuer 2 min | 2 consumer-side (fetch script, `MULTI`/`EXEC`) plus 1 script per idle poll, 1 producer-side (`LPUSH`; plus at least 1 `SADD` per `Enqueuer`, one per concurrent first enqueue (8 and 10 seen in two runs with 100 producers), and again every 300 s) | JSON `Job` whose `args.p` holds the JSON payload as a string | `poolSize(workers)` for redigo `MaxActive` and `MaxIdle`, `Wait: true` (our choice) | none beyond pool size; `MaxFails` 4 and priority 1 stay at their defaults (`applyDefaultsAndValidate`, `worker_pool.go`) and are not reached because the handler returns nil; namespace `bench`, job `bench`; `logError` prints to stdout (`log.go`) and cannot be redirected |
