package fairgate

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// ProductionURL is the official production endpoint.
	ProductionURL = "https://fsa.fairgate.ch/"
	// TestURL is the official test endpoint.
	TestURL = "https://fsa-test.fairgate.ch/"

	modulePath = "thde.io/fairgate"
)

var (
	// ErrStatus is returned when the API returns an unexpected status code.
	ErrStatus = errors.New("unexpected status code")
	// ErrNoAccessKey is returned when no access key is available.
	ErrNoAccessKey = errors.New("no token available")
	// ErrNoRefreshToken is returned when no refresh token is available.
	ErrNoRefreshToken = errors.New("no refresh token available")
	// ErrRateLimit is returned when the rate limit is exceeded.
	ErrRateLimit = errors.New("rate limit exceeded")
)

// Client holds configuration needed to call the Fairgate Standard API.
// Use [New] to create a new client.
type Client struct {
	baseURL *url.URL

	oid        string
	httpClient *http.Client
	userAgent  string

	auth *tokenStore

	retryAftertMU sync.Mutex
	retryAfter    time.Time
}

// ClientOption configures a Client before use.
type ClientOption func(*Client)

// WithBaseURL sets a custom base URL.
func WithBaseURL(baseURL *url.URL) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithTest configures the client to use the Fairgate test endpoint.
func WithTest() ClientOption {
	return func(c *Client) {
		testURL, _ := url.Parse(TestURL)

		c.baseURL = testURL
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithAccessKey configures the client to use the provided access key so it can lazily obtain tokens when needed.
// If not provided, [Client.TokenCreate] needs to be called explicitly.
func WithAccessKey(accessKey string) ClientOption {
	return func(c *Client) {
		c.auth.accessKey = accessKey
	}
}

// WithJWTParser configures the client to use the provided JWT parser.
// Not recommended for production use.
func WithJWTParser(parser *jwt.Parser) ClientOption {
	return func(c *Client) {
		c.auth.parser = parser
	}
}

// WithUserAgent sets a custom User-Agent header for API requests.
func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) {
		c.userAgent = userAgent
	}
}

// New creates a Fairgate API client for the provided organisation.
// The client defaults to the production Fairgate endpoint and applies any
// provided options.
func New(oid string, key *ecdsa.PublicKey, opts ...ClientOption) *Client {
	productionURL, _ := url.Parse(ProductionURL)

	c := &Client{
		baseURL: productionURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		oid: oid,
		auth: &tokenStore{
			parser: jwt.NewParser(
				jwt.WithValidMethods([]string{signingMethod.Alg()}),
				jwt.WithLeeway(2*time.Minute),
			),
			keyFunc: func(verifyKey *jwt.Token) (any, error) {
				return key, nil
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.userAgent == "" {
		c.userAgent = userAgent()
	}

	return c
}

// version returns the module version of the fairgate package.
// It returns "devel" if built without module version information.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}

	for _, dep := range info.Deps {
		if dep.Path == modulePath {
			if dep.Version == "(devel)" {
				return "devel"
			}

			return dep.Version
		}
	}

	if info.Main.Path == modulePath {
		if info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		// If main version is (devel), we can try to read vcs revision
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return "devel+" + setting.Value[:7]
			}
		}
	}

	return "devel"
}

// userAgent returns the default User-Agent string for this package.
func userAgent() string {
	v := version()
	goVersion := runtime.Version()
	os := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("go-fairgate/%s (%s; %s/%s)", v, goVersion, os, arch)
}
