package fairgate

import (
	"context"
	"net/http"
	"testing"
)

func TestClient_newRequest_URL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		orgID   string
		want    string
	}{
		{
			name:    "production base URL",
			baseURL: ProductionURL,
			orgID:   "org",
			want:    "https://fsa.fairgate.ch/fsa/v2.0/contact/org/contacts",
		},
		{
			name:    "base URL path prefix is kept",
			baseURL: "https://proxy.example.com/prefix/",
			orgID:   "org",
			want:    "https://proxy.example.com/prefix/fsa/v2.0/contact/org/contacts",
		},
		{
			name:    "base URL without trailing slash",
			baseURL: "https://proxy.example.com/prefix",
			orgID:   "org",
			want:    "https://proxy.example.com/prefix/fsa/v2.0/contact/org/contacts",
		},
		{
			name:    "org ID with path separator is escaped",
			baseURL: ProductionURL,
			orgID:   "ab/c+d=",
			want:    "https://fsa.fairgate.ch/fsa/v2.0/contact/ab%2Fc+d=/contacts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(tt.orgID, nil, WithBaseURL(mustParseURL(tt.baseURL)))
			req, err := c.newRequest(
				context.Background(),
				http.MethodGet,
				c.orgPath("/fsa/v2.0/contact/%s/contacts"),
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("newRequest() error = %v", err)
			}
			if got := req.URL.String(); got != tt.want {
				t.Errorf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWithBaseURL_Copies(t *testing.T) {
	u := mustParseURL("https://a.example.com/")
	c := New("org", nil, WithBaseURL(u))
	u.Host = "b.example.com"

	if c.baseURL.Host != "a.example.com" {
		t.Errorf("baseURL.Host = %q, want a.example.com", c.baseURL.Host)
	}
}
