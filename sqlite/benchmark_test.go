package sqlite

import (
	"fmt"
	"testing"
)

// checkpoints are the WAL checkpoint page counts every row runs at.
var checkpoints = []int{1000, 10000}

// BenchmarkSQLite enqueues and drains jobs through a varmq worker bound to a
// sqliteq queue. Names are <scenario>/<layout>/ckpt<n>.
func BenchmarkSQLite(b *testing.B) {
	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			for _, l := range sc.layouts {
				b.Run(l.String(), func(b *testing.B) {
					for _, ckpt := range checkpoints {
						b.Run(fmt.Sprintf("ckpt%d", ckpt), func(b *testing.B) {
							runScenario(b, sc, l, ckpt)
						})
					}
				})
			}
		})
	}
}
