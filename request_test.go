package fairgate

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClient_handleRetryAfter(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid unix timestamp",
			header:  "1704110400", // 2024-01-01 12:00:00 UTC
			wantErr: false,
		},
		{
			name:    "valid unix timestamp - recent",
			header:  "1700000000",
			wantErr: false,
		},
		{
			name:        "empty header",
			header:      "",
			wantErr:     true,
			errContains: "missing X-Ratelimit-Retry-After header",
		},
		{
			name:        "invalid format - not a number",
			header:      "not-a-number",
			wantErr:     true,
			errContains: "invalid X-Ratelimit-Retry-After header",
		},
		{
			name:        "invalid format - float",
			header:      "1704110400.5",
			wantErr:     true,
			errContains: "invalid X-Ratelimit-Retry-After header",
		},
		{
			name:    "zero timestamp",
			header:  "0",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{}
			err := c.handleRetryAfter(tt.header)

			if (err != nil) != tt.wantErr {
				t.Errorf("handleRetryAfter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf(
						"handleRetryAfter() error = %v, should contain %q",
						err,
						tt.errContains,
					)
				}
			}
		})
	}
}

func TestClient_handleRetryAfter_UpdatesTime(t *testing.T) {
	c := &Client{}

	// Set initial retry time to a past time
	pastTime := time.Now().Add(-1 * time.Hour)
	c.retryAfter = pastTime

	// Update with a future time
	futureTimestamp := time.Now().Add(1 * time.Hour).Unix()
	err := c.handleRetryAfter(strconv.FormatInt(futureTimestamp, 10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.retryAfter.Before(pastTime) || c.retryAfter.Equal(pastTime) {
		t.Errorf("retryAfter should be updated to future time, got %v", c.retryAfter)
	}

	// Verify the exact time
	expectedTime := time.Unix(futureTimestamp, 0)
	if !c.retryAfter.Equal(expectedTime) {
		t.Errorf("retryAfter = %v, want %v", c.retryAfter, expectedTime)
	}
}

func TestClient_handleRetryAfter_OnlyUpdatesIfLater(t *testing.T) {
	c := &Client{}

	// Set initial retry time to a future time
	futureTime := time.Now().Add(2 * time.Hour)
	c.retryAfter = futureTime

	// Try to update with an earlier time
	earlierTimestamp := time.Now().Add(1 * time.Hour).Unix()
	err := c.handleRetryAfter(strconv.FormatInt(earlierTimestamp, 10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Retry time should not have changed
	if !c.retryAfter.Equal(futureTime) {
		t.Errorf(
			"retryAfter should not be updated to earlier time, got %v, want %v",
			c.retryAfter,
			futureTime,
		)
	}
}

func TestClient_wait_NoWaiting(t *testing.T) {
	c := &Client{}

	// Set retry time to the past
	c.retryAfter = time.Now().Add(-1 * time.Second)

	start := time.Now()
	err := c.wait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("wait() error = %v, want nil", err)
	}

	// Should return immediately
	if elapsed > 100*time.Millisecond {
		t.Errorf("wait() took %v, should be nearly instant", elapsed)
	}
}

func TestClient_wait_WaitsUntilTime(t *testing.T) {
	c := &Client{}

	// Set retry time to 200ms in the future
	waitDuration := 200 * time.Millisecond
	c.retryAfter = time.Now().Add(waitDuration)

	start := time.Now()
	err := c.wait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("wait() error = %v, want nil", err)
	}

	// Should wait approximately the specified duration
	// Allow some tolerance for timing inaccuracy
	if elapsed < waitDuration-50*time.Millisecond {
		t.Errorf("wait() took %v, expected at least %v", elapsed, waitDuration-50*time.Millisecond)
	}
	if elapsed > waitDuration+150*time.Millisecond {
		t.Errorf(
			"wait() took %v, expected no more than %v",
			elapsed,
			waitDuration+150*time.Millisecond,
		)
	}
}

func TestClient_wait_ContextCancellation(t *testing.T) {
	c := &Client{}

	// Set retry time far in the future
	c.retryAfter = time.Now().Add(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := c.wait(ctx)
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Errorf("wait() error = %v, want context.Canceled", err)
	}

	// Should return quickly due to cancellation
	if elapsed > 1*time.Second {
		t.Errorf("wait() took %v, should return quickly after cancellation", elapsed)
	}
}

func TestClient_wait_ContextTimeout(t *testing.T) {
	c := &Client{}

	// Set retry time far in the future
	c.retryAfter = time.Now().Add(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.wait(ctx)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("wait() error = %v, want context.DeadlineExceeded", err)
	}

	// Should timeout after approximately 50ms
	if elapsed > 200*time.Millisecond {
		t.Errorf("wait() took %v, should timeout quickly", elapsed)
	}
}

func TestClient_rewindBody_NoBody(t *testing.T) {
	c := &Client{}

	tests := []struct {
		name string
		body io.ReadCloser
	}{
		{
			name: "nil body",
			body: nil,
		},
		{
			name: "http.NoBody",
			body: http.NoBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com", tt.body)

			err := c.rewindBody(req)
			if err != nil {
				t.Errorf("rewindBody() error = %v, want nil for %s", err, tt.name)
			}
		})
	}
}

func TestClient_rewindBody_WithGetBody(t *testing.T) {
	c := &Client{}

	bodyContent := []byte("test request body")
	body := bytes.NewReader(bodyContent)

	req, err := http.NewRequest(http.MethodPost, "http://example.com", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Read the body to consume it
	_, err = io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	// Now rewind
	err = c.rewindBody(req)
	if err != nil {
		t.Fatalf("rewindBody() error = %v", err)
	}

	// Verify we can read the body again
	rewoundContent, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read rewound body: %v", err)
	}

	if !bytes.Equal(rewoundContent, bodyContent) {
		t.Errorf("rewound body = %q, want %q", rewoundContent, bodyContent)
	}
}

func TestClient_rewindBody_NoGetBody(t *testing.T) {
	c := &Client{}

	// Create a request with a body but no GetBody function
	req, err := http.NewRequest(
		http.MethodPost,
		"http://example.com",
		io.NopCloser(strings.NewReader("test")),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Clear GetBody to simulate a non-rewindable body
	req.GetBody = nil

	err = c.rewindBody(req)
	if err == nil {
		t.Error("rewindBody() should return error when GetBody is nil")
	}

	if !strings.Contains(err.Error(), "GetBody is nil") {
		t.Errorf("rewindBody() error = %v, should mention GetBody is nil", err)
	}
}

func TestClient_wait_ZeroTime(t *testing.T) {
	c := &Client{}
	// retryAfter is zero time by default

	start := time.Now()
	err := c.wait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("wait() error = %v, want nil", err)
	}

	// Should return immediately for zero time
	if elapsed > 50*time.Millisecond {
		t.Errorf("wait() took %v, should be nearly instant for zero time", elapsed)
	}
}

func TestClient_handleRetryAfter_ConcurrentAccess(t *testing.T) {
	c := &Client{}

	// Test concurrent updates don't cause race conditions
	done := make(chan bool)

	for i := range 10 {
		go func(i int) {
			timestamp := time.Now().Add(time.Duration(i) * time.Second).Unix()
			_ = c.handleRetryAfter(strconv.FormatInt(timestamp, 10))
			done <- true
		}(i)
	}

	for range 10 {
		<-done
	}

	// Just verify no panic occurred and retryAfter is set
	if c.retryAfter.IsZero() {
		t.Error("retryAfter should be set after concurrent updates")
	}
}

func TestClient_wait_ConcurrentAccess(t *testing.T) {
	c := &Client{}
	c.retryAfter = time.Now().Add(100 * time.Millisecond)

	// Multiple goroutines waiting concurrently
	done := make(chan bool)

	for range 5 {
		go func() {
			err := c.wait(context.Background())
			if err != nil {
				t.Errorf("wait() error = %v", err)
			}
			done <- true
		}()
	}

	for range 5 {
		<-done
	}
}
