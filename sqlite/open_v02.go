//go:build !sqliteq_v03

package sqlite

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/goptics/sqliteq"
	"github.com/goptics/varmq"
)

// v02Store is the sqliteq v0.2 way of opening queues. Folder puts every queue
// in its own database file, <path>/<name>.db, because v0.2 has no other way to
// get a file per queue. SingleFile puts them all in one database file at path.
type v02Store struct {
	tb   testing.TB
	l    layout
	path string
	dbs  map[string]sqliteq.Queues
}

func openStore(tb testing.TB, path string, l layout, ckpt int) store {
	tb.Helper()

	if ckpt != defaultCkpt {
		tb.Skip("v0.2.3 cannot set checkpoint pages")
	}

	return &v02Store{tb: tb, l: l, path: path, dbs: map[string]sqliteq.Queues{}}
}

// open returns the database that holds queue name, opening it on first use.
func (s *v02Store) open(name string) sqliteq.Queues {
	s.tb.Helper()

	file := s.path
	if s.l == folder {
		file = filepath.Join(s.path, name+".db")
	}

	if db, ok := s.dbs[file]; ok {
		return db
	}

	// sqliteq.New panics when it cannot open the database.
	defer func() {
		if r := recover(); r != nil {
			s.tb.Fatalf("sqliteq.New(%s): %v", file, r)
		}
	}()

	s.dbs[file] = sqliteq.New(file)

	return s.dbs[file]
}

func (s *v02Store) queue(name string) (varmq.IPersistentQueue, error) {
	q, err := s.open(name).NewQueue(name)
	if err != nil {
		return nil, fmt.Errorf("new queue %s: %w", name, err)
	}

	return q, nil
}

func (s *v02Store) priorityQueue(name string) (varmq.IPersistentPriorityQueue, error) {
	q, err := s.open(name).NewPriorityQueue(name)
	if err != nil {
		return nil, fmt.Errorf("new priority queue %s: %w", name, err)
	}

	return q, nil
}

func (s *v02Store) close() error {
	var first error

	for _, db := range s.dbs {
		if err := db.Close(); err != nil && first == nil {
			first = err
		}
	}

	return first
}
