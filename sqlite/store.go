// Package sqlite benchmarks sqliteq, the SQLite storage adapter, end to end
// through a varmq worker.
package sqlite

import (
	"testing"

	"github.com/goptics/sqliteq"
)

// layout is how queues map onto database files.
type layout int

const (
	// folder is one database file per queue, all in the directory path.
	folder layout = iota
	// singleFile is every queue in the one database file path.
	singleFile
)

func (l layout) String() string {
	if l == folder {
		return "Folder"
	}

	return "SingleFile"
}

// openStore opens one sqliteq database set at path with the given WAL
// checkpoint page count.
func openStore(tb testing.TB, path string, l layout, ckpt int) sqliteq.Queues {
	tb.Helper()

	opts := []sqliteq.StoreOption{sqliteq.WithCheckpointPages(ckpt)}
	if l == singleFile {
		opts = append(opts, sqliteq.WithSingleFile())
	}

	qs, err := sqliteq.New(path, opts...)
	if err != nil {
		tb.Fatalf("sqliteq.New(%s): %v", path, err)
	}

	return qs
}
