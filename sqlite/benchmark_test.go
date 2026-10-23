package sqlite

import (
	"fmt"
	"os"
	"testing"
)

// checkpoints are the WAL checkpoint page counts every row runs at. 1000 is
// what sqliteq v0.2.3 always uses; a version that cannot set it skips the rest.
var checkpoints = []int{1000, 10000}

// BenchmarkSQLite enqueues and drains jobs through a varmq worker bound to a
// sqliteq queue. Names are <scenario>/<layout>-ckpt<n>/<label>, and the label
// is SQLITEQ_LABEL (default "sqliteq") so runs of two versions can sit side by
// side in one chart.
func BenchmarkSQLite(b *testing.B) {
	label := os.Getenv("SQLITEQ_LABEL")
	if label == "" {
		label = "sqliteq"
	}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			for _, l := range sc.layouts {
				for _, ckpt := range checkpoints {
					b.Run(fmt.Sprintf("%s-ckpt%d", l, ckpt), func(b *testing.B) {
						b.Run(label, func(b *testing.B) {
							runScenario(b, sc, l, ckpt)
						})
					})
				}
			}
		})
	}
}
