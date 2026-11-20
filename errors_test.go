package fairgate

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func newTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestDecodeResponse(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		want       string
		wantStatus int // 0 means no APIError expected
		wantMsg    string
		wantErr    bool
	}{
		{
			name:   "success",
			status: http.StatusOK,
			body:   `{"code":200,"success":true,"data":"ok"}`,
			want:   "ok",
		},
		{
			name:       "envelope failure with HTTP 200",
			status:     http.StatusOK,
			body:       `{"code":404,"success":false,"message":"not found"}`,
			wantStatus: 404,
			wantMsg:    "not found",
			wantErr:    true,
		},
		{
			name:       "non-2xx with envelope",
			status:     http.StatusBadRequest,
			body:       `{"code":400,"success":false,"message":"bad","errors":[{"field":"x","message":"required"}]}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "bad",
			wantErr:    true,
		},
		{
			name:       "non-2xx without envelope",
			status:     http.StatusBadGateway,
			body:       `<html>oops</html>`,
			wantStatus: http.StatusBadGateway,
			wantMsg:    http.StatusText(http.StatusBadGateway),
			wantErr:    true,
		},
		{
			name:    "invalid JSON on success",
			status:  http.StatusOK,
			body:    `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeResponse[string](newTestResponse(tt.status, tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("decodeResponse() = %q, want %q", got, tt.want)
			}
			if tt.wantStatus == 0 {
				return
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v is not an *APIError", err)
			}
			if apiErr.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.wantStatus)
			}
			if apiErr.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", apiErr.Message, tt.wantMsg)
			}
		})
	}
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		StatusCode: 400,
		Message:    "validation failed",
		Errors:     []FieldError{{Field: "email", Message: "invalid"}},
	}

	want := "fairgate: status 400: validation failed; email: invalid"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
