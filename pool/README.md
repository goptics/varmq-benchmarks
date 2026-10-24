# Pool benchmarks

Nine Go worker pools driven by one harness across five workload shapes and four task types, 1M tasks per run. VarMQ is raced against Pond, Ants, WorkerPool, Conc, ErrGroup, Gopool and two hand-rolled channel baselines.

This module is `package main` with its own `go.mod`. Run everything from this directory, or from the repo root as `task pool:<name>`.

## Run

| Task | What it does |
|------|--------------|
| `task pool:test` | `go vet` and `go test` |
| `task pool:bench` | Every benchmark once (`-benchtime=1x`) |
| `task pool:txt` | Full matrix at 24, 8 and 4 CPUs into `bench/pool-24cpu.txt`, `bench/pool-8cpu.txt` and `bench/pool-4cpu.txt` (hours) |

One slice, for example the pool of one worker (about 40 s):

```bash
go test -run xxx -bench 'Tasks/W1$' -benchtime=1x
```

Loop10 task, 50K workers, 100 producers, only the baselines:

```bash
go test -run xxx -bench 'Tasks/Loop10W50K/100u-10Kt/(GoroutinePool|BufferedPool)$' -benchtime=1x
```

One pool size, here 100K workers (also `W50K`, `W300K`):

```bash
go test -run xxx -bench 'Tasks/W100K$' -benchtime=1x
```

## Results

`bench/pool-24cpu.txt`, `pool-8cpu.txt` and `pool-4cpu.txt` are one run from
2026-10-24: varmq 415ad73 (unreleased, through the `replace` directive in
`go.mod`), `-benchtime=2x`, 495 rows each. Counting the fastest pool per row
over the 55 rows of each file (11 task and size groups x 5 workloads): VarMQ is
first in 40 rows at 24 CPUs, 36 at 8 and 20 at 4; BufferedPool takes 13, 13
and 20; ErrGroup 2, 6 and 11; Ants 4 at 4 CPUs. The one-worker `Loop10W1`
rows are the consistent loss, where VarMQ runs at about a quarter of
BufferedPool: with no parallelism the hand-off cost is all there is.

## Subjects

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

## Dimensions

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

## Benchmark names

Every row is named `BenchmarkTasks/<Task>W<size>/<workload>/<pool>`, where `W` stands for workers and sizes in thousands end in `K` (`W50K`, `W100K`, `W300K`, `W1`), in the same style as the workload names:

| Row | Task | Pool size | Workload | Pool |
|-----|------|-----------|----------|------|
| `BenchmarkTasks/Loop10W50K/100u-10Kt/VarMQ` | `Loop10` | 50,000 workers | 100 producers x 10K tasks | VarMQ |
| `BenchmarkTasks/Sleep10msW300K/1Mu-1t/Pond` | `Sleep10ms` | 300,000 workers | 1M producers x 1 task | Pond |
| `BenchmarkTasks/Loop10KW1/1u-1Mt/Ants` | `Loop10K` | 1 worker | 1 producer x 1M tasks | Ants |

## Metrics

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
4. **Results go to text, text goes to charts.** `task pool:txt` saves `go test -bench` output to `bench/pool-24cpu.txt`, `bench/pool-8cpu.txt` and `bench/pool-4cpu.txt`. The root `task site:build` turns each file into a dataset with [Vizb](https://github.com/goptics/vizb).

## Adding a pool

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
- The 24, 8 and 4 CPU runs are separate datasets (`pool-24cpu`, `pool-8cpu`, `pool-4cpu`) in the dataset switcher, so you can see how each pool scales down.
- Compare each library against **GoroutinePool** first. The gap to that baseline is the real overhead of the library.
- **ErrGroup** shows what skipping a pool costs: no worker reuse, one goroutine spawned per task. A pool that loses to it adds overhead without saving any.
