package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq"
	_ "github.com/mattn/go-sqlite3"
)

// scenario is one row of the benchmark matrix.
type scenario struct {
	name      string
	queues    int
	producers int  // per queue
	total     int  // jobs over all queues
	drain     bool // pause the worker, add everything, then time the drain
	priority  bool
	layouts   []layout
}

func (sc scenario) perProducer() int { return sc.total / sc.queues / sc.producers }

var scenarios = []scenario{
	{name: "Mixed1Q", queues: 1, producers: 4, total: 20000, layouts: []layout{singleFile}},
	{name: "Drain1Q", queues: 1, producers: 4, total: 20000, drain: true, layouts: []layout{singleFile}},
	{name: "DrainPrio", queues: 1, producers: 4, total: 10000, drain: true, priority: true, layouts: []layout{singleFile}},
	{name: "Mixed10Q", queues: 10, producers: 1, total: 20000, layouts: []layout{folder, singleFile}},
}

// benchRoot is where the benchmark databases live. They must be on a real disk:
// on a tmpfs /tmp the fsync cost this benchmark measures would vanish.
func benchRoot(tb testing.TB) string {
	tb.Helper()

	if d := os.Getenv("SQLITEQ_BENCH_DIR"); d != "" {
		return d
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		tb.Fatalf("no SQLITEQ_BENCH_DIR and no user cache dir: %v", err)
	}

	return filepath.Join(cache, "varmq-bench-sqlite")
}

// freshDir makes an empty directory under benchRoot and removes it on cleanup.
func freshDir(tb testing.TB) string {
	tb.Helper()

	root := benchRoot(tb)
	if err := os.MkdirAll(root, 0o755); err != nil {
		tb.Fatalf("mkdir %s: %v", root, err)
	}

	dir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		tb.Fatalf("mkdir temp: %v", err)
	}

	tb.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}

// waitDeadline bounds how long one iteration waits for its workers to drain,
// so a stuck job fails its row instead of hanging the whole run.
const waitDeadline = 5 * time.Minute

// bound is one queue with its worker.
type bound struct {
	worker varmq.Worker
	add    func(i int) bool // enqueues job number i
}

func bind(tb testing.TB, st store, sc scenario, name string, ran *atomic.Int64) bound {
	tb.Helper()

	w := varmq.NewWorker(func(varmq.Job[int]) { ran.Add(1) }, varmq.WithConcurrency(16))

	if sc.priority {
		q, err := st.priorityQueue(name)
		if err != nil {
			tb.Fatal(err)
		}

		pq := w.WithPersistentPriorityQueue(q)

		return bound{worker: pq.Worker(), add: func(i int) bool { return pq.Add(i, i%4) }}
	}

	q, err := st.queue(name)
	if err != nil {
		tb.Fatal(err)
	}

	pq := w.WithPersistentQueue(q)

	return bound{worker: pq.Worker(), add: func(i int) bool { return pq.Add(i) }}
}

// runScenario runs sc b.N times, each on a fresh database, and reports jobs/s
// over the timed part only: setup, teardown and the leftover check are not
// timed. Mixed times the first Add to Wait. Drain times Resume to Wait. It
// also reports walMB, the largest size any one -wal file reached in any
// iteration, untimed parts included. procs is the GOMAXPROCS the benchmark
// function itself ran at, so every row shows what it was measured with.
func runScenario(b *testing.B, sc scenario, l layout, ckpt int) {
	b.Helper()

	var walPeak int64

	for range b.N {
		b.StopTimer()
		walPeak = max(walPeak, runOnce(b, sc, l, ckpt))
	}

	if sec := b.Elapsed().Seconds(); sec > 0 {
		b.ReportMetric(float64(sc.total*b.N)/sec, "jobs/s")
	}

	b.ReportMetric(float64(walPeak)/1e6, "walMB")
	b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "procs")
}

// walSampleEvery is how often sampleWAL reads the -wal file sizes.
const walSampleEvery = 50 * time.Millisecond

// sampleWAL reads the size of every -wal file in dir every walSampleEvery, on
// its own goroutine, until the returned stop is called. stop takes one last
// sample and returns the largest size any one file reached; it may be called
// more than once.
func sampleWAL(dir string) (stop func() int64) {
	var peak int64 // owned by the goroutine until done closes

	sample := func() {
		files, _ := filepath.Glob(filepath.Join(dir, "*-wal"))
		for _, f := range files {
			if fi, err := os.Stat(f); err == nil {
				peak = max(peak, fi.Size())
			}
		}
	}

	quit, done := make(chan struct{}), make(chan struct{})

	go func() {
		defer close(done)

		t := time.NewTicker(walSampleEvery)
		defer t.Stop()

		for {
			select {
			case <-t.C:
				sample()
			case <-quit:
				sample()

				return
			}
		}
	}()

	var once sync.Once

	return func() int64 {
		once.Do(func() { close(quit) })
		<-done

		return peak
	}
}

// runOnce runs one iteration and returns its WAL peak in bytes.
func runOnce(b *testing.B, sc scenario, l layout, ckpt int) int64 {
	b.Helper()

	dir := freshDir(b)

	path := dir
	if l == singleFile {
		path = filepath.Join(dir, "bench.db")
	}

	st := openStore(b, path, l, ckpt)

	stopWAL := sampleWAL(dir)
	defer stopWAL() // a failed iteration still stops the sampler

	var ran atomic.Int64 // handler runs over all queues

	bounds := make([]bound, sc.queues)
	for i := range bounds {
		bounds[i] = bind(b, st, sc, fmt.Sprintf("q%d", i), &ran)

		if sc.drain {
			if err := bounds[i].worker.Pause(); err != nil {
				b.Fatalf("pause: %v", err)
			}
		}
	}

	var (
		wg     sync.WaitGroup
		failed atomic.Int64
	)

	produce := func() {
		for _, bd := range bounds {
			for p := range sc.producers {
				wg.Go(func() {
					for j := range sc.perProducer() {
						if !bd.add(p*sc.perProducer() + j) {
							failed.Add(1)
						}
					}
				})
			}
		}

		wg.Wait()
	}

	if sc.drain {
		produce()
		b.StartTimer()

		for _, bd := range bounds {
			if err := bd.worker.Resume(); err != nil {
				b.Fatalf("resume: %v", err)
			}
		}
	} else {
		b.StartTimer()
		produce()
	}

	waited := make(chan struct{})

	go func() {
		defer close(waited)

		for _, bd := range bounds {
			bd.worker.Wait()
		}
	}()

	select {
	case <-waited:
	case <-time.After(waitDeadline):
		b.StopTimer()
		b.Fatalf("%s: workers still busy %s after the last Add: a job is stuck", sc.name, waitDeadline)
	}

	b.StopTimer()

	if n := ran.Load(); n != int64(sc.total) {
		b.Fatalf("%s: handler ran %d times, want %d: a job was lost or run twice", sc.name, n, sc.total)
	}

	if n := failed.Load(); n > 0 {
		b.Fatalf("%d Add calls returned false", n)
	}

	if left, err := rowsLeft(path, l, sc.queues); err != nil {
		b.Fatalf("count leftover rows: %v", err)
	} else if left > 0 {
		b.Fatalf("%d rows left in the queue tables after Wait", left)
	}

	for _, bd := range bounds {
		if err := bd.worker.WaitAndStop(); err != nil {
			b.Fatalf("stop: %v", err)
		}
	}

	// Closing checkpoints and removes the WAL, so the sampling stops first
	walPeak := stopWAL()

	if err := st.close(); err != nil {
		b.Fatalf("close store: %v", err)
	}

	return walPeak
}

// rowsLeft counts the rows in every table of the store's database files, read
// with database/sql directly. It fails when it finds fewer than queues tables,
// so a check that looked at nothing cannot pass.
func rowsLeft(path string, l layout, queues int) (rows int, err error) {
	files := []string{path}

	if l == folder {
		if files, err = filepath.Glob(filepath.Join(path, "*.db")); err != nil {
			return 0, err
		}
	}

	tables := 0

	for _, f := range files {
		db, err := sql.Open("sqlite3", f)
		if err != nil {
			return 0, err
		}

		names, err := tableNames(db)
		if err != nil {
			db.Close()

			return 0, err
		}

		for _, t := range names {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM "` + t + `"`).Scan(&n); err != nil {
				db.Close()

				return 0, err
			}

			rows += n
			tables++
		}

		db.Close()
	}

	if tables < queues {
		return 0, fmt.Errorf("found %d queue tables, want at least %d", tables, queues)
	}

	return rows, nil
}

func tableNames(db *sql.DB) ([]string, error) {
	r, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var names []string

	for r.Next() {
		var n string
		if err := r.Scan(&n); err != nil {
			return nil, err
		}

		names = append(names, n)
	}

	return names, r.Err()
}
