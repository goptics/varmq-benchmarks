package main

import (
	p2 "github.com/alitto/pond/v2"
	"github.com/goptics/varmq"
)

// Configuration
var maxWorkers = []int{50_000, 1_00_000, 3_00_000}

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
		name: "PondV2",
		factory: func(maxWorkers int) (poolSubmit, poolTeardown) {
			pool := p2.NewPool(maxWorkers)
			return func(t func()) {
				pool.Submit(t)
			}, pool.StopAndWait
		},
	},
}
