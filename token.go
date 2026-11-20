package fairgate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signingMethod is the method used by FSA:
// https://fsa.fairgate.ch/documentation/getting-started/fsa-sdk/
var signingMethod = jwt.SigningMethodES512

const (
	// tokenLeeway is the clock skew tolerated when validating tokens.
	tokenLeeway = 2 * time.Minute
	// refreshMargin is how long before expiry a token is refreshed.
	refreshMargin = 2 * time.Minute
)

// tokenStore holds the client's credentials. mu guards all fields and is
// held for the duration of a token request, so concurrent callers wait
// for a single create or refresh instead of each issuing their own.
type tokenStore struct {
	mu           sync.Mutex
	token        string
	refreshToken string
	accessKey    string
	claim        *jwtClaim

	keyFunc jwt.Keyfunc
	parser  *jwt.Parser
}

// jwtClaim represents the custom claims in Fairgate JWT tokens.
type jwtClaim struct {
	FsaID  string `json:"fsa_id"`
	UniqID string `json:"uniq_id"`
	jwt.RegisteredClaims
}

// CreateTokenRequest is the body of a token creation request.
type CreateTokenRequest struct {
	AccessKey string `json:"access_key"`
}

// CreateTokenResponse is returned by the token creation and refresh endpoints.
type CreateTokenResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenRequest is the body of a token refresh request.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenCreate creates a token using accessKey. On success the access key is
// kept, so the client can create a new token once the refresh token is no
// longer accepted. Calling it is only needed if [WithAccessKey] was not used.
func (c *Client) TokenCreate(ctx context.Context, accessKey string) error {
	if accessKey == "" {
		return ErrNoAccessKey
	}

	c.auth.mu.Lock()
	defer c.auth.mu.Unlock()

	if err := c.createToken(ctx, accessKey); err != nil {
		return err
	}
	c.auth.accessKey = accessKey

	return nil
}

// TokenRefresh creates or refreshes the token if it is missing or about to
// expire. API calls do this automatically.
func (c *Client) TokenRefresh(ctx context.Context) error {
	_, err := c.validToken(ctx)
	return err
}

// validToken returns a token that is valid for at least refreshMargin,
// creating or refreshing it as needed. If a refresh fails and an access key
// is available, a new token is created instead.
func (c *Client) validToken(ctx context.Context) (string, error) {
	c.auth.mu.Lock()
	defer c.auth.mu.Unlock()

	if c.auth.token == "" {
		if err := c.createToken(ctx, c.auth.accessKey); err != nil {
			return "", err
		}
		return c.auth.token, nil
	}

	if !c.auth.shouldRefresh(time.Now()) {
		return c.auth.token, nil
	}

	err := c.refreshToken(ctx)
	if err != nil && c.auth.accessKey != "" {
		if createErr := c.createToken(ctx, c.auth.accessKey); createErr != nil {
			return "", errors.Join(err, createErr)
		}
		err = nil
	}
	if err != nil {
		return "", err
	}

	return c.auth.token, nil
}

// invalidateToken forces the next validToken call to refresh, unless the
// token has already been replaced by another caller.
func (c *Client) invalidateToken(token string) {
	c.auth.mu.Lock()
	defer c.auth.mu.Unlock()

	if c.auth.token == token {
		c.auth.claim = nil
	}
}

// createToken obtains a new token using accessKey. c.auth.mu must be held.
func (c *Client) createToken(ctx context.Context, accessKey string) error {
	if accessKey == "" {
		return ErrNoAccessKey
	}

	return c.authRequest(ctx,
		c.orgPath("/fsa/v1.1/auth/create/%s/token"),
		CreateTokenRequest{AccessKey: accessKey},
	)
}

// refreshToken obtains a new token using the stored refresh token.
// c.auth.mu must be held.
func (c *Client) refreshToken(ctx context.Context) error {
	if c.auth.refreshToken == "" {
		return ErrNoRefreshToken
	}

	return c.authRequest(ctx,
		c.orgPath("/fsa/v1.1/auth/refresh/%s/token"),
		RefreshTokenRequest{RefreshToken: c.auth.refreshToken},
	)
}

// authRequest posts body to a token endpoint and stores the returned
// token. c.auth.mu must be held.
func (c *Client) authRequest(ctx context.Context, path string, body any) error {
	req, err := c.newRequest(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := decodeResponse[CreateTokenResponse](resp)
	if err != nil {
		return err
	}

	return c.auth.updateToken(data)
}

// shouldRefresh reports whether the token is missing or expires within
// refreshMargin of now.
func (ts *tokenStore) shouldRefresh(now time.Time) bool {
	if ts.token == "" || ts.claim == nil {
		return true
	}

	exp, err := ts.claim.GetExpirationTime()
	if err != nil || exp == nil {
		// A token without an expiry never expires.
		return false
	}

	return now.Add(refreshMargin).After(exp.Time)
}

// updateToken validates and stores the token pair.
func (ts *tokenStore) updateToken(resp CreateTokenResponse) error {
	claim, err := ts.validateToken(resp.Token)
	if err != nil {
		return err
	}

	ts.claim = claim
	ts.token = resp.Token
	ts.refreshToken = resp.RefreshToken

	return nil
}

// validateToken verifies the token's signature and returns its claims.
func (ts *tokenStore) validateToken(tokenString string) (*jwtClaim, error) {
	claim := &jwtClaim{}
	if _, err := ts.parser.ParseWithClaims(tokenString, claim, ts.keyFunc); err != nil {
		return nil, fmt.Errorf("fairgate: parse token: %w", err)
	}

	return claim, nil
}
