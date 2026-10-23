# SQLite queue benchmarks

End-to-end benchmarks for [sqliteq](https://github.com/goptics/sqliteq), VarMQ's
SQLite storage adapter: jobs go in through a VarMQ persistent queue, a worker
runs them, and the benchmark waits until the queue is empty. Unlike
[durable/](../durable/README.md) there are no rival libraries. The committed
`bench/sqlite.txt` (24 CPUs) and `bench/sqlite-4cpu.txt` (4 CPUs) are a baseline
for one sqliteq version (v0.2.3), and the same benchmark is run again on a later
version to see what a change bought.

It lives in its own Go module, like `durable/`, so sqliteq's cgo SQLite driver
stays out of the root `go.mod`. It needs a C compiler and nothing else.

## Run

```sh
cd sqlite
SQLITEQ_LABEL=v0.2.3 go test -run xxx -bench . -benchtime=1x -count=2 -cpu=4
SQLITEQ_LABEL=v0.2.3 go test -run xxx -bench . -benchtime=1x -count=2 -cpu=24
```

or `task bench-sqlite` from the repo root, which runs both and saves the
`-cpu=24` run to `bench/sqlite.txt` and the `-cpu=4` run to
`bench/sqlite-4cpu.txt` (about 4 minutes). Each run replaces its own file. They
are two files, like `bench.txt` and `bench-4cpu.txt`, because the chart tool
drops the `-N` suffix from row names, so one file with both CPU counts would be
charted as one bar per row mixing 4- and 24-CPU samples.

Run each `-cpu` value in its own `go test` process, never `-cpu=4,24`. Go's
testing package runs a benchmark's first iteration (run1) before it sets
GOMAXPROCS for the current `-cpu` entry, and with `-benchtime=1x` the first
sample reuses it, so in a multi-value process the first `-4` sample runs at the
GOMAXPROCS left over from the last entry (24) under a `-4` label. A/B
comparisons between sqliteq versions must also use separate processes per `-cpu`
value.

- `SQLITEQ_LABEL` is the last part of every row name (default `sqliteq`). Run
  two versions with two labels and put both versions' rows into one chart per
  CPU count.
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

Row names are `BenchmarkSQLite/<scenario>/<layout>-ckpt<n>/<label>`:

- layout `Folder` is one database file per queue; `SingleFile` is every queue
  in one file. Only `Mixed10Q` runs both, since with one queue they are the
  same thing; the others run `SingleFile`.
- `ckpt<n>` is the WAL checkpoint size in pages, 1000 or 10000. sqliteq v0.2.3
  cannot set it, so its `ckpt10000` rows are skipped and do not appear in the
  output; later versions fill them in.
- Metrics: `jobs/s`, `walMB` and `procs`. Setup, teardown and the checks below
  are not timed.
- `procs` is the GOMAXPROCS the benchmark function actually ran at, read inside
  it. It must equal the row's `-N` suffix: a row whose `procs` does not match
  its suffix is invalid and must be discarded and re-run.
- `walMB` is the largest size, in MB (10^6 bytes), that any one `-wal` file of
  the run reached. A goroutine reads the size of every `-wal` file in the run's
  directory every 50 ms, from the store's opening until just before it closes,
  so a drain row includes its untimed adding phase. With several iterations the
  largest peak is reported. A peak that lasts under 50 ms can be missed.

After `Wait`, a run fails if any `Add` returned false or any row is still in a
queue table (counted with `database/sql`, not through sqliteq). A lost or stuck
job therefore fails the benchmark instead of making it look fast.

## Results

`bench/sqlite.txt` and `bench/sqlite-4cpu.txt`: sqliteq v0.2.3, from one
`-benchtime=1x -count=2 -cpu=4,24` run, split by CPU count afterwards. This is a
baseline only: it predates the one-process-per-`-cpu` rule above, so the first
`-4` sample of every row ran at GOMAXPROCS 24, and both files are regenerated
before sqliteq v0.3.0 publishes. Every row is repeated four times (two counts,
two CPU settings); the numbers below are the range over those four.

| Scenario | Layout | jobs/s |
|----------|--------|-------:|
| `Mixed1Q` | SingleFile | 1,342 to 1,399 |
| `Drain1Q` | SingleFile | 1,228 to 1,368 |
| `DrainPrio` | SingleFile | 848 to 873 |
| `Mixed10Q` | Folder | 9,331 to 13,681 |
| `Mixed10Q` | SingleFile | 2,853 to 3,731 |

Ten queues in separate files run roughly 3 to 4 times faster than the same ten
in one file, which fits SQLite allowing one writer per database file.

The machine is a 13th Gen Intel i7-13700 on ext4 over NVMe. Numbers from another
disk will differ a lot; compare runs on the same machine only.

## Swapping the sqliteq version

Everything that depends on the sqliteq version is behind
`openStore(tb, path, layout, ckpt)` in `open_v02.go`, which is built unless the
`sqliteq_v03` tag is set. A second file with that tag opens the newer API behind
the same function. For a local checkout, uncomment the `replace` lines in
`go.mod`.
