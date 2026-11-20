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
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

// The wait tests run in a synctest bubble, where time is fake and only
// advances while every goroutine is blocked, so durations are exact.

func TestClient_wait(t *testing.T) {
	tests := []struct {
		name    string
		retryIn time.Duration // relative to now; 0 leaves retryAfter unset
		want    time.Duration
	}{
		{name: "retryAfter unset", want: 0},
		{name: "retryAfter in the past", retryIn: -time.Second, want: 0},
		{
			name:    "retryAfter in the future",
			retryIn: 200 * time.Millisecond,
			want:    200 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := &Client{}
				if tt.retryIn != 0 {
					c.retryAfter = time.Now().Add(tt.retryIn)
				}

				start := time.Now()
				if err := c.wait(t.Context()); err != nil {
					t.Fatalf("wait() error = %v, want nil", err)
				}
				if got := time.Since(start); got != tt.want {
					t.Errorf("wait() took %v, want %v", got, tt.want)
				}
			})
		})
	}
}

func TestClient_wait_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Client{}
		c.retryAfter = time.Now().Add(10 * time.Second)

		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		if err := c.wait(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("wait() error = %v, want context.Canceled", err)
		}
		if got := time.Since(start); got != 50*time.Millisecond {
			t.Errorf("wait() took %v, want 50ms", got)
		}
	})
}

func TestClient_wait_ContextTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Client{}
		c.retryAfter = time.Now().Add(10 * time.Second)

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		if err := c.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("wait() error = %v, want context.DeadlineExceeded", err)
		}
		if got := time.Since(start); got != 50*time.Millisecond {
			t.Errorf("wait() took %v, want 50ms", got)
		}
	})
}

func TestClient_wait_Concurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Client{}
		c.retryAfter = time.Now().Add(100 * time.Millisecond)
		start := time.Now()

		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				if err := c.wait(t.Context()); err != nil {
					t.Errorf("wait() error = %v", err)
				}
				if got := time.Since(start); got != 100*time.Millisecond {
					t.Errorf("wait() took %v, want 100ms", got)
				}
			})
		}
		wg.Wait()
	})
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

// handlerTransport is an [http.RoundTripper] that serves requests in
// memory with an [http.Handler]. Unlike httptest.NewServer it involves no
// network I/O, so it can be used inside a synctest bubble.
type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		defer r.Body.Close()
	}

	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, r)

	resp := rec.Result()
	resp.Request = r
	return resp, nil
}

// newAuthenticatedTestClient returns a client that is served in memory by
// handler and has a valid token in place, and the key that signs the
// client's tokens.
func newAuthenticatedTestClient(
	t *testing.T,
	handler http.Handler,
) (*Client, *ecdsa.PrivateKey) {
	t.Helper()
	privateKey, publicKey := generateTestKeyPair(t)

	c := New("test-org", publicKey,
		WithHTTPClient(&http.Client{Transport: handlerTransport{handler}}),
		WithBaseURL(mustParseURL("http://fairgate.test/")),
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	c, _ := newAuthenticatedTestClient(t, handler)
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Ratelimit-Retry-After", strconv.FormatInt(time.Now().Unix(), 10))
		w.WriteHeader(http.StatusTooManyRequests)
	})

	c, _ := newAuthenticatedTestClient(t, handler)
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"success":false,"message":"contact not found"}`))
	})

	c, _ := newAuthenticatedTestClient(t, handler)
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	c, privateKey := newAuthenticatedTestClient(t, handler)
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/refresh/") {
			_ = json.NewEncoder(w).Encode(Response[CreateTokenResponse]{
				Success: true,
				Data:    CreateTokenResponse{Token: freshToken.Load().(string), RefreshToken: "r2"},
			})
			return
		}
		apiCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})

	c, privateKey := newAuthenticatedTestClient(t, handler)
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

func TestClient_do_WaitsForRateLimitReset(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The handler runs in the calling goroutine, so no locking is needed.
		var calls []time.Time
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, time.Now())
			if len(calls) == 1 {
				reset := time.Now().Add(30 * time.Second).Unix()
				w.Header().Set("X-Ratelimit-Retry-After", strconv.FormatInt(reset, 10))
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":"ok"}`))
		})

		c, _ := newAuthenticatedTestClient(t, handler)
		req, err := c.newRequest(t.Context(), http.MethodGet, "/x", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := doJSON[string](c, req); err != nil {
			t.Fatalf("doJSON() error = %v", err)
		}

		if len(calls) != 2 {
			t.Fatalf("server called %d times, want 2", len(calls))
		}
		if got := calls[1].Sub(calls[0]); got != 30*time.Second {
			t.Errorf("retry sent after %v, want 30s", got)
		}

		// The reset applies to the client, not just the request that hit it:
		// a later request goes out immediately once the reset has passed.
		start := time.Now()
		if _, err := doJSON[string](c, req); err != nil {
			t.Fatalf("doJSON() error = %v", err)
		}
		if got := calls[2].Sub(start); got != 0 {
			t.Errorf("request after reset waited %v, want 0", got)
		}
	})
}

func TestClient_do_RateLimitWaitHonoursContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls int
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			reset := time.Now().Add(time.Minute).Unix()
			w.Header().Set("X-Ratelimit-Retry-After", strconv.FormatInt(reset, 10))
			w.WriteHeader(http.StatusTooManyRequests)
		})

		c, _ := newAuthenticatedTestClient(t, handler)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		req, err := c.newRequest(ctx, http.MethodGet, "/x", nil, nil)
		if err != nil {
			t.Fatal(err)
		}

		start := time.Now()
		_, err = c.do(req)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("do() error = %v, want context.DeadlineExceeded", err)
		}
		if got := time.Since(start); got != 10*time.Second {
			t.Errorf("do() returned after %v, want 10s", got)
		}
		if calls != 1 {
			t.Errorf("server called %d times, want 1", calls)
		}
	})
}
