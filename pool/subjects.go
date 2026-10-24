package main

import (
	"sync"

	"github.com/alitto/pond/v2"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gammazero/workerpool"
	"github.com/goptics/varmq"
	"github.com/panjf2000/ants/v2"
	"github.com/sourcegraph/conc/pool"
	"golang.org/x/sync/errgroup"
)

// Configuration

var workloads = []workload{
	{
		name:      "1u-1Mt", // 1 user 1 million tasks each
		userCount: 1,
		taskCount: 10_00_000,
	},
	{
		name:      "100u-10Kt", // 100 user 10 thousands tasks each
		userCount: 100,
		taskCount: 10000,
	},
	{
		name:      "1Ku-1Kt", // 1 thousand users 1 thousand tasks
		userCount: 1000,
		taskCount: 1000,
	},
	{
		name:      "10Ku-100t", // 10 thousands users 100 tasks each
		userCount: 10000,
		taskCount: 100,
	},
	{
		name:      "1Mu-1t", // 1 million users 1 tasks each
		userCount: 10_00_000,
		taskCount: 1,
	},
}

// Predefined task configurations
var taskConfigs = []TaskConfig{
	{Name: "Sleep10ms", SleepMs: 10, LoopSize: 0},
	{Name: "Loop10", SleepMs: 0, LoopSize: 10},
	{Name: "Sleep5msLoop5", SleepMs: 5, LoopSize: 5},
}

// serialTaskConfigs run on a pool of one worker. With one worker the pools
// differ only in what each costs per task (submit, hand-off, wake), so only
// CPU tasks run here: a sleeping task would measure time.Sleep, and one
// worker sleeping 10 ms per task needs almost three hours for 1M tasks.
var serialTaskConfigs = []TaskConfig{
	{Name: "Loop10", LoopSize: 10},      // a few ns of work: what remains is the pool's own cost
	{Name: "Loop10K", LoopSize: 10_000}, // about 1 µs of work: how much the pool adds to a small real task
}

// poolSizes are the pool sizes the matrix runs, each with the tasks worth
// running at that size.
var poolSizes = []poolSize{
	{workers: 50_000, tasks: taskConfigs},
	{workers: 100_000, tasks: taskConfigs},
	{workers: 300_000, tasks: taskConfigs},
	{workers: 1, tasks: serialTaskConfigs},
}

// subjects contains all the worker pool implementations to benchmark
var subjects = []subject{
	{
		name: "VarMQ",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			w := varmq.NewWorker(varmq.Func(), maxWorkers)
			q := w.BindQueue()

			return func(task func()) {
					q.Add(task)
				}, func() {
					w.WaitAndStop()
				}
		},
	},
	{
		name: "Ants",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			// Default options: Submit blocks while all maxWorkers goroutines are busy.
			p, err := ants.NewPool(maxWorkers)
			if err != nil {
				panic(err)
			}

			return func(t func()) {
				_ = p.Submit(t)
			}, p.Release
		},
	},
	{
		name: "WorkerPool",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			p := workerpool.New(maxWorkers)
			return p.Submit, p.StopWait
		},
	},
	{
		name: "Conc",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			p := pool.New().WithMaxGoroutines(maxWorkers)
			return p.Go, p.Wait
		},
	},
	{
		name: "ErrGroup",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			// One goroutine per task behind a semaphore, not reused workers.
			var g errgroup.Group
			g.SetLimit(maxWorkers)
			return func(t func()) {
					g.Go(func() error { t(); return nil })
				}, func() {
					_ = g.Wait()
				}
		},
	},
	{
		name: "Gopool",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			// Idle workers exit on their own and there is no Close, so teardown has nothing to do.
			// Submit never blocks (unbounded task list), and the cap is soft: it can overshoot under contention.
			p := gopool.NewPool("bench", int32(maxWorkers), gopool.NewConfig())
			return p.Go, func() {}
		},
	},
	{
		name: "GoroutinePool",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			return chanPool(maxWorkers, 0)
		},
	},
	{
		name: "BufferedPool",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			return chanPool(maxWorkers, 1_000_000)
		},
	},
	{
		name: "Pond",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			p := pond.NewPool(maxWorkers)
			return func(t func()) {
				p.Submit(t)
			}, p.StopAndWait
		},
	},
}

// chanPool: N goroutines draining one task channel; buf=0 unbuffered, else buffered.
func chanPool(maxWorkers, buf int) (poolSubmit, poolTeardown) {
	var wg sync.WaitGroup
	taskChan := make(chan func(), buf)
	for range maxWorkers {
		wg.Go(func() {
			for task := range taskChan {
				task()
			}
		})
	}
	return func(task func()) { taskChan <- task }, func() {
		close(taskChan)
		wg.Wait()
	}
}
