package durable

import (
	"sync/atomic"
	"time"

	"github.com/adjust/rmq/v5"
)

const (
	rmqQueue = "bench"
	// rmqPollDuration is the sleep after every consume pass (queue.go, consume).
	// rmq has no default. 10ms is this benchmark's choice; rmq's README example
	// uses 1s and example/consumer/main.go uses 100ms.
	rmqPollDuration = 10 * time.Millisecond
)

// rmqAckErrors counts failed acks. An ack can fail with rmq.ErrorNotFound
// when the unacked entry is already gone, e.g. FLUSHDB ran under a late
// consumer. Nothing is logged and nothing panics.
var rmqAckErrors atomic.Int64

// rmqOpen opens an rmq connection on its own go-redis client. The tag need not
// be unique: openConnection appends a random suffix (rmq/connection.go). rmq
// never stops a connection's heartbeat goroutine (stopHeartbeat is
// unexported), so after the client is closed it fails once a second for
// rmq.HeartbeatErrorLimit ticks and then sends one blocking error to errChan.
// The 1024-entry buffer lets that last send complete with no reader, so the
// goroutine exits instead of leaking. stopDrain ends the reader and returns
// only after it has exited.
func rmqOpen(url string, pool int) (rmq.Connection, func(), func()) {
	client := rawClient(url, pool)
	errChan := make(chan error, 1024)
	quit := make(chan struct{})
	exited := make(chan struct{})

	go func() {
		defer close(exited)

		for {
			select {
			case <-errChan:
			case <-quit:
				return
			}
		}
	}()

	conn, err := rmq.OpenConnectionWithRedisClient("bench", client, errChan)
	if err != nil {
		panic(err) // the URL was already used by redisOrSkip
	}

	stopDrain := func() {
		close(quit)
		<-exited
	}

	return conn, stopDrain, func() { _ = client.Close() }
}

// rmqProducer publishes the marshaled payload as a string, with one LPUSH.
func rmqProducer(url string) (enqueueFunc, func()) {
	conn, stopDrain, closeClient := rmqOpen(url, 0)

	q, err := conn.OpenQueue(rmqQueue)
	if err != nil {
		panic(err)
	}

	return func(p payload) error {
			return q.Publish(string(p.marshal()))
		}, func() {
			<-conn.StopAllConsuming()
			stopDrain()
			closeClient()
		}
}

// rmqConsumer starts one rmq queue consumer with workers prefetch and workers
// consumer goroutines. The ack comes after the handler.
func rmqConsumer(url string, workers int, h handler) (enqueueFunc, func()) {
	conn, stopDrain, closeClient := rmqOpen(url, poolSize(workers))

	q, err := conn.OpenQueue(rmqQueue)
	if err != nil {
		panic(err)
	}

	if err := q.StartConsuming(int64(workers), rmqPollDuration); err != nil {
		panic(err)
	}

	for range workers {
		_, err := q.AddConsumerFunc("bench", func(d rmq.Delivery) {
			if p, err := unmarshalPayload([]byte(d.Payload())); err == nil {
				h(p)
			}

			if err := d.Ack(); err != nil {
				rmqAckErrors.Add(1)
			}
		})
		if err != nil {
			panic(err)
		}
	}

	return nil, func() {
		// Waits for every consumer goroutine to return from its current Consume.
		// The queue's fetch goroutine is not waited on (queue.go, consume): it
		// exits at its next check, up to one poll sleep later. Closing the
		// client first makes anything it still sends fail.
		<-conn.StopAllConsuming()
		closeClient()
		stopDrain()
	}
}
