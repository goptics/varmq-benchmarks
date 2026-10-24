# VarMQ Benchmarks

[![Benchmark Charts](https://img.shields.io/badge/%F0%9F%93%8A-Live_Charts-blue?style=for-the-badge)](https://benchmarks.goptics.org)

How fast is [VarMQ](https://github.com/goptics/varmq) compared to other Go worker pools and job queues, and under which conditions?

This repo answers that with three benchmark suites, fixed workloads, and one live chart site at <https://benchmarks.goptics.org>.

## Vision

Benchmark numbers in library READMEs are easy to cherry-pick. We wanted the opposite:

- **One harness per suite, many subjects.** Every subject in a suite is driven by the exact same code path.
- **Realistic shapes, not a single number.** A pool that shines with one producer and a million tasks can fall apart with a million producers submitting one task each. We test both ends and the middle.
- **Overhead in isolation.** With tens or hundreds of thousands of workers a slow pool can hide behind parallelism. A pool of one worker takes that cover away and shows what each pool costs per task.
- **Reproducible and public.** Raw `go test -bench` output is committed to each suite's `bench/` directory. The site is built from those files, so anyone can see the data behind every bar.
- **Honest baselines.** In the pool suite, alongside real libraries, we include two hand-rolled channel pools. If a library cannot beat a `for task := range ch` loop, that is worth knowing.

## Suites

| Suite | Module | What is raced | Datasets | README |
|-------|--------|---------------|----------|--------|
| Pool | `pool/` | VarMQ and eight other in-process worker pools | [24 CPUs](https://benchmarks.goptics.org/?id=pool-24cpu), [8 CPUs](https://benchmarks.goptics.org/?id=pool-8cpu), [4 CPUs](https://benchmarks.goptics.org/?id=pool-4cpu) | [pool/README.md](pool/README.md) |
| Redis | `redis/` | VarMQ on Redis against Asynq, rmq, taskq and gocraft/work | [Persistent](https://benchmarks.goptics.org/?id=redis-persistent), [Distributed](https://benchmarks.goptics.org/?id=redis-distributed) | [redis/README.md](redis/README.md) |
| Sqlite | `sqlite/` | sqliteq driven through a VarMQ worker | [Persistent](https://benchmarks.goptics.org/?id=sqlite-persistent) | [sqlite/README.md](sqlite/README.md) |

**Pool.** Nine subjects (VarMQ, Pond, Ants, WorkerPool, Conc, ErrGroup, Gopool and two channel baselines) run the same five workload shapes of 1M tasks, at three pool sizes plus a single worker. The same matrix is recorded at 24, 8 and 4 CPUs, one dataset each.

**Redis.** VarMQ's Redis queue (redisq) is raced against Asynq, rmq, taskq and gocraft/work with real consumer processes. The Persistent dataset uses one consumer process and also carries enqueue-only and pickup-latency rows. The Distributed dataset uses four consumer processes.

**Sqlite.** sqliteq, the SQLite-backed persistent queue, through a VarMQ worker: one queue, drain, priority and ten queues, with a WAL checkpoint of 1000 and 10000 pages. It has its own module because it needs cgo.

## How results get published

1. `<module>/bench/*.txt` is raw `go test -bench` output, committed from the reference machine. Benchmarks never run in CI.
2. `task site:build` converts each file to a dataset JSON with [vizb](https://github.com/goptics/vizb), writes a catalog, and builds one HTML page, all under `worker/public/`.
3. CI runs `task site:build` and then `wrangler deploy`. Pull requests build only; the deploy runs on pushes to `main`. The workflow is [.github/workflows/site.yml](.github/workflows/site.yml).
4. A small Cloudflare Worker ([worker/](worker/)) serves the page and the datasets from Workers Static Assets at `benchmarks.goptics.org`.

| URL | Serves |
|-----|--------|
| `/` | The dashboard with the dataset switcher. Loads the catalog, then the first dataset |
| `/?id=<id>` | The same dashboard with one dataset selected, for example `/?id=redis-persistent` |
| `/<id>` | A single dataset with no switcher |
| `/dataset` | The catalog, `[{id, name}]` in dashboard order |
| `/dataset/<id>` | One dataset as JSON |

The dataset ids are `pool-24cpu`, `pool-8cpu`, `pool-4cpu`, `redis-persistent`, `redis-distributed` and `sqlite-persistent`. Only the latest run of each is kept.

Deploying needs two GitHub secrets:

- `CLOUDFLARE_API_TOKEN` with Workers Scripts: Edit, plus Zone goptics.org: DNS Edit and Workers Routes Edit for the custom domain.
- `CLOUDFLARE_ACCOUNT_ID`.

## Running it yourself

Requires Go 1.26+, [Task](https://taskfile.dev), [vizb](https://github.com/goptics/vizb) and `jq`. Redis suite: Docker. Sqlite suite: a C compiler. `task site:dev`: Node.

```bash
# every task, per suite and for the site
task --list
```

```bash
# one iteration of every pool benchmark
task pool:bench
```

```bash
# one iteration of every Redis benchmark (starts Redis on localhost:6390 first)
task redis:up redis:bench
```

```bash
# one iteration of every sqlite scenario
task sqlite:bench
```

```bash
# build all six datasets from bench/*.txt into one HTML, no Worker
task site:preview
```

```bash
# build against localhost:8787 and run the Worker with wrangler dev
task site:dev
```

```bash
# regenerate every bench/*.txt (hours), then open the local preview
task report
```

To run a slice, call `go test` inside the suite's module:

```bash
# pool: Loop10 task, 50K workers, 100 producers, only the baselines
cd pool && go test -run xxx -bench 'Tasks/Loop10W50K/100u-10Kt/(GoroutinePool|BufferedPool)$' -benchtime=1x
```

```bash
# pool: only the pool of one worker, every task, workload and subject (about 40 s)
cd pool && go test -run xxx -bench 'Tasks/W1$' -benchtime=1x
```

```bash
# redis: one Durable cell, needs task redis:up
cd redis && go test -run xxx -bench 'Durable/NoopW10C1' -benchtime=1x -timeout 0
```

```bash
# sqlite: one scenario
cd sqlite && go test -run xxx -bench Mixed10Q -benchtime=1x
```

## Layout

| Path | Purpose |
|------|---------|
| `pool/` | Pool suite: its own Go module, `bench/pool-{24,8,4}cpu.txt`, Taskfile and README |
| `redis/` | Redis suite: its own Go module, `compose.yml`, `bench/redis.txt`, Taskfile and README |
| `sqlite/` | Sqlite suite: its own Go module, `bench/sqlite.txt`, Taskfile and README |
| `worker/` | Cloudflare Worker (`worker.js`, `wrangler.jsonc`). `worker/public/` is build output and gitignored |
| `dist/` | Local preview from `task site:preview`. Gitignored |
| `Taskfile.yml` | Includes the three suite Taskfiles, plus `site:*`, `report` and `clean` |
| `.github/workflows/site.yml` | Site build and deploy |

## License

MIT. See [LICENSE](LICENSE).
