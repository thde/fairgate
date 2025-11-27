package fairgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// newRequest creates a new HTTP request.
func (c *Client) newRequest(
	ctx context.Context,
	method, path string,
	params url.Values,
	body any,
) (*http.Request, error) {
	rel := &url.URL{Path: path}
	u := c.baseURL.ResolveReference(rel)
	u.RawQuery = params.Encode()

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("User-Agent", c.userAgent)

	return req, nil
}

// doJSON executes the request and decodes JSON response.
func (c *Client) doJSON(req *http.Request, v any) (*http.Response, error) {
	resp, err := c.do(req)
	if err != nil {
		return resp, err
	}
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	if v != nil {
		err = json.NewDecoder(resp.Body).Decode(v)
	}

	return resp, err
}

// do executes the request with automatic token refresh and rate limit retries.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if err := c.wait(req.Context()); err != nil {
		return nil, err
	}

	if err := c.TokenRefresh(req.Context()); err != nil {
		return nil, err
	}
	c.auth.Lock()
	req.Header.Set("Authorization", "Bearer "+c.auth.token)
	c.auth.Unlock()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		err := c.handleRetryAfter(resp.Header.Get("X-Ratelimit-Retry-After"))
		if err != nil {
			return nil, fmt.Errorf("too many requests: %w, %w", err, ErrRateLimit)
		}

		if err := c.rewindBody(req); err != nil {
			return resp, fmt.Errorf("cannot rewind body: %w, %w", err, ErrRateLimit)
		}

		return c.do(req)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return resp, fmt.Errorf(
			"%s: %d, %w",
			http.StatusText(resp.StatusCode),
			resp.StatusCode,
			ErrStatus,
		)
	}

	return resp, nil
}

// wait checks if the client is currently rate-limited.
// If so, it blocks until the reset time or until the context is canceled.
func (c *Client) wait(ctx context.Context) error {
	c.retryAftertMU.Lock()
	waitUntil := c.retryAfter
	c.retryAftertMU.Unlock()

	if time.Now().After(waitUntil) {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(waitUntil)):
		return nil
	}
}

// handleRetryAfter updates the client's retry-after timestamp based on the
// value in the X-Ratelimit-Retry-After header.
func (c *Client) handleRetryAfter(header string) error {
	if header == "" {
		return fmt.Errorf("missing X-Ratelimit-Retry-After header")
	}

	ts, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid X-Ratelimit-Retry-After header %q: %w", header, err)
	}

	t := time.Unix(ts, 0)

	c.retryAftertMU.Lock()
	defer c.retryAftertMU.Unlock()

	if t.After(c.retryAfter) {
		c.retryAfter = t
	}

	return nil
}

// rewindBody attempts to reset the request body for a retry.
func (c *Client) rewindBody(req *http.Request) error {
	// If there is no body, there is nothing to rewind.
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}

	// If GetBody is nil, we cannot recreate the reader.
	// This happens with io.Pipe or raw io.Reader inputs.
	if req.GetBody == nil {
		return fmt.Errorf("cannot rewind body: GetBody is nil")
	}

	freshBody, err := req.GetBody()
	if err != nil {
		return err
	}

	req.Body = freshBody
	return nil
}
