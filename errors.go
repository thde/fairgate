package fairgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrNoAccessKey is returned when a token must be created but no access
	// key is available.
	ErrNoAccessKey = errors.New("fairgate: no access key available")
	// ErrNoRefreshToken is returned when a token must be refreshed but no
	// refresh token is available.
	ErrNoRefreshToken = errors.New("fairgate: no refresh token available")
	// ErrRateLimit is returned when the rate limit is exceeded.
	ErrRateLimit = errors.New("fairgate: rate limit exceeded")
)

// maxErrorBody limits how much of an error response body is read.
const maxErrorBody = 1 << 20

// APIError is returned when the API responds with a non-2xx status code or
// reports a failure in the response envelope. Use [errors.As] to inspect it.
type APIError struct {
	// StatusCode is the HTTP status code, or the code reported in the
	// response envelope if the HTTP status was successful.
	StatusCode int
	Message    string
	Errors     []FieldError
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "fairgate: status %d", e.StatusCode)
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	for _, fe := range e.Errors {
		b.WriteString("; ")
		b.WriteString(fe.Error())
	}
	return b.String()
}

// FieldError describes a validation error for a single request field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string {
	return e.Field + ": " + e.Message
}

// newAPIError builds an [APIError] from a non-2xx response, using the
// response envelope's message and field errors if the body contains one.
func newAPIError(resp *http.Response) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode}

	var r Response[json.RawMessage]
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&r); err == nil {
		apiErr.Message = r.Message
		apiErr.Errors = r.Errors
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}

	return apiErr
}
