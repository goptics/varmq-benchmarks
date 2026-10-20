package durable

import (
	"context"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// gocraftGoroutines counts goroutines still inside gocraft/work.
func gocraftGoroutines() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]

	n := 0

	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "gocraft/work.") {
			n++
		}
	}

	return n
}

// TestGocraftStopLeavesNothingRunning pins that Stop plus pool.Close leave no
// gocraft/work goroutine (worker, observer, heartbeater, requeuers, dead pool
// reaper, periodic enqueuer) and nothing that writes to Redis afterwards: a
// key appearing after FLUSHDB would leak into the next round. It also pins
// that nothing reaches os.Stdout, where the library's logError prints.
func TestGocraftStopLeavesNothingRunning(t *testing.T) {
	url := redisOrSkip(t)
	flush(t, url)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	captured := make(chan string, 1)

	go func() {
		b, _ := io.ReadAll(r)
		captured <- string(b)
	}()

	var seen atomic.Int64

	_, stop := gocraftConsumer(url, 5, func(payload) { seen.Add(1) })

	enqueue, closeProducer := gocraftProducer(url)
	for i := range 20 {
		if err := enqueue(newPayload(int64(i))); err != nil {
			t.Fatal(err)
		}
	}

	for deadline := time.Now().Add(10 * time.Second); seen.Load() < 20 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}

	n := seen.Load()

	closeProducer()
	stop()

	os.Stdout = stdout
	_ = w.Close()

	if out := <-captured; out != "" {
		t.Errorf("gocraft/work wrote to stdout: %q", out)
	}

	if n != 20 {
		t.Fatalf("consumed %d of 20 jobs", n)
	}

	deadline := time.Now().Add(time.Second)
	for gocraftGoroutines() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if g := gocraftGoroutines(); g != 0 {
		t.Fatalf("%d gocraft/work goroutines still running after stop", g)
	}

	flush(t, url)

	// Longer than the 5 s heartbeat and the 1 s observer and requeuer ticks.
	time.Sleep(6 * time.Second)

	c := rawClient(url, 1)
	defer c.Close()

	if n, err := c.DBSize(context.Background()).Result(); err != nil || n != 0 {
		keys, _ := c.Keys(context.Background(), "*").Result()
		t.Fatalf("DBSIZE = %d (err %v) after stop and flush, keys %v", n, err, keys)
	}
}
