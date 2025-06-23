package main

import "time"

// Core benchmark types and interfaces
type poolSubmit func(func())
type poolTeardown func()
type poolFactory func(maxWorkers int) (poolSubmit, poolTeardown)

type subject struct {
	name    string
	factory poolFactory
}

type workload struct {
	name      string
	userCount int
	taskCount int
}

// TaskConfig defines a benchmark task with configurable sleep and loop parameters
type TaskConfig struct {
	Name     string // Name of the task configuration
	SleepMs  int    // Sleep duration in milliseconds
	LoopSize int    // Number of iterations for the loop
}

// GenerateTask creates a task function based on the configuration
func (tc TaskConfig) GenerateTask() func() {
	return func() {
		// Execute the loop if configured
		if tc.LoopSize > 0 {
			for i := range tc.LoopSize {
				_ = i * i
			}
		}

		// Sleep if configured
		if tc.SleepMs > 0 {
			time.Sleep(time.Duration(tc.SleepMs) * time.Millisecond)
		}
	}
}
