package fairgate

import (
	"errors"
	"fmt"
)

// Response represents a response from the Fairgate API.
type Response[T any] struct {
	Code    int     `json:"code"`
	Success bool    `json:"success"`
	Message string  `json:"message"`
	Data    T       `json:"data"`
	Errors  []Error `json:"errors,omitempty"`
}

// Error returns an error if the response is not successful.
func (r Response[T]) Error() error {
	if r.Success {
		return nil
	}

	errs := []error{}
	if r.Message != "" {
		errs = append(errs, errors.New(r.Message))
	}
	for _, e := range r.Errors {
		errs = append(errs, error(e))
	}

	return errors.Join(errs...)
}

// Error represents an error from the API.
type Error struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
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
