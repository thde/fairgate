package fairgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// orgPath formats an API path, inserting the path-escaped organisation ID
// in place of the first %s verb.
func (c *Client) orgPath(format string, args ...any) string {
	return fmt.Sprintf(format, append([]any{url.PathEscape(c.orgID)}, args...)...)
}

// newRequest creates a new HTTP request. path is relative to the base URL
// and must already be escaped.
func (c *Client) newRequest(
	ctx context.Context,
	method, path string,
	params url.Values,
	body any,
) (*http.Request, error) {
	u := c.baseURL.JoinPath(path)
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

// maxAttempts bounds how often a request is sent when it is rate limited.
const maxAttempts = 5

// doJSON executes the request and decodes the response envelope into T.
func doJSON[T any](c *Client, req *http.Request) (T, error) {
	resp, err := c.do(req)
	if err != nil {
		var zero T
		return zero, err
	}
	defer resp.Body.Close()

	return decodeResponse[T](resp)
}

// do sends the request with authentication, retrying up to maxAttempts
// times while rate limited and once with a fresh token if the token was
// rejected. It returns a response with a 2xx status, or an error.
// req itself is not modified.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	reauthenticated := false

	for attempt := 1; ; attempt++ {
		if err := c.wait(ctx); err != nil {
			return nil, err
		}

		token, err := c.validToken(ctx)
		if err != nil {
			return nil, err
		}

		r, err := rewindBody(req)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", "Bearer "+token)

		resp, err := c.httpClient.Do(r)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		apiErr := newAPIError(resp)
		drainAndClose(resp.Body)

		if resp.StatusCode == http.StatusUnauthorized && !reauthenticated && attempt < maxAttempts {
			reauthenticated = true
			c.invalidateToken(token)
			continue
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return nil, apiErr
		}
		if attempt == maxAttempts {
			return nil, fmt.Errorf("%w after %d attempts: %w", ErrRateLimit, attempt, apiErr)
		}
		if err := c.handleRetryAfter(resp.Header.Get("X-Ratelimit-Retry-After")); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRateLimit, err)
		}
	}
}

// drainAndClose reads a bounded amount of the remaining body so the
// connection can be reused, then closes it.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorBody))
	_ = body.Close()
}

// wait checks if the client is currently rate-limited.
// If so, it blocks until the reset time or until the context is canceled.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	waitUntil := c.retryAfter
	c.mu.Unlock()

	d := time.Until(waitUntil)
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// handleRetryAfter updates the client's retry-after timestamp based on the
// value in the X-Ratelimit-Retry-After header.
func (c *Client) handleRetryAfter(header string) error {
	if header == "" {
		return errors.New("missing X-Ratelimit-Retry-After header")
	}

	ts, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid X-Ratelimit-Retry-After header %q: %w", header, err)
	}

	t := time.Unix(ts, 0)

	c.mu.Lock()
	defer c.mu.Unlock()

	if t.After(c.retryAfter) {
		c.retryAfter = t
	}

	return nil
}

// rewindBody returns a copy of req with a fresh body, so it can be sent
// again. req itself is not modified.
func rewindBody(req *http.Request) (*http.Request, error) {
	r := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return r, nil
	}

	// GetBody is nil for bodies that cannot be recreated, such as an io.Pipe.
	if req.GetBody == nil {
		return nil, errors.New("fairgate: cannot rewind body: GetBody is nil")
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("fairgate: cannot rewind body: %w", err)
	}
	r.Body = body

	return r, nil
}
