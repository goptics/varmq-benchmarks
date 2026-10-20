package durable

import "time"

var workloads = []workload{
	{"1u-100Kt", 1, 100_000},
	{"100u-1Kt", 100, 1_000},
}

// shapes is every workers x instances combination of the matrix.
var shapes = func() []shape {
	var out []shape
	for _, w := range []int{10, 100, 1000} {
		for _, c := range []int{1, 4} {
			out = append(out, shape{w, c})
		}
	}

	return out
}()

var tasks = []task{
	{name: "Noop"},
	{name: "Sleep1ms", sleep: time.Millisecond},
}

var (
	latencyWorkload = workload{"1u-1Kt", 1, 1_000}
	latencyShape    = shape{10, 1}
)

// subjects is every queue under test; each adapter file appends its own.
var subjects = []durableSubject{
	{name: "RawRedis", maxInstances: 4, producer: rawProducer, consumer: rawConsumer},
	{name: "VarMQ", maxInstances: 1, producer: varmqProducer, consumer: varmqConsumer},
	{name: "Asynq", maxInstances: 4, producer: asynqProducer, consumer: asynqConsumer},
	{name: "VarMQDistributed", maxInstances: 4, producer: varmqDistributedProducer, consumer: varmqDistributedConsumer},
	{name: "Rmq", maxInstances: 4, producer: rmqProducer, consumer: rmqConsumer},
	{name: "Taskq", maxInstances: 4, producer: taskqProducer, consumer: taskqConsumer},
}
