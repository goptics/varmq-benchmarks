# VarMQ Benchmarks - Worker Pool Performance Testing

[![Benchmark Charts](https://img.shields.io/badge/%F0%9F%93%8A-Benchmark_Charts-blue?style=for-the-badge)](https://goptics.github.io/varmq-benchmarks/)

A comprehensive benchmark suite comparing Go worker pool implementations, focusing on VarMQ and PondV2 performance across different workload patterns.

## Why I chose PondV2 for comparison

I selected PondV2 as the comparison benchmark because it provides similar worker pool features to VarMQ, making it an ideal candidate for performance analysis. Interestingly, VarMQ was inspired by some design patterns from PondV2, creating a meaningful comparison between these two Go worker pool implementations.

## 🌐 Live Interactive Charts

**👉 [View Benchmark Results](https://goptics.github.io/varmq-benchmarks/)** — Auto-deployed from `bench/` data via GitHub Actions on every push to main.

The live site includes:

- 📊 **Interactive Performance Charts** — Compare VarMQ vs PondV2 across all workload patterns
- 🖥️ **Multiple CPU Configurations** — Results for default, 8, and 4 CPU cores
- 📈 **Detailed Metrics** — Execution time and memory usage analysis

## CI/CD Pipeline

Benchmark charts are automatically generated and deployed via the [bench workflow](.github/workflows/bench.yml):

1. Reads pre-generated bench txt files from `bench/`
2. Converts each to JSON using the [Vizb Action](https://github.com/goptics/vizb)
3. Merges all charts and deploys to GitHub Pages (`gh-pages` branch)

To update the live charts, commit updated bench txt files to the `bench/` directory.

## Overview

This project benchmarks worker pool libraries to understand their performance characteristics under various workload scenarios:

- Different user/task ratios (1 user with 1M tasks vs 1M users with 1 task each)
- Various task types (CPU-intensive loops, I/O simulation with sleep, mixed workloads)
- Multiple worker pool sizes (50k, 100k, 300k, 500k workers)

## Benchmarked Libraries

- **[VarMQ](https://github.com/goptics/varmq)** — High-performance message queue and worker pool with job queuing
- **[PondV2](https://github.com/alitto/pond)** — Minimalistic and high-performance goroutine worker pool written in Go

## Local Setup

### Prerequisites

- **Go 1.24+** — [Install Go](https://golang.org/doc/install)
- **Task** (optional but recommended) — [Install Task](https://taskfile.dev/installation/)
- **Vizb** (optional for visualization) — [Install Vizb](https://github.com/goptics/vizb)

### Quick Start

1. **Clone the repository:**

   ```bash
   git clone https://github.com/goptics/varmq-benchmarks.git
   cd varmq-benchmarks
   ```

2. **Setup development environment:**

   ```bash
   # With Task (recommended)
   task setup

   # Or manually
   go mod download
   go mod tidy
   ```

3. **Run benchmarks:**

   ```bash
   # With Task
   task bench-quick

   # Or manually
   go test -bench=. -benchtime=1x -benchmem
   ```

### Available Tasks

Use `task --list` to see all available tasks, or run `task help` for common usage patterns.

#### Common Tasks

```bash
# Development
task setup          # Setup development environment
task deps           # Download and tidy dependencies
task build          # Build the binary
task test           # Run tests
task check          # Run all checks (format, vet, test)

# Benchmarking
task bench          # Run all benchmarks (3x iterations)
task bench-quick    # Run quick benchmarks (1x iteration)
task bench-sleep    # Run only Sleep10ms benchmarks
task bench-loop     # Run only Loop10 benchmarks
task bench-mixed    # Run only Sleep5Loop5 benchmarks
task bench-varmq    # Run VarMQ benchmarks only
task bench-pond     # Run PondV2 benchmarks only
task bench-gen      # Save benchmark output to bench/ directory

# Profiling
task bench-profile  # Run benchmarks with CPU/memory profiling

# Visualization
task vizb           # Generate charts from bench/bench-default.txt
task vizb-8cpu      # Generate charts from bench/bench-8cpu.txt
task vizb-4cpu      # Generate charts from bench/bench-4cpu.txt
task vizb-all       # Generate charts for all CPU configurations
task merge          # Merge charts into index.html

# Cleanup
task clean          # Clean build artifacts, profiles, and charts
```

#### Manual Commands (without Task)

```bash
# Run all benchmarks
go test -bench=. -benchtime=3x -benchmem

# Run specific task type
go test -bench=BenchmarkTasks/Sleep10ms -benchtime=3x -benchmem

# Run specific implementation
go test -bench=VarMQ -benchtime=3x -benchmem

# Generate bench txt data
go test -bench=. -benchtime=2x -benchmem > bench/bench-default.txt
go test -bench=. -benchtime=2x -benchmem -cpu=8 > bench/bench-8cpu.txt
go test -bench=. -benchtime=2x -benchmem -cpu=4 > bench/bench-4cpu.txt

# Generate charts from bench files
vizb bench/bench-default.txt -p /n/y/x -t s -m kb -o charts/bench.json -l -s asc --scale log -c bar,pie -n "VarMQ vs PondV2 (default CPUs)"

# Merge charts into a single HTML page
vizb merge charts/ -o index.html

# Analyze profiles
go tool pprof cpu.prof
go tool pprof mem.prof
```

## Benchmark Visualization

This project includes integration with [Vizb](https://github.com/goptics/vizb) for generating interactive HTML charts from benchmark results.

### Prerequisites for Visualization

Install Vizb:

```bash
go install github.com/goptics/vizb@latest
```

### Generating Charts

1. **Generate bench txt data:**

   ```bash
   task bench-gen
   ```

2. **Convert to charts:**

   ```bash
   task vizb           # From bench/bench-default.txt
   task vizb-8cpu      # From bench/bench-8cpu.txt
   task vizb-4cpu      # From bench/bench-4cpu.txt

   # Or all at once
   task vizb-all
   ```

3. **Merge into a single page:**

   ```bash
   task merge          # Outputs index.html at project root
   ```

### Chart Output

- `charts/bench.json` — Default CPU configuration
- `charts/bench-8cpu.json` — 8 CPU cores configuration
- `charts/bench-4cpu.json` — 4 CPU cores configuration
- `index.html` — Merged interactive page (at project root)

Each chart includes:

- **Interactive visualizations** comparing VarMQ and PondV2 performance
- **Workload grouping**: Results organized by user/task patterns

### Chart Configuration

The visualization uses these settings:

- **Group pattern**: `/n/y/x` — benchmark name / user count / task count
- **Time unit**: Seconds (`-t s`)
- **Memory unit**: Kilobytes (`-m kb`)
- **Scale**: Logarithmic (`--scale log`)
- **Chart types**: Bar and Pie (`-c bar,pie`)

## Project Structure

- **`types.go`** — Core type definitions and interfaces
- **`subjects.go`** — Worker pool implementations and configurations
- **`benchmark_test.go`** — Benchmark test logic
- **`bench/`** — Pre-generated benchmark output (txt files)
- **`charts/`** — Generated JSON chart data
- **`Taskfile.yml`** — Build automation and development tasks
- **`.github/workflows/bench.yml`** — CI/CD pipeline for chart deployment

## Benchmark Configuration

### Workload Patterns

- **1u-1Mt**: 1 user submitting 1 million tasks
- **100u-10Kt**: 100 users submitting 10k tasks each
- **1Ku-1Kt**: 1k users submitting 1k tasks each
- **10Ku-100t**: 10k users submitting 100 tasks each
- **1Mu-1t**: 1 million users submitting 1 task each

### Task Types

- **Sleep10ms**: I/O simulation (10ms sleep)
- **Loop10**: CPU-intensive (10 iterations of math operations)
- **Sleep5Loop5**: Mixed workload (5ms sleep + 5 iterations)

### Worker Pool Sizes

- 50,000 workers
- 100,000 workers
- 300,000 workers
- 500,000 workers

## Adding New Worker Pools

1. Add your implementation to `subjects.go`:

   ```go
   {
       name: "YourPool",
       factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
           // Your implementation
           return submitFunc, teardownFunc
       },
   },
   ```

2. Add any new dependencies to `go.mod`

3. Run benchmarks to compare performance

## Performance Analysis

### Interpreting Results

- **ns/op**: Nanoseconds per operation (lower is better)
- **B/op**: Bytes allocated per operation (lower is better)
- **allocs/op**: Number of allocations per operation (lower is better)

### Key Metrics to Watch

- **Throughput**: How many tasks processed per second
- **Memory efficiency**: Allocations and memory usage
- **Scaling behavior**: Performance across different worker counts
- **Workload sensitivity**: How performance varies with user/task patterns

## License

MIT License — see [LICENSE](LICENSE) file for details.
