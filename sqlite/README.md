# SQLite queue benchmarks

End-to-end benchmarks for [sqliteq](https://github.com/goptics/sqliteq) v0.3.0,
VarMQ's SQLite storage adapter. Jobs go in through a VarMQ persistent queue, a
worker runs them, and the benchmark waits until the queue is empty. Unlike
[redis/](../redis/README.md) there are no rival libraries, so the results show
how sqliteq behaves under different layouts and checkpoint sizes.

It lives in its own Go module so sqliteq's cgo SQLite driver stays out of the
other modules. It needs a C compiler and nothing else.

## Run

```sh
task sqlite:test    # vet and compile
task sqlite:bench   # every scenario once
task sqlite:txt     # full run into bench/sqlite.txt
```

- `SQLITEQ_BENCH_DIR` is where the databases are created (default
  `<user cache dir>/varmq-bench-sqlite`). **Use a real disk.** Durable writes
  pay for fsync, and `/tmp` is often a tmpfs where that cost vanishes. Each
  iteration gets a fresh subdirectory and removes it afterwards.

## Scenarios

Every job is an `int`; the worker handler does nothing and runs 16 jobs at a
time (`WithConcurrency(16)`).

| Name | Queues | Producers per queue | Jobs | Timed |
|------|-------:|--------------------:|-----:|-------|
| `Mixed1Q` | 1 | 4 | 20,000 | first `Add` to `Wait`, producing and consuming at once |
| `Drain1Q` | 1 | 4 | 20,000 | worker paused, everything added, then `Resume` to `Wait` |
| `DrainPrio` | 1 | 4 | 10,000 | as `Drain1Q` on a priority queue, priority `i % 4` |
| `Mixed10Q` | 10 | 1 | 20,000 | as `Mixed1Q`, one worker per queue |

## Names and metrics

Row names are `BenchmarkSQLite/<scenario>/<layout>/ckpt<n>`, ten rows in all.

- Layout `Folder` is one database file per queue; `SingleFile` is every queue
  in one file. Only `Mixed10Q` runs both, since with one queue they are the
  same thing; the others run `SingleFile`.
- `ckpt<n>` is the WAL checkpoint size in pages, 1000 or 10000.
- `jobs/s` is the job count divided by the timed part. Setup, teardown and the
  checks below are not timed.
- `walMB` is the largest size, in MB (10^6 bytes), that any one `-wal` file of
  the run reached. A goroutine reads the size of every `-wal` file in the run's
  directory every 50 ms, from the store's opening until just before it closes,
  so a drain row includes its untimed adding phase. With several iterations the
  largest peak is reported. A peak that lasts under 50 ms can be missed.

After `Wait`, a run fails if any `Add` returned false, if the handler ran a
different number of times than jobs were added, or if any row is still in a
queue table (counted with `database/sql`, not through sqliteq). A lost or stuck
job therefore fails the benchmark instead of making it look fast.

## Dataset on the site

`bench/sqlite.txt` becomes the `sqlite-persistent` dataset: groups are the
scenarios, the y axis is the layout and the x axis is the checkpoint size.
`walMB` is marked lower-is-better.

## Results

Not committed yet. `bench/sqlite.txt` is written by `task sqlite:txt` on the
reference machine. The machine and disk change these numbers a lot, so compare
runs on the same machine only.
