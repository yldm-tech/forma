package forma2import

import (
	"context"
	"fmt"
)

// RunWithColumn is Run with one column of one table spec read through expr instead of the mapping's own expression: a mapping mistake, which verification must notice.
func RunWithColumn(ctx context.Context, opts Options, target, from, column, expr string) (*Report, error) {
	return run(ctx, opts, func(specs []*tableSpec) {
		for _, s := range specs {
			if s.target != target || s.label() != from {
				continue
			}
			for i := range s.cols {
				if s.cols[i].name == column {
					s.cols[i].expr = expr
					return
				}
			}
		}
		panic(fmt.Sprintf("no column %s in %s from %s", column, target, from))
	})
}
