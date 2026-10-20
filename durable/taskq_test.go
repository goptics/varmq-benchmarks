package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestTaskqStopLeavesNothingWriting pins that no taskq goroutine (fetcher,
// worker, or one of the queue's three scheduler loops, which Close does not
// wait for) writes to Redis after stop returns: a key appearing after FLUSHDB
// would leak into the next round.
func TestTaskqStopLeavesNothingWriting(t *testing.T) {
	url := redisOrSkip(t)
	flush(t, url)

	var seen atomic.Int64

	_, stop := taskqConsumer(url, 5, func(payload) { seen.Add(1) })

	enqueue, closeProducer := taskqProducer(url)
	for i := range 20 {
		if err := enqueue(newPayload(int64(i))); err != nil {
			t.Fatal(err)
		}
	}

	for deadline := time.Now().Add(10 * time.Second); seen.Load() < 20 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}

	if n := seen.Load(); n != 20 {
		t.Fatalf("consumed %d of 20 jobs", n)
	}

	closeProducer()
	stop()
	flush(t, url)

	// The scheduler loops sleep 1 to 1.5 s between passes.
	time.Sleep(3 * time.Second)

	c := rawClient(url, 1)
	defer c.Close()

	if n, err := c.DBSize(context.Background()).Result(); err != nil || n != 0 {
		keys, _ := c.Keys(context.Background(), "*").Result()
		t.Fatalf("DBSIZE = %d (err %v) after stop and flush, keys %v", n, err, keys)
	}
}
