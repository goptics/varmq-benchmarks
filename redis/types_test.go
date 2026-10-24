package durable

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestShapeLabel(t *testing.T) {
	if got := (shape{workers: 100, instances: 4}).label(); got != "W100C4" {
		t.Fatal(got)
	}
}

func TestPercentile(t *testing.T) {
	s := make([]int64, 100)
	for i := range s {
		s[i] = int64(i + 1)
	}

	for _, c := range []struct {
		q    float64
		want int64
	}{{0.5, 50}, {0.99, 99}, {1, 100}} {
		if got := percentile(s, c.q); got != c.want {
			t.Fatalf("q=%v: got %d, want %d", c.q, got, c.want)
		}
	}

	for _, q := range []float64{0.5, 0.99, 1} {
		if got := percentile([]int64{7}, q); got != 7 {
			t.Fatalf("single element q=%v: got %d, want 7", q, got)
		}
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	p := newPayload(42)
	p.Pad[0], p.Pad[47] = 1, 2

	got, err := unmarshalPayload(p.marshal())
	if err != nil {
		t.Fatal(err)
	}

	if got != p {
		t.Fatalf("got %+v, want %+v", got, p)
	}
}

func TestWithPoolSize(t *testing.T) {
	if got := withPoolSize("redis://localhost:6390/0", 100); got != "redis://localhost:6390/0?pool_size=100" {
		t.Fatal(got)
	}

	u, err := url.Parse(withPoolSize("redis://localhost:6390/0?protocol=3", 5))
	if err != nil {
		t.Fatal(err)
	}

	if q := u.Query(); q.Get("protocol") != "3" || q.Get("pool_size") != "5" {
		t.Fatal(u.RawQuery)
	}

	u, err = url.Parse(withPoolSize("redis://localhost:6390/0?pool_size=1", 5))
	if err != nil {
		t.Fatal(err)
	}

	if vals := u.Query()["pool_size"]; len(vals) != 1 || vals[0] != "5" {
		t.Fatal(u.RawQuery)
	}
}

func TestCollectorDuplicates(t *testing.T) {
	c := newCollector(3, task{name: "Noop"})
	for _, id := range []int64{0, 1, 1, 2} {
		c.handle(payload{ID: id})
	}

	if err := c.wait(time.Second); err != nil {
		t.Fatal(err)
	}

	if got := c.dups(); got != 1 {
		t.Fatalf("dups = %d, want 1", got)
	}
}

func TestCollectorTimeout(t *testing.T) {
	c := newCollector(3, task{name: "Noop"})
	c.handle(payload{ID: 0})

	err := c.wait(50 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "1/3") {
		t.Fatalf("err = %v, want one containing 1/3", err)
	}
}
