package fairgate

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Response is the envelope wrapping every Fairgate API response.
type Response[T any] struct {
	Code    int          `json:"code"`
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Data    T            `json:"data"`
	Errors  []FieldError `json:"errors,omitempty"`
}

// Err returns an [*APIError] if the response reports a failure, or nil.
func (r Response[T]) Err() error {
	if r.Success {
		return nil
	}

	return &APIError{StatusCode: r.Code, Message: r.Message, Errors: r.Errors}
}

// decodeResponse checks the status code of resp and decodes its envelope,
// returning the payload or an [*APIError]. It does not close the body.
func decodeResponse[T any](resp *http.Response) (T, error) {
	var zero T
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, newAPIError(resp)
	}

	var r Response[T]
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return zero, fmt.Errorf("fairgate: decode response: %w", err)
	}
	if err := r.Err(); err != nil {
		return zero, err
	}

	return r.Data, nil
}

// Pagination represents pagination information from the API.
type Pagination struct {
	TotalRecords int `json:"totalRecords,omitempty"`
	TotalPages   int `json:"totalPages,omitempty"`
	PageNo       int `json:"pageNo,omitempty"`
	PageLimit    int `json:"pageLimit,omitempty"`
}

// PageParams represents pagination parameters for API requests.
type PageParams struct {
	PageNo    int `url:"pageNo,omitempty"`
	PageLimit int `url:"pageLimit,omitempty"`
}
