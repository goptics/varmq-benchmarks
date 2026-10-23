//go:build sqliteq_v03

package sqlite

import (
	"fmt"
	"testing"

	"github.com/goptics/sqliteq"
	"github.com/goptics/varmq"
)

// v03Store is the sqliteq v0.3 way of opening queues: one New, with the
// layout and the checkpoint size passed as store options.
type v03Store struct {
	qs sqliteq.Queues
}

func openStore(tb testing.TB, path string, l layout, ckpt int) store {
	tb.Helper()

	// Folder: path is the folder, one <name>.db per queue. SingleFile: path is
	// the one database file, every queue a table in it.
	opts := []sqliteq.StoreOption{sqliteq.WithCheckpointPages(ckpt)}
	if l == singleFile {
		opts = append(opts, sqliteq.WithSingleFile())
	}

	qs, err := sqliteq.New(path, opts...)
	if err != nil {
		tb.Fatalf("sqliteq.New(%s): %v", path, err)
	}

	return &v03Store{qs: qs}
}

func (s *v03Store) queue(name string) (varmq.IPersistentQueue, error) {
	q, err := s.qs.NewQueue(name)
	if err != nil {
		return nil, fmt.Errorf("new queue %s: %w", name, err)
	}

	return q, nil
}

func (s *v03Store) priorityQueue(name string) (varmq.IPersistentPriorityQueue, error) {
	q, err := s.qs.NewPriorityQueue(name)
	if err != nil {
		return nil, fmt.Errorf("new priority queue %s: %w", name, err)
	}

	return q, nil
}

func (s *v03Store) close() error {
	return s.qs.Close()
}
