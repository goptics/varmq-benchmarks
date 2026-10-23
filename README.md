# VarMQ Benchmarks

[![Benchmark Charts](https://img.shields.io/badge/%F0%9F%93%8A-Live_Charts-blue?style=for-the-badge)](https://goptics.github.io/varmq-benchmarks/)

How fast is [VarMQ](https://github.com/goptics/varmq) compared to other Go worker pools, and under which conditions?

This repo answers that with one benchmark harness, a fixed set of workloads, and a live chart site that updates itself on every push.

## Vision

Benchmark numbers in library READMEs are easy to cherry-pick. We wanted the opposite:

- **One harness, many pools.** Every pool is driven by the exact same code path. A pool is just a `submit` function and a `teardown` function.
- **Realistic shapes, not a single number.** A pool that shines with one producer and a million tasks can fall apart with a million producers submitting one task each. We test both ends and the middle.
- **Overhead in isolation.** With tens or hundreds of thousands of workers a slow pool can hide behind parallelism. A pool of one worker takes that cover away and shows what each pool costs per task.
- **Reproducible and public.** Raw `go test -bench` output is committed to `bench/`. Charts are built from those files in CI and published to GitHub Pages, so anyone can see the data behind every bar.
- **Honest baselines.** Alongside real libraries we include two hand-rolled channel pools. If a library cannot beat a `for task := range ch` loop, that is worth knowing.

## What gets benchmarked

### Subjects

| Name | What it is |
|------|------------|
| **VarMQ** | [goptics/varmq](https://github.com/goptics/varmq), worker bound to a queue |
| **Pond** | [alitto/pond](https://github.com/alitto/pond) v2 |
| **Ants** | [panjf2000/ants](https://github.com/panjf2000/ants) v2 |
| **WorkerPool** | [gammazero/workerpool](https://github.com/gammazero/workerpool) |
| **Conc** | [sourcegraph/conc](https://github.com/sourcegraph/conc) `pool` with a goroutine cap |
| **ErrGroup** | [x/sync/errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup) with `SetLimit`. Not a pool: a new goroutine per task behind a semaphore, the stdlib-adjacent default many projects reach for |
| **Gopool** | [bytedance/gopkg](https://github.com/bytedance/gopkg) `util/gopool`. Tasks go to an unbounded list, so `submit` never blocks; idle workers exit on their own. The worker cap is soft and can overshoot slightly under contention |
| **GoroutinePool** | N goroutines reading from an **unbuffered** channel. Baseline. |
| **BufferedPool** | Same, but the channel holds 1M tasks. Baseline. |

All subjects live in [subjects.go](subjects.go).

### Dimensions

Every subject is run across the full product of these axes, except that a pool of one worker runs only the CPU tasks (see **Pool size**):

**Workload shape** (producers x tasks per producer, always 1M tasks total)

| Name | Producers | Tasks each |
|------|-----------|------------|
| `1u-1Mt` | 1 | 1,000,000 |
| `100u-10Kt` | 100 | 10,000 |
| `1Ku-1Kt` | 1,000 | 1,000 |
| `10Ku-100t` | 10,000 | 100 |
| `1Mu-1t` | 1,000,000 | 1 |

**Task type**

| Name | Behaviour |
|------|-----------|
| `Sleep10ms` | I/O-like: sleep 10 ms |
| `Loop10` | CPU-like: 10 iterations of trivial math |
| `Sleep5msLoop5` | Mixed: 5 iterations then sleep 5 ms |
| `Loop10K` | CPU-like: 10,000 iterations, about 1 µs. Pool of 1 only. |

**Pool size**

| Size | Tasks | What it shows |
|------|-------|---------------|
| 50K workers | `Sleep10ms`, `Loop10`, `Sleep5msLoop5` | How the pool scales when work can run in parallel |
| 100K workers | `Sleep10ms`, `Loop10`, `Sleep5msLoop5` | The same, with twice the goroutines to spawn and schedule |
| 300K workers | `Sleep10ms`, `Loop10`, `Sleep5msLoop5` | The same, at six times the 50K size |
| 1 worker | `Loop10`, `Loop10K` | What each pool costs per task: submit, hand-off and wake-up, with no parallelism to hide it |

A pool of one runs no sleeping tasks: every pool would show the same `time.Sleep`, and one worker sleeping 10 ms per task needs almost three hours for 1M tasks. Sizes and their tasks live in `poolSizes` in [subjects.go](subjects.go).

**CPU count**: results are recorded for the machine default (24 cores on the reference machine), 8 cores and 4 cores (`-cpu` flag).

### Benchmark names

Every row is named `BenchmarkTasks/<Task>W<size>/<workload>/<pool>`, where `W` stands for workers and sizes in thousands end in `K` (`W50K`, `W100K`, `W300K`, `W1`), in the same style as the workload names:

| Row | Task | Pool size | Workload | Pool |
|-----|------|-----------|----------|------|
| `BenchmarkTasks/Loop10W50K/100u-10Kt/VarMQ` | `Loop10` | 50,000 workers | 100 producers x 10K tasks | VarMQ |
| `BenchmarkTasks/Sleep10msW300K/1Mu-1t/Pond` | `Sleep10ms` | 300,000 workers | 1M producers x 1 task | Pond |
| `BenchmarkTasks/Loop10KW1/1u-1Mt/Ants` | `Loop10K` | 1 worker | 1 producer x 1M tasks | Ants |

### Metrics

Each benchmark reports the standard Go numbers plus throughput:

- `ns/op`: wall time to push all 1M tasks through the pool and drain it
- `ops/s`: tasks completed per second (custom metric, higher is better)
- `B/op` and `allocs/op`: memory cost

On a `W1` row there is no parallelism, so `ns/op` divided by 1,000,000 is the time per task: the task itself plus everything the pool adds to it.

## How it works

```
subjects.go          types.go            benchmark_test.go
┌──────────────┐     ┌──────────────┐    ┌──────────────────────────────┐
│ VarMQ        │     │ poolSubmit   │    │ for each pool size           │
│ Pond         │ ──▶ │ poolTeardown │ ──▶│   for each of its task types │
│ Ants         │     │ workload     │    │     for each workload        │
│ WorkerPool   │     │ TaskConfig   │    │       for each subject       │
│ Conc         │     │              │    │                              │
│ ErrGroup     │     └──────────────┘    │         spawn producers      │
│ Gopool       │                         │         submit, wait, teardown│
│ GoroutinePool│                         │                              │
│ BufferedPool │                         │                              │
└──────────────┘                         └──────────────────────────────┘
```

1. **A subject is a factory.** Given `maxWorkers`, it returns `submit(task)` and `teardown()`. Nothing else leaks into the harness.
2. **Producers are goroutines.** For a `100u-10Kt` run the harness starts 100 goroutines, each calling `submit` 10,000 times. A `WaitGroup` counts task completions.
3. **One iteration = full lifecycle.** Build the pool, push 1M tasks, wait for all to finish, tear the pool down. Pool construction and shutdown are deliberately inside the timed region.
4. **Results go to text, text goes to charts.** `go test -bench` output is saved to `bench/*.txt`. [Vizb](https://github.com/goptics/vizb) turns each file into a chart JSON, merges them, and renders one HTML page.

### Publishing pipeline

Defined in [.github/workflows/bench.yml](.github/workflows/bench.yml). It runs whenever anything under `bench/` changes on `main`:

1. Four parallel jobs convert `bench.txt`, `bench-8cpu.txt`, `bench-4cpu.txt` and `durable.txt` to chart JSON.
2. A merge job combines the first three into `public/index.html` and renders `durable.json` on its own to `public/durable/index.html`.
3. Both pages are pushed to the `gh-pages` branch together and served at <https://goptics.github.io/varmq-benchmarks/> and <https://goptics.github.io/varmq-benchmarks/durable/>.

Benchmarks themselves do not run in CI. Runner hardware is noisy and shared, so numbers are generated on a known machine and committed.

## Running it yourself

Requires Go 1.26+. [Task](https://taskfile.dev) and [Vizb](https://github.com/goptics/vizb) are optional.

```bash
# quick sanity pass, one iteration of everything
go test -bench=. -benchtime=1x -benchmem
```

```bash
# one slice: Loop10 task, 50K workers, 100 producers, only the baselines
go test -run xxx -bench 'Tasks/Loop10W50K/100u-10Kt/(GoroutinePool|BufferedPool)$' -benchtime=1x
```

```bash
# only the pool of one worker, every task, workload and subject (about 40 s)
go test -run xxx -bench 'Tasks/W1$' -benchtime=1x
```

```bash
# only one pool size, here 100K workers (also W50K, W300K)
go test -run xxx -bench 'Tasks/W100K$' -benchtime=1x
```

```bash
# regenerate bench/*.txt, build charts, merge, open dist/index.html (slow, runs the full matrix three times)
task report
```

Run `task --list` for everything else (filtered runs, profiling, clean).

### Adding a pool

Append an entry to `subjects` in [subjects.go](subjects.go):

```go
{
    name: "MyPool",
    factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
        p := mypool.New(maxWorkers)
        return p.Submit, p.StopAndWait
    },
},
```

That is the whole integration. The harness picks it up on the next run. The factory is called with every pool size in `poolSizes`, including `1`, so it must work with a single worker.

## Reading the charts

- Chart groups are named after the task and pool size, for example `Loop10W50K` or `Loop10W1`. Inside a group, bars are grouped by workload shape, then by subject.
- The `W50K`, `W100K` and `W300K` groups show how each pool scales with parallel work; comparing the same group across sizes shows what more workers cost or buy. The `W1` groups show what each pool costs per task, with nothing running in parallel to hide it.
- Hover a bar for exact `ns/op`, `ops/s` and memory values.
- Switch between the default, 8-CPU and 4-CPU tabs to see how each pool scales down.
- Compare each library against **GoroutinePool** first. The gap to that baseline is the real overhead of the library.
- **ErrGroup** shows what skipping a pool costs: no worker reuse, one goroutine spawned per task. A pool that loses to it adds overhead without saving any.

## Layout

| Path | Purpose |
|------|---------|
| `subjects.go` | Pools under test, workloads, task types, pool sizes |
| `types.go` | Harness types and the task generator |
| `benchmark_test.go` | The benchmark loop |
| `bench/` | Committed raw benchmark output (`bench.txt`, `bench-8cpu.txt`, `bench-4cpu.txt`), source of truth for the charts |
| `durable/` | Separate Go module: the Redis-backed durable queue benchmarks, with its own [README](durable/README.md) |
| `bench/durable.txt` | Committed raw output of the durable benchmarks, source of truth for the durable chart page |
| `jsons/` | Local chart JSON, one per CPU count, plus `durable.json`. Gitignored. |
| `dist/` | Local merged chart data and the rendered `index.html`. Gitignored. |
| `Taskfile.yml` | Shortcuts for running, profiling and charting |
| `.github/workflows/bench.yml` | Chart build and GitHub Pages deploy |

## Durable queues

The pool benchmark above needs nothing but Go. Redis-backed job queues are a different race, so they live in their own module, [durable/](durable/): VarMQ's persistent and distributed queues against Asynq, rmq, taskq and gocraft/work, with a raw Redis list as the floor. The harness measures end-to-end jobs/s, enqueue-only jobs/s and per-job latency. The committed `bench/durable.txt` so far holds the end-to-end rows only (it stopped at a known stall, see [durable/README.md](durable/README.md)); enqueue and latency rows arrive with the next full run.

[![Durable Charts](https://img.shields.io/badge/%F0%9F%93%8A-Durable_Charts-blue?style=for-the-badge)](https://goptics.github.io/varmq-benchmarks/durable/)

These benchmarks need Redis. `task durable-up` starts one on port 6390 (Docker), then `task durable-report` runs the full matrix and builds the page. The root pool benchmark still needs nothing. Subject notes, forced settings and the work each library does per job are in [durable/README.md](durable/README.md).

## License

MIT. See [LICENSE](LICENSE).
