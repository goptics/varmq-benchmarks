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
  `BenchmarkLatency/NoopW10C1/1u-1Kt/<subject>`
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
  Jobs enqueued during it wait it out. Measured: median enqueue-to-handled
  latency about 990 ms and max 1.4 s over 40 sequential jobs, so the
  `Latency` benchmark runs 1,000 jobs, one at a time, for every subject:
  Asynq's default idle poll would make a 10,000-job paced run take about 2.75
  hours per iteration.
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

Library versions `varmq v1.9.0` and `redisq v1.4.0`.

- Dequeue and ack: the same path as VarMQ (persistent). `redisq.DistributedQueue`
  embeds `*Queue` (`redisq/distributed.go`), so `ackSource.Next`
  (`varmq/source.go`) reaches `Queue.DequeueWithAckId` (`redisq/queue.go`):
  `LPOP`, then one `HSET`+`ZADD` pipeline. The ack is one `HDEL`+`ZREM`
  pipeline (`job.acknowledge` in `varmq/job.go`, `Queue.Acknowledge`). The
  `Queue.Dequeue` that `DequeueWithAckId` calls is the embedded one, not
  `DistributedQueue.Dequeue`, so the `dequeued` notification is never sent on
  this path.
- Enqueue: `distributedQueue.Add` (`varmq/distributed.go`) calls
  `DistributedQueue.Enqueue` (`redisq/distributed.go`): the `RPUSH` pipeline
  from `Queue.Enqueue`, then `Notification.Send` (`redisq/notification.go`),
  one `PUBLISH` of `enqueued` on the channel `bench:notifications`. Two round
  trips per job, and the `Queue` mutex covers only the `RPUSH`.
- Wake: push, not polling. `newDistributedQueue` (`redisq/distributed.go`)
  creates the subscription (`newNotification` runs `Client.Subscribe`) and
  starts the goroutine that forwards messages. `WithDistributedQueue`
  (`varmq/worker_binder.go`) registers `handleQueueSubscription`, which on
  `enqueued` does a non-blocking signal to the worker's event loop
  (`notifyToPullNextJobs` in `varmq/worker_eventloop.go`). Redis `PUBLISH`
  reaches every subscribed connection, so every instance is woken by every
  enqueue, and each wake runs the `LLEN` checks below. A worker is also woken
  when a batch of jobs finishes (`varmq/worker_pool.go`) and by `Start`
  (`varmq/worker_lifecycle.go`). There is no job polling: the wake is push. The
  one idle timer is go-redis's: `Notification.Start` (`redisq/notification.go`)
  calls `PubSub.Channel()` with default options, and `newChannel` /
  `initHealthCheck` (`go-redis/v9/pubsub.go`) send a `PING` on the pub/sub
  connection when no message has arrived for 3 s (`checkInterval`; ping timeout
  5 s, reconnect timeout 10 s). It has no effect on job pickup.
- No `Listen` is needed: `Queue.Listen` (`redisq/queue.go`) only waits for
  SIGINT or SIGTERM, and the subscription already runs after
  `NewDistributedQueue`. The adapter does not call it.
- Start-up race: `Client.Subscribe` (`go-redis/v9/redis.go`) writes `SUBSCRIBE`
  before it returns, so the subscription is in place as soon as
  `NewDistributedQueue` returns. A job enqueued before that is still in the
  list. `NewWorker` starts the worker (`WithAutoRun` default) before
  `WithDistributedQueue` binds the queue, so the `Start` wake can run before
  the queue is bound. We did not see a job stranded that way: a test that
  pre-filled 5 jobs and then started a consumer drained them in 150 of 150
  rounds, and `TestSubjects` at 4 instances passed 30 runs in a row. A job
  finishing only wakes a worker that already had jobs; an idle instance is
  woken only by a later `enqueued` notification.
- Extra `LLEN`, one mutex, visibility timeout, batch size: as for VarMQ
  (persistent), with one `redisq.Queue` per instance. Four instances have four
  mutexes.
- Pool size: `redisq.New` parses `pool_size` from the URL, as for VarMQ
  (persistent). The producer keeps `poolSize(0)`, which is `10*GOMAXPROCS`. The
  pub/sub connection comes from go-redis's separate pub/sub pool
  (`Client.pubSub` in `go-redis/v9/redis.go`), so it is not counted in
  `pool_size`.
- Close: `DistributedQueue.Close` (`redisq/distributed.go`) calls
  `Notification.Stop`, which cancels the subscription, then `Queue.Close`. The
  consumer calls `WaitAndStop` first, then closes the queue and the client.
- Logging: `Notification.Send` and `Queue` use the std `log`, which `TestMain`
  discards.

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
  nothing to remove returns `ErrorNotFound`; the adapter counts it and moves on.
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

### Work table

| Subject | Ack on hot path | Wake mechanism | Idle wait | Round trips per job | Serialization | Pool size | Forced settings |
|---------|-----------------|----------------|-----------|---------------------|---------------|-----------|-----------------|
| RawRedis | none | blocking pop | `BRPOP` 1 s block timeout | 1 | raw JSON bytes | `poolSize(workers)` | none |
| VarMQ (persistent) | `LPOP`, then `HSET`+`ZADD` pipeline, then `HDEL`+`ZREM` pipeline (5 commands, 3 round trips) | in-process: `Add` signals the worker's event loop, no polling | none: no Redis polling or blocking wait; wakes come from in-process `Add`, `Start` and job completion, so an idle second instance is not woken by another process's adds | at least 6: `LLEN` x3, `LPOP`, `HSET`+`ZADD`, `HDEL`+`ZREM` | `varmq.Job` JSON wrapping the payload | `poolSize(workers)` via `pool_size` in the URL | none; visibility timeout stays at its 5 min default |
| Asynq | `EVALSHA` Lua scripts: dequeue (`RPOPLPUSH` pending to active, `HSET`/`HDEL`, `ZADD` lease, `HGET`), then done (`LREM`, `ZREM`, `DEL`, `INCR`/`GET`/`INCR`), 2 round trips | polling: no blocking pop, no push wake | `time.Sleep(TaskCheckInterval/2 + rand.N(TaskCheckInterval))` on an empty queue, `TaskCheckInterval` default 1 s, so 0.5 to 1.5 s; idle `Shutdown` waits out the current sleep | 2 consumer-side, 1 producer-side (plus 1 `SADD` per client) | protobuf `TaskMessage` wrapping the JSON payload, in a task hash | `poolSize(workers)` via `RedisClientOpt.PoolSize` | queue `bench`; defaults kept: retry 25, timeout 30 min, lease 30 s, no retention |
| VarMQDistributed | `LPOP`, then `HSET`+`ZADD` pipeline, then `HDEL`+`ZREM` pipeline (5 commands, 3 round trips), same as VarMQ | push: `PUBLISH enqueued` on `bench:notifications` after each `RPUSH`, received by every instance's subscriber; also job completion and `Start` | no job polling or blocking pop; the go-redis pub/sub channel health check pings every 3 s when idle (`pubsub.go`, `initHealthCheck`) | at least 6 consumer-side (`LLEN` x3, `LPOP`, `HSET`+`ZADD`, `HDEL`+`ZREM`), 2 producer-side (`RPUSH` pipeline, `PUBLISH`) | `varmq.Job` JSON wrapping the payload | `poolSize(workers)` via `pool_size` in the URL; pub/sub uses its own connection | none; visibility timeout stays at its 5 min default |
| Rmq | `LREM unacked 1 <payload>` per job (`redisDelivery.Ack`, `delivery.go`), 1 round trip; fetch is one `RPOPLPUSH` ready to unacked per job (`consumeBatch`, `queue.go`) | polling only: no blocking pop, no push | `time.Sleep(jitteredDuration(pollDuration))` after every fetch pass, +/-10% (90 to 110 ms at our setting), not only when empty; no back-off found in `consume` | 2 consumer-side (`RPOPLPUSH`, `LREM`) plus 1 `LLEN` per pass, 1 producer-side (`LPUSH`) | plain string payload (the JSON) | `poolSize(workers)` on our go-redis client (rmq does not create it) | poll duration 100 ms and prefetch limit `max(1000, 2*workers)`: rmq has no default for either; both follow `example/consumer/main.go` in rmq v5.2.0 (`prefetchLimit = 1000`, `pollDuration = 100 * time.Millisecond`), with the prefetch raised to 2 x workers because rmq's `README.md` says the prefetch limit should always be greater than the number of consumers; `consume` sleeps after every batch, so a connection is capped near prefetch / poll (about 10,000 jobs/s at 1000 / 100 ms; derived from `consume`, not measured); queue `bench`, tag `bench`; no cleaner |
