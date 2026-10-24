module github.com/goptics/varmq-benchmarks/pool

go 1.26.0

require (
	github.com/alitto/pond/v2 v2.7.2
	github.com/bytedance/gopkg v0.1.4
	github.com/gammazero/workerpool v1.2.2
	github.com/goptics/varmq v1.9.0
	github.com/panjf2000/ants/v2 v2.12.1
	github.com/sourcegraph/conc v0.3.0
	golang.org/x/sync v0.24.0
)

require (
	github.com/gammazero/deque v1.2.2 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.9.0 // indirect
)

// replace github.com/goptics/varmq => ../varmq

replace github.com/goptics/varmq => ../../varmq
