package fairgate

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestRewindBody_NoBody(t *testing.T) {
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

			_, err := rewindBody(req)
			if err != nil {
				t.Errorf("rewindBody() error = %v, want nil for %s", err, tt.name)
			}
		})
	}
}

func TestRewindBody_WithGetBody(t *testing.T) {
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
	rewound, err := rewindBody(req)
	if err != nil {
		t.Fatalf("rewindBody() error = %v", err)
	}

	// Verify we can read the body again
	rewoundContent, err := io.ReadAll(rewound.Body)
	if err != nil {
		t.Fatalf("failed to read rewound body: %v", err)
	}

	if !bytes.Equal(rewoundContent, bodyContent) {
		t.Errorf("rewound body = %q, want %q", rewoundContent, bodyContent)
	}
}

func TestRewindBody_NoGetBody(t *testing.T) {
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

	_, err = rewindBody(req)
	if err == nil {
		t.Fatal("rewindBody() should return error when GetBody is nil")
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

// newAuthenticatedTestClient returns a client for server with a valid token
// already in place, and the key that signs the client's tokens.
func newAuthenticatedTestClient(
	t *testing.T,
	server *httptest.Server,
) (*Client, *ecdsa.PrivateKey) {
	t.Helper()
	privateKey, publicKey := generateTestKeyPair(t)

	c := New("test-org", publicKey,
		WithHTTPClient(server.Client()),
		WithBaseURL(mustParseURL(server.URL)),
	)
	err := c.auth.updateToken(CreateTokenResponse{
		Token:        createTestToken(t, privateKey, time.Now().Add(time.Hour)),
		RefreshToken: "refresh",
	})
	if err != nil {
		t.Fatalf("updateToken() error = %v", err)
	}

	return c, privateKey
}

func TestClient_do_RetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `"payload"` {
			t.Errorf("attempt %d body = %q, want %q", calls.Load()+1, body, `"payload"`)
		}
		if calls.Add(1) == 1 {
			w.Header().Set("X-Ratelimit-Retry-After", strconv.FormatInt(time.Now().Unix(), 10))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":"ok"}`))
	}))
	defer server.Close()

	c, _ := newAuthenticatedTestClient(t, server)
	req, err := c.newRequest(context.Background(), http.MethodPost, "/x", nil, "payload")
	if err != nil {
		t.Fatal(err)
	}

	got, err := doJSON[string](c, req)
	if err != nil {
		t.Fatalf("doJSON() error = %v", err)
	}
	if got != "ok" {
		t.Errorf("doJSON() = %q, want ok", got)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server called %d times, want 2", n)
	}
}

func TestClient_do_RateLimitGivesUp(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Ratelimit-Retry-After", strconv.FormatInt(time.Now().Unix(), 10))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	c, _ := newAuthenticatedTestClient(t, server)
	req, err := c.newRequest(context.Background(), http.MethodGet, "/x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.do(req)
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("do() error = %v, want ErrRateLimit", err)
	}
	if n := calls.Load(); n != maxAttempts {
		t.Errorf("server called %d times, want %d", n, maxAttempts)
	}
}

func TestClient_do_StatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"success":false,"message":"contact not found"}`))
	}))
	defer server.Close()

	c, _ := newAuthenticatedTestClient(t, server)
	_, err := c.Contact(context.Background(), 1)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Contact() error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Message != "contact not found" {
		t.Errorf("APIError = %+v", apiErr)
	}
}

func TestClient_do_ReauthenticatesOnce(t *testing.T) {
	var freshToken atomic.Value
	var apiCalls, refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/refresh/") {
			refreshCalls.Add(1)
			_ = json.NewEncoder(w).Encode(Response[CreateTokenResponse]{
				Success: true,
				Data:    CreateTokenResponse{Token: freshToken.Load().(string), RefreshToken: "r2"},
			})
			return
		}

		apiCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+freshToken.Load().(string) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":"ok"}`))
	}))
	defer server.Close()

	c, privateKey := newAuthenticatedTestClient(t, server)
	freshToken.Store(createTestToken(t, privateKey, time.Now().Add(2*time.Hour)))

	req, err := c.newRequest(context.Background(), http.MethodGet, "/x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doJSON[string](c, req); err != nil {
		t.Fatalf("doJSON() error = %v", err)
	}
	if apiCalls.Load() != 2 || refreshCalls.Load() != 1 {
		t.Errorf("api calls = %d, refresh calls = %d, want 2 and 1",
			apiCalls.Load(), refreshCalls.Load())
	}
}

func TestClient_do_ReauthenticatesOnlyOnce(t *testing.T) {
	var freshToken atomic.Value
	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/refresh/") {
			_ = json.NewEncoder(w).Encode(Response[CreateTokenResponse]{
				Success: true,
				Data:    CreateTokenResponse{Token: freshToken.Load().(string), RefreshToken: "r2"},
			})
			return
		}
		apiCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c, privateKey := newAuthenticatedTestClient(t, server)
	freshToken.Store(createTestToken(t, privateKey, time.Now().Add(2*time.Hour)))

	req, err := c.newRequest(context.Background(), http.MethodGet, "/x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.do(req)

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("do() error = %v, want 401 APIError", err)
	}
	if n := apiCalls.Load(); n != 2 {
		t.Errorf("api calls = %d, want 2", n)
	}
}
