package fairgate

import (
	"context"
	"errors"
	"testing"
)

func TestIterate_SinglePage(t *testing.T) {
	items := []string{"item1", "item2", "item3"}

	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		if params.PageNo != 1 {
			t.Errorf("expected PageNo = 1, got %d", params.PageNo)
		}
		if params.PageLimit != 100 {
			t.Errorf("expected PageLimit = 100, got %d", params.PageLimit)
		}
		return items, Pagination{
			TotalRecords: 3,
			TotalPages:   1,
			PageNo:       1,
			PageLimit:    100,
		}, nil
	}

	var collected []string
	for item, err := range iterate(context.Background(), fetcher) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		collected = append(collected, item)
	}

	if len(collected) != len(items) {
		t.Errorf("expected %d items, got %d", len(items), len(collected))
	}

	for i, item := range collected {
		if item != items[i] {
			t.Errorf("item[%d] = %v, want %v", i, item, items[i])
		}
	}
}

func TestIterate_MultiplePages(t *testing.T) {
	page1 := []string{"item1", "item2"}
	page2 := []string{"item3", "item4"}
	page3 := []string{"item5"}

	callCount := 0
	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		callCount++

		switch params.PageNo {
		case 1:
			return page1, Pagination{TotalRecords: 5, TotalPages: 3, PageNo: 1, PageLimit: 2}, nil
		case 2:
			return page2, Pagination{TotalRecords: 5, TotalPages: 3, PageNo: 2, PageLimit: 2}, nil
		case 3:
			return page3, Pagination{TotalRecords: 5, TotalPages: 3, PageNo: 3, PageLimit: 2}, nil
		default:
			t.Fatalf("unexpected page number: %d", params.PageNo)
			return nil, Pagination{}, nil
		}
	}

	var collected []string
	for item, err := range iterate(context.Background(), fetcher) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		collected = append(collected, item)
	}

	want := []string{"item1", "item2", "item3", "item4", "item5"}
	if len(collected) != len(want) {
		t.Errorf("expected %d items, got %d", len(want), len(collected))
	}

	for i, item := range collected {
		if item != want[i] {
			t.Errorf("item[%d] = %v, want %v", i, item, want[i])
		}
	}

	if callCount != 3 {
		t.Errorf("expected 3 fetcher calls, got %d", callCount)
	}
}

func TestIterate_EmptyResults(t *testing.T) {
	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		return []string{}, Pagination{
			TotalRecords: 0,
			TotalPages:   0,
			PageNo:       1,
			PageLimit:    100,
		}, nil
	}

	var collected []string
	for item, err := range iterate(context.Background(), fetcher) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		collected = append(collected, item)
	}

	if len(collected) != 0 {
		t.Errorf("expected 0 items, got %d", len(collected))
	}
}

func TestIterate_Error(t *testing.T) {
	expectedErr := errors.New("fetch error")

	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		return nil, Pagination{}, expectedErr
	}

	var errCount int
	for _, err := range iterate(context.Background(), fetcher) {
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
		errCount++
	}

	if errCount != 1 {
		t.Errorf("expected 1 error, got %d", errCount)
	}
}

func TestIterate_ErrorOnSecondPage(t *testing.T) {
	expectedErr := errors.New("second page error")

	callCount := 0
	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		callCount++

		if params.PageNo == 1 {
			return []string{
					"item1",
					"item2",
				}, Pagination{
					TotalRecords: 10,
					TotalPages:   2,
					PageNo:       1,
					PageLimit:    2,
				}, nil
		}
		return nil, Pagination{}, expectedErr
	}

	var collected []string
	var gotErr error
	for item, err := range iterate(context.Background(), fetcher) {
		if err != nil {
			gotErr = err
			break
		}
		collected = append(collected, item)
	}

	if len(collected) != 2 {
		t.Errorf("expected 2 items before error, got %d", len(collected))
	}

	if gotErr == nil {
		t.Fatal("expected error on second page")
	}

	if !errors.Is(gotErr, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, gotErr)
	}
}

func TestIterate_EarlyTermination(t *testing.T) {
	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		return []string{"item1", "item2", "item3", "item4", "item5"},
			Pagination{TotalRecords: 5, TotalPages: 1, PageNo: 1, PageLimit: 100}, nil
	}

	var collected []string
	maxItems := 3
	for item, err := range iterate(context.Background(), fetcher) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		collected = append(collected, item)
		if len(collected) >= maxItems {
			break
		}
	}

	if len(collected) != maxItems {
		t.Errorf("expected %d items, got %d", maxItems, len(collected))
	}
}

func TestIterate_NoTotalPages(t *testing.T) {
	// Test case where TotalPages is 0 (API doesn't provide it)
	// Iterator should stop when no items are returned
	callCount := 0
	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		callCount++

		if params.PageNo == 1 {
			return []string{"item1"}, Pagination{PageNo: 1, PageLimit: 100}, nil
		}
		// Second call returns empty
		return []string{}, Pagination{PageNo: 2, PageLimit: 100}, nil
	}

	var collected []string
	for item, err := range iterate(context.Background(), fetcher) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		collected = append(collected, item)
	}

	if len(collected) != 1 {
		t.Errorf("expected 1 item, got %d", len(collected))
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls (one returning items, one empty), got %d", callCount)
	}
}

func TestIterate_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callCount := 0
	fetcher := func(ctx context.Context, params PageParams) ([]string, Pagination, error) {
		callCount++

		// Check if context is cancelled
		if ctx.Err() != nil {
			return nil, Pagination{}, ctx.Err()
		}

		return []string{"item1", "item2"},
			Pagination{TotalRecords: 100, TotalPages: 50, PageNo: params.PageNo, PageLimit: 2}, nil
	}

	var collected []string
	for item, err := range iterate(ctx, fetcher) {
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected context.Canceled error, got %v", err)
			}
			break
		}
		collected = append(collected, item)

		// Cancel after collecting 2 items
		if len(collected) == 2 {
			cancel()
		}
	}

	if len(collected) != 2 {
		t.Errorf("expected 2 items before cancellation, got %d", len(collected))
	}
}
