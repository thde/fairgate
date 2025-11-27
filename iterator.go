package fairgate

import (
	"context"
	"iter"
)

// paginatorFunc fetches a single page of items T.
type paginatorFunc[T any] func(context.Context, PageParams) ([]T, Pagination, error)

// iterate returns an iterator that walks through all pages using the provided fetcher.
func iterate[T any](ctx context.Context, fetch paginatorFunc[T]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		params := PageParams{PageNo: 1, PageLimit: 100}

		for {
			items, meta, err := fetch(ctx, params)
			if err != nil {
				yield(*new(T), err)
				return
			}

			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}

			if meta.TotalPages > 0 && params.PageNo >= meta.TotalPages {
				return
			}
			if len(items) == 0 {
				return
			}
			params.PageNo++
		}
	}
}
