// Package sqlite benchmarks sqliteq, the SQLite storage adapter, end to end
// through a varmq worker. Everything that depends on the sqliteq version lives
// behind openStore, so the same benchmarks run against any release.
package sqlite

import "github.com/goptics/varmq"

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

// defaultCkpt is the checkpoint page count a store uses when it is not asked
// for another one (SQLite's own wal_autocheckpoint default).
const defaultCkpt = 1000

// store opens the persistent queues of one sqliteq database set.
type store interface {
	queue(name string) (varmq.IPersistentQueue, error)
	priorityQueue(name string) (varmq.IPersistentPriorityQueue, error)
	close() error
}
