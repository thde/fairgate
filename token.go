package fairgate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signingMethod is the method used by FSA:
// https://fsa.fairgate.ch/documentation/getting-started/fsa-sdk/
var signingMethod = jwt.SigningMethodES512

type tokenStore struct {
	sync.Mutex

	token        string
	refreshToken string
	accessKey    string

	claim *jwtClaim

	keyFunc jwt.Keyfunc
	parser  *jwt.Parser
}

// jwtClaim represents the custom claims in Fairgate JWT tokens.
type jwtClaim struct {
	FsaID  string `json:"fsa_id"`
	UniqID string `json:"uniq_id"`
	jwt.RegisteredClaims
}

type CreateTokenRequest struct {
	AccessKey string `json:"access_key"`
}

type CreateTokenResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// TokenCreate generates a JWT token using an access key.
func (c *Client) TokenCreate(ctx context.Context, accessKey string) error {
	c.auth.Lock()
	defer c.auth.Unlock()

	if accessKey == "" {
		return ErrNoAccessKey
	}

	req, err := c.newRequest(ctx,
		http.MethodPost,
		fmt.Sprintf("/fsa/v1.1/auth/create/%s/token", c.oid),
		nil,
		CreateTokenRequest{AccessKey: accessKey},
	)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	var authResp Response[CreateTokenResponse]
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return err
	}

	if err := authResp.Error(); err != nil {
		return err
	}

	return c.auth.updateToken(authResp.Data)
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenRefresh refreshes the JWT token if necessary.
func (c *Client) TokenRefresh(ctx context.Context) error {
	c.auth.Lock()
	if c.auth.token == "" {
		c.auth.Unlock()
		return c.TokenCreate(ctx, c.auth.accessKey)
	}

	defer c.auth.Unlock()

	if !c.auth.shouldRefresh(time.Now()) {
		return nil
	}

	if c.auth.refreshToken == "" {
		return ErrNoRefreshToken
	}

	reqBody := RefreshTokenRequest{RefreshToken: c.auth.refreshToken}
	path := fmt.Sprintf("/fsa/v1.1/auth/refresh/%s/token", c.oid)

	req, err := c.newRequest(ctx, http.MethodPost, path, nil, reqBody)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf(
			"%s: %d, %w",
			http.StatusText(resp.StatusCode),
			resp.StatusCode,
			ErrStatus,
		)
	}

	var authResp Response[CreateTokenResponse]
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return err
	}

	if err := authResp.Error(); err != nil {
		return err
	}

	return c.auth.updateToken(authResp.Data)
}

// shouldRefresh checks if token needs refreshing.
func (ts *tokenStore) shouldRefresh(now time.Time) bool {
	if ts == nil {
		return true
	}

	if ts.token == "" {
		return true
	}
	if ts.claim == nil {
		return true
	}

	return now.Add(2 * time.Minute).After(ts.claim.ExpiresAt.Time)
}

// updateToken validates and updates the token store.
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

// validateToken gets the expiration time from a JWT.
func (ts *tokenStore) validateToken(tokenString string) (*jwtClaim, error) {
	token, err := ts.parser.ParseWithClaims(tokenString, &jwtClaim{}, ts.keyFunc)
	if err != nil {
		return nil, fmt.Errorf("unable to parse token: %w", err)
	}

	if claim, ok := token.Claims.(*jwtClaim); ok {
		return claim, nil
	}

	return nil, fmt.Errorf("token doesn't contain claim details")
}
