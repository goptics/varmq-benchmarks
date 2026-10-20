package durable

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// rmqPollers counts goroutines still running rmq's fetch loop or a consumer.
func rmqPollers() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]

	n := 0

	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "rmq/v5.(*redisQueue).consume") || strings.Contains(g, "rmq/v5.(*redisQueue).consumerConsume") {
			n++
		}
	}

	return n
}

func TestRmqStopEndsPollers(t *testing.T) {
	url := redisOrSkip(t)

	_, stop := rmqConsumer(url, 5, func(payload) {})
	if rmqPollers() == 0 {
		t.Fatal("no rmq goroutines running before stop")
	}

	stop()

	// the fetch loop is not waited on by rmq, so it may take up to one poll sleep
	deadline := time.Now().Add(2 * time.Second)
	for rmqPollers() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if n := rmqPollers(); n != 0 {
		t.Fatalf("%d rmq poller goroutines still running after stop", n)
	}
}
