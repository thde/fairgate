package fairgate

import (
	"context"
	"iter"
)

// defaultPageLimit is the page size used when iterating over all items.
const defaultPageLimit = 100

// paginatorFunc fetches a single page of items T.
type paginatorFunc[T any] func(context.Context, PageParams) ([]T, Pagination, error)

// iterate returns an iterator that walks through all pages using fetch.
// It stops after the last page, an empty page, or the first error.
func iterate[T any](ctx context.Context, fetch paginatorFunc[T]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		params := PageParams{PageNo: 1, PageLimit: defaultPageLimit}

		for {
			items, meta, err := fetch(ctx, params)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}

			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}

			if len(items) == 0 || (meta.TotalPages > 0 && params.PageNo >= meta.TotalPages) {
				return
			}
			params.PageNo++
		}
	}
}
