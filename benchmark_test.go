package main

import (
	"fmt"
	"sync"
	"testing"
)

func BenchmarkTasks(b *testing.B) {
	for _, size := range poolSizes {
		for _, taskConfig := range size.tasks {
			b.Run(fmt.Sprintf("%sW%s", taskConfig.Name, size.label()), func(b *testing.B) {
				runBenchmarks(b, workloads, taskConfig.GenerateTask(), size.workers)
			})
		}
	}
}

func runBenchmarks(b *testing.B, workloads []workload, task func(), maxWorkers int) {
	for _, workload := range workloads {
		for _, subject := range subjects {
			testName := fmt.Sprintf("%s/%s", workload.name, subject.name)

			b.Run(testName, func(b *testing.B) {
				for b.Loop() {
					// Create pool
					poolSubmit, poolTeardown := subject.factory(maxWorkers)

					// Spawn one goroutine per simulated user
					var wg sync.WaitGroup
					wg.Add(workload.userCount * workload.taskCount)

					testFunc := func() {
						task()
						wg.Done()
					}

					for range workload.userCount {
						go func() {
							for range workload.taskCount {
								poolSubmit(testFunc)
							}
						}()
					}
					wg.Wait()

					// Tear down
					poolTeardown()
				}

				totalOps := int64(workload.userCount) * int64(workload.taskCount) * int64(b.N)
				elapsedSec := b.Elapsed().Seconds()
				if elapsedSec > 0 {
					b.ReportMetric(float64(totalOps)/elapsedSec, "ops/s")
				}
			})
		}
	}
}
