package durable

import (
	"slices"
	"testing"
)

// BenchmarkDurable is the full matrix: enqueue and drain a workload through
// each subject, for every task, worker count and instance count.
func BenchmarkDurable(b *testing.B) {
	url := redisOrSkip(b)

	for _, t := range tasks {
		for _, sh := range shapes {
			b.Run(t.name+sh.label(), func(b *testing.B) {
				for _, w := range workloads {
					b.Run(w.name, func(b *testing.B) {
						for _, s := range subjects {
							if sh.instances > s.maxInstances {
								continue
							}

							b.Run(s.name, func(b *testing.B) {
								for range b.N {
									b.StopTimer()
									flush(b, url)
									b.StartTimer()

									runEndToEnd(b, s, url, w, sh, t)
								}

								reportJobsPerSec(b, w)
							})
						}
					})
				}
			})
		}
	}
}

// BenchmarkEnqueue measures producing only; nothing consumes the jobs.
func BenchmarkEnqueue(b *testing.B) {
	url := redisOrSkip(b)

	for _, w := range workloads {
		b.Run(w.name, func(b *testing.B) {
			for _, s := range subjects {
				b.Run(s.name, func(b *testing.B) {
					for range b.N {
						b.StopTimer()
						flush(b, url)
						b.StartTimer()

						runEnqueue(b, s, url, w)
					}

					reportJobsPerSec(b, w)
				})
			}
		})
	}
}

// BenchmarkLatency reports enqueue-to-handled latency, one job in flight.
func BenchmarkLatency(b *testing.B) {
	url := redisOrSkip(b)

	b.Run("Noop"+latencyShape.label(), func(b *testing.B) {
		b.Run(latencyWorkload.name, func(b *testing.B) {
			for _, s := range subjects {
				b.Run(s.name, func(b *testing.B) {
					var samples []int64

					for range b.N {
						b.StopTimer()
						flush(b, url)
						b.StartTimer()

						samples = append(samples, runLatency(b, s, url, latencyWorkload.total())...)
					}

					slices.Sort(samples)

					const msPerNs = 1e-6
					b.ReportMetric(float64(percentile(samples, 0.50))*msPerNs, "p50-ms")
					b.ReportMetric(float64(percentile(samples, 0.99))*msPerNs, "p99-ms")
					b.ReportMetric(float64(percentile(samples, 1))*msPerNs, "max-ms")
				})
			}
		})
	})
}

func reportJobsPerSec(b *testing.B, w workload) {
	if sec := b.Elapsed().Seconds(); sec > 0 {
		b.ReportMetric(float64(w.total()*b.N)/sec, "jobs/s")
	}
}
