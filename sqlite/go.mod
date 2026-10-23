module github.com/goptics/varmq-benchmarks/sqlite

go 1.26.0

require (
	github.com/goptics/sqliteq v0.2.3
	github.com/goptics/varmq v1.9.0
	github.com/mattn/go-sqlite3 v1.14.28
)

require github.com/lucsky/cuid v1.2.1 // indirect

// replace github.com/goptics/sqliteq => ../../sqliteq
// replace github.com/goptics/varmq => ../../varmq
