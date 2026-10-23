package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

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

// bound is one queue with its worker.
type bound struct {
	worker varmq.Worker
	add    func(i int) bool // enqueues job number i
}

func bind(tb testing.TB, st store, sc scenario, name string) bound {
	tb.Helper()

	w := varmq.NewWorker(func(varmq.Job[int]) {}, varmq.WithConcurrency(16))

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
// timed. Mixed times the first Add to Wait. Drain times Resume to Wait.
func runScenario(b *testing.B, sc scenario, l layout, ckpt int) {
	b.Helper()

	for range b.N {
		b.StopTimer()
		runOnce(b, sc, l, ckpt)
	}

	if sec := b.Elapsed().Seconds(); sec > 0 {
		b.ReportMetric(float64(sc.total*b.N)/sec, "jobs/s")
	}
}

func runOnce(b *testing.B, sc scenario, l layout, ckpt int) {
	b.Helper()

	dir := freshDir(b)

	path := dir
	if l == singleFile {
		path = filepath.Join(dir, "bench.db")
	}

	st := openStore(b, path, l, ckpt)

	bounds := make([]bound, sc.queues)
	for i := range bounds {
		bounds[i] = bind(b, st, sc, fmt.Sprintf("q%d", i))

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

	for _, bd := range bounds {
		bd.worker.Wait()
	}

	b.StopTimer()

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

	if err := st.close(); err != nil {
		b.Fatalf("close store: %v", err)
	}
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
