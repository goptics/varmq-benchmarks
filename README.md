# VarMQ Benchmarks - Worker Pool Performance Testing

A comprehensive benchmark suite comparing Go worker pool implementations, focusing on VarMQ and PondV2 performance across different workload patterns.

## Why PondV2 was chosen for comparison

PondV2 was selected as the comparison benchmark because it provides similar worker pool features to VarMQ, making it an ideal candidate for performance analysis. Interestingly, VarMQ was inspired by some design patterns from PondV2, creating a meaningful comparison between these two Go worker pool implementations.

## Overview

This project benchmarks worker pool libraries to understand their performance characteristics under various workload scenarios:

- Different user/task ratios (1 user with 1M tasks vs 1M users with 1 task each)
- Various task types (CPU-intensive loops, I/O simulation with sleep, mixed workloads)
- Multiple worker pool sizes (50k, 100k, 300k, 500k workers)

## Benchmarked Libraries

- **[VarMQ](https://github.com/goptics/varmq)** - High-performance Message queue and worker pool with job queuing
- **[PondV2](https://github.com/alitto/pond)** - Minimalistic and High-performance goroutine worker pool written in Go

## Development Setup

### Prerequisites

- **Go 1.24+** - [Install Go](https://golang.org/doc/install)
- **Task** (optional but recommended) - [Install Task](https://taskfile.dev/installation/)
- **Vizb** (optional for visualization) - [Install Vizb](https://github.com/goptics/vizb)

### Quick Start

1. **Clone the repository:**

   ```bash
   git clone <repository-url>
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

# Profiling
task bench-profile  # Run benchmarks with CPU/memory profiling

# Visualization
task vizb           # Generate benchmark charts with default CPU count
task vizb-8cpu      # Generate benchmark charts with 8 CPU cores
task vizb-4cpu      # Generate benchmark charts with 4 CPU cores
task vizb-all       # Generate benchmark charts for all CPU configurations

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

# Analyze profiles
go tool pprof cpu.prof
go tool pprof mem.prof

# Generate interactive charts
go test -bench=. -benchmem -json | vizb -t s -m kb -a k -o charts/bench.html
```

## Benchmark Visualization

This project includes integration with [Vizb](https://github.com/goptics/vizb) for generating interactive HTML charts from benchmark results.

### Prerequisites for Visualization

Install Vizb:

```bash
go install github.com/goptics/vizb@latest
```

### Generating Charts

```bash
# Generate charts with default CPU configuration
task vizb

# Generate charts with specific CPU limits
task vizb-8cpu      # Limited to 8 CPU cores
task vizb-4cpu      # Limited to 4 CPU cores

# Generate all chart variations
task vizb-all
```

### Chart Output

Charts are saved in the `charts/` directory:

- `bench.html` - Default CPU configuration
- `bench-8cpu.html` - 8 CPU cores configuration
- `bench-4cpu.html` - 4 CPU cores configuration

Each chart includes:

- **Interactive visualizations** comparing VarMQ and PondV2 performance
- **Multiple metrics**: execution time (seconds), memory usage (KB), allocations (K)
- **Workload grouping**: Results organized by user/task patterns
- **Export capability**: Save charts as PNG images

### Chart Configuration

The visualization uses these settings:

- **Time unit**: Seconds (`-t s`)
- **Memory unit**: Kilobytes (`-m kb`)
- **Allocation unit**: Thousands (`-a k`)

## Project Structure

- **`types.go`** - Core type definitions and interfaces
- **`subjects.go`** - Worker pool implementations and configurations
- **`benchmark_test.go`** - Benchmark test logic
- **`Taskfile.yml`** - Build automation and development tasks

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

MIT License - see [LICENSE](LICENSE) file for details.
