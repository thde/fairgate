package fairgate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Helper function to generate a test key pair
func generateTestKeyPair(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return privateKey, &privateKey.PublicKey
}

// Helper function to create a test JWT token
func createTestToken(t *testing.T, privateKey *ecdsa.PrivateKey, expiresAt time.Time) string {
	t.Helper()

	claims := jwtClaim{
		FsaID:  "test-fsa-id",
		UniqID: "test-uniq-id",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(signingMethod, claims)
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	return tokenString
}

func TestTokenStore_shouldRefresh(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expiresAt   time.Time
		checkTime   time.Time
		wantRefresh bool
	}{
		{
			name:        "empty token should refresh",
			token:       "",
			expiresAt:   time.Time{},
			checkTime:   time.Now(),
			wantRefresh: true,
		},
		{
			name:        "token expiring in 1 minute should refresh",
			token:       "test-token",
			expiresAt:   time.Now().Add(1 * time.Minute),
			checkTime:   time.Now(),
			wantRefresh: true,
		},
		{
			name:        "token expiring in 2 minutes exactly should refresh",
			token:       "test-token",
			expiresAt:   time.Now().Add(2 * time.Minute),
			checkTime:   time.Now(),
			wantRefresh: true,
		},
		{
			name:        "token expiring in 5 minutes should not refresh",
			token:       "test-token",
			expiresAt:   time.Now().Add(5 * time.Minute),
			checkTime:   time.Now(),
			wantRefresh: false,
		},
		{
			name:        "token expiring in 10 minutes should not refresh",
			token:       "test-token",
			expiresAt:   time.Now().Add(10 * time.Minute),
			checkTime:   time.Now(),
			wantRefresh: false,
		},
		{
			name:        "expired token should refresh",
			token:       "test-token",
			expiresAt:   time.Now().Add(-1 * time.Minute),
			checkTime:   time.Now(),
			wantRefresh: true,
		},
		{
			name:        "nil tokenStore should refresh",
			token:       "",
			expiresAt:   time.Time{},
			checkTime:   time.Now(),
			wantRefresh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &tokenStore{
				token: tt.token,
			}
			if tt.token != "" {
				ts.claim = &jwtClaim{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(tt.expiresAt),
					},
				}
			}

			got := ts.shouldRefresh(tt.checkTime)
			if got != tt.wantRefresh {
				t.Errorf("shouldRefresh() = %v, want %v", got, tt.wantRefresh)
			}
		})
	}
}

func TestTokenStore_shouldRefresh_NilStore(t *testing.T) {
	var ts *tokenStore
	got := ts.shouldRefresh(time.Now())
	if !got {
		t.Error("shouldRefresh() on nil store should return true")
	}
}

func TestTokenStore_validateToken(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)

	tests := []struct {
		name      string
		token     string
		wantErr   bool
		setupFunc func() string
	}{
		{
			name: "valid token",
			setupFunc: func() string {
				return createTestToken(t, privateKey, time.Now().Add(1*time.Hour))
			},
			wantErr: false,
		},
		{
			name: "expired token with leeway",
			setupFunc: func() string {
				// Expired 1 minute ago, but within 2 minute leeway
				return createTestToken(t, privateKey, time.Now().Add(-1*time.Minute))
			},
			wantErr: false,
		},
		{
			name: "expired token beyond leeway",
			setupFunc: func() string {
				// Expired 5 minutes ago, beyond leeway
				return createTestToken(t, privateKey, time.Now().Add(-5*time.Minute))
			},
			wantErr: true,
		},
		{
			name:    "invalid token format",
			token:   "not-a-jwt-token",
			wantErr: true,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "malformed JWT",
			token:   "header.payload",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &tokenStore{
				parser: jwt.NewParser(
					jwt.WithValidMethods([]string{signingMethod.Alg()}),
					jwt.WithLeeway(2*time.Minute),
				),
				keyFunc: func(token *jwt.Token) (any, error) {
					return publicKey, nil
				},
			}

			token := tt.token
			if tt.setupFunc != nil {
				token = tt.setupFunc()
			}

			claim, err := ts.validateToken(token)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && claim == nil {
				t.Error("validateToken() returned nil claim for valid token")
			}

			if !tt.wantErr && claim != nil {
				if claim.FsaID != "test-fsa-id" {
					t.Errorf("validateToken() FsaID = %v, want test-fsa-id", claim.FsaID)
				}
				if claim.UniqID != "test-uniq-id" {
					t.Errorf("validateToken() UniqID = %v, want test-uniq-id", claim.UniqID)
				}
			}
		})
	}
}

func TestTokenStore_validateToken_WrongSigningMethod(t *testing.T) {
	// Create token with wrong signing method (HS256 instead of ES512)
	claims := jwtClaim{
		FsaID:  "test-fsa-id",
		UniqID: "test-uniq-id",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("secret"))

	_, publicKey := generateTestKeyPair(t)

	ts := &tokenStore{
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{signingMethod.Alg()}),
		),
		keyFunc: func(token *jwt.Token) (any, error) {
			return publicKey, nil
		},
	}

	_, err := ts.validateToken(tokenString)
	if err == nil {
		t.Error("validateToken() should reject token with wrong signing method")
	}
}

func TestTokenStore_updateToken(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)

	ts := &tokenStore{
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{signingMethod.Alg()}),
			jwt.WithLeeway(2*time.Minute),
		),
		keyFunc: func(token *jwt.Token) (any, error) {
			return publicKey, nil
		},
	}

	tokenString := createTestToken(t, privateKey, time.Now().Add(1*time.Hour))

	resp := CreateTokenResponse{
		Token:        tokenString,
		RefreshToken: "refresh-token-123",
	}

	err := ts.updateToken(resp)
	if err != nil {
		t.Fatalf("updateToken() error = %v", err)
	}

	if ts.token != tokenString {
		t.Errorf("token not updated: got %v, want %v", ts.token, tokenString)
	}

	if ts.refreshToken != "refresh-token-123" {
		t.Errorf("refreshToken not updated: got %v, want refresh-token-123", ts.refreshToken)
	}

	if ts.claim == nil {
		t.Error("claim not set")
	}

	if ts.claim.FsaID != "test-fsa-id" {
		t.Errorf("claim.FsaID = %v, want test-fsa-id", ts.claim.FsaID)
	}
}

func TestTokenStore_updateToken_InvalidToken(t *testing.T) {
	_, publicKey := generateTestKeyPair(t)

	ts := &tokenStore{
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{signingMethod.Alg()}),
		),
		keyFunc: func(token *jwt.Token) (any, error) {
			return publicKey, nil
		},
	}

	resp := CreateTokenResponse{
		Token:        "invalid-token",
		RefreshToken: "refresh-token-123",
	}

	err := ts.updateToken(resp)
	if err == nil {
		t.Error("updateToken() should return error for invalid token")
	}

	// Token should not be updated on error
	if ts.token != "" {
		t.Errorf("token should not be updated on error, got %v", ts.token)
	}
}

func TestClient_TokenCreate(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)
	tokenString := createTestToken(t, privateKey, time.Now().Add(1*time.Hour))

	tests := []struct {
		name           string
		accessKey      string
		serverResponse Response[CreateTokenResponse]
		statusCode     int
		wantErr        bool
		errContains    string
	}{
		{
			name:      "successful token creation",
			accessKey: "valid-access-key",
			serverResponse: Response[CreateTokenResponse]{
				Code:    200,
				Success: true,
				Data: CreateTokenResponse{
					Token:        tokenString,
					RefreshToken: "refresh-token-123",
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:      "empty access key",
			accessKey: "",
			wantErr:   true,
		},
		{
			name:      "API error response",
			accessKey: "invalid-key",
			serverResponse: Response[CreateTokenResponse]{
				Code:    401,
				Success: false,
				Message: "invalid access key",
			},
			statusCode:  http.StatusOK,
			wantErr:     true,
			errContains: "invalid access key",
		},
		{
			name:       "network error - non-200 status",
			accessKey:  "valid-key",
			statusCode: http.StatusInternalServerError,
			wantErr:    true, // Empty response causes token parsing to fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Verify request method and path
					if r.Method != http.MethodPost {
						t.Errorf("expected POST request, got %s", r.Method)
					}

					if !strings.Contains(r.URL.Path, "/fsa/v1.1/auth/create/") ||
						!strings.HasSuffix(r.URL.Path, "/token") {
						t.Errorf("unexpected path: %s", r.URL.Path)
					}

					// Verify request body
					var req CreateTokenRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Errorf("failed to decode request: %v", err)
					}

					if req.AccessKey != tt.accessKey {
						t.Errorf("AccessKey = %v, want %v", req.AccessKey, tt.accessKey)
					}

					// Send response
					w.WriteHeader(tt.statusCode)
					_ = json.NewEncoder(w).Encode(tt.serverResponse)
				}),
			)
			defer server.Close()

			client := New("test-org", publicKey,
				WithHTTPClient(server.Client()),
				WithBaseURL(mustParseURL(server.URL)),
			)

			err := client.TokenCreate(context.Background(), tt.accessKey)

			if (err != nil) != tt.wantErr {
				t.Errorf("TokenCreate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("TokenCreate() error = %v, should contain %q", err, tt.errContains)
				}
			}

			if !tt.wantErr {
				if client.auth.token == "" {
					t.Error("token should be set after successful creation")
				}
				if client.auth.refreshToken == "" {
					t.Error("refreshToken should be set after successful creation")
				}
			}
		})
	}
}

func TestClient_TokenRefresh(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)
	newTokenString := createTestToken(t, privateKey, time.Now().Add(1*time.Hour))

	tests := []struct {
		name             string
		initialToken     string
		initialRefresh   string
		initialAccessKey string
		expiresAt        time.Time
		serverResponse   Response[CreateTokenResponse]
		wantErr          bool
		errContains      string
		expectCreate     bool // Should call TokenCreate instead
	}{
		{
			name:           "successful token refresh",
			initialToken:   createTestToken(t, privateKey, time.Now().Add(1*time.Minute)),
			initialRefresh: "refresh-token-123",
			expiresAt:      time.Now().Add(1 * time.Minute),
			serverResponse: Response[CreateTokenResponse]{
				Code:    200,
				Success: true,
				Data: CreateTokenResponse{
					Token:        newTokenString,
					RefreshToken: "new-refresh-token",
				},
			},
			wantErr: false,
		},
		{
			name:             "no token - should create",
			initialToken:     "",
			initialRefresh:   "",
			initialAccessKey: "access-key-123",
			expectCreate:     true,
		},
		{
			name:           "no refresh token",
			initialToken:   createTestToken(t, privateKey, time.Now().Add(1*time.Minute)),
			initialRefresh: "",
			expiresAt:      time.Now().Add(1 * time.Minute),
			wantErr:        true,
			errContains:    "no refresh token",
		},
		{
			name:           "API error during refresh",
			initialToken:   createTestToken(t, privateKey, time.Now().Add(1*time.Minute)),
			initialRefresh: "expired-refresh-token",
			expiresAt:      time.Now().Add(1 * time.Minute),
			serverResponse: Response[CreateTokenResponse]{
				Code:    401,
				Success: false,
				Message: "refresh token expired",
			},
			wantErr:     true,
			errContains: "refresh token expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createCalled := false
			refreshCalled := false

			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/auth/create/") {
						createCalled = true
						resp := Response[CreateTokenResponse]{
							Code:    200,
							Success: true,
							Data: CreateTokenResponse{
								Token:        newTokenString,
								RefreshToken: "new-refresh-token",
							},
						}
						_ = json.NewEncoder(w).Encode(resp)
						return
					}

					if strings.Contains(r.URL.Path, "/auth/refresh/") {
						refreshCalled = true

						// Verify request body
						var req RefreshTokenRequest
						_ = json.NewDecoder(r.Body).Decode(&req)

						if req.RefreshToken != tt.initialRefresh {
							t.Errorf(
								"RefreshToken = %v, want %v",
								req.RefreshToken,
								tt.initialRefresh,
							)
						}

						_ = json.NewEncoder(w).Encode(tt.serverResponse)
						return
					}

					t.Errorf("unexpected path: %s", r.URL.Path)
				}),
			)
			defer server.Close()

			client := New("test-org", publicKey,
				WithHTTPClient(server.Client()),
				WithBaseURL(mustParseURL(server.URL)),
				WithAccessKey(tt.initialAccessKey),
			)

			// Set initial token state
			client.auth.token = tt.initialToken
			client.auth.refreshToken = tt.initialRefresh
			if tt.initialToken != "" && !tt.expiresAt.IsZero() {
				client.auth.claim = &jwtClaim{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(tt.expiresAt),
					},
				}
			}

			err := client.TokenRefresh(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("TokenRefresh() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("TokenRefresh() error = %v, should contain %q", err, tt.errContains)
				}
			}

			if tt.expectCreate && !createCalled {
				t.Error("expected TokenCreate to be called")
			}

			if !tt.expectCreate && !tt.wantErr && !refreshCalled {
				t.Error("expected refresh endpoint to be called")
			}
		})
	}
}

func TestClient_TokenRefresh_NoRefreshNeeded(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)

	// Token that expires in 10 minutes (no refresh needed)
	tokenString := createTestToken(t, privateKey, time.Now().Add(10*time.Minute))

	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		t.Error("server should not be called when token doesn't need refresh")
	}))
	defer server.Close()

	client := New("test-org", publicKey,
		WithHTTPClient(server.Client()),
		WithBaseURL(mustParseURL(server.URL)),
	)

	// Manually set token state
	client.auth.token = tokenString
	client.auth.refreshToken = "refresh-token-123"
	client.auth.claim = &jwtClaim{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
	}

	err := client.TokenRefresh(context.Background())
	if err != nil {
		t.Errorf("TokenRefresh() error = %v, want nil", err)
	}

	if serverCalled {
		t.Error("server should not be called when token is still valid")
	}
}

func TestClient_TokenRefresh_ConcurrentCalls(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)
	newTokenString := createTestToken(t, privateKey, time.Now().Add(1*time.Hour))

	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		time.Sleep(50 * time.Millisecond) // Simulate network delay
		resp := Response[CreateTokenResponse]{
			Code:    200,
			Success: true,
			Data: CreateTokenResponse{
				Token:        newTokenString,
				RefreshToken: "new-refresh-token",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New("test-org", publicKey,
		WithHTTPClient(server.Client()),
		WithBaseURL(mustParseURL(server.URL)),
	)

	// Set token that needs refresh
	client.auth.token = createTestToken(t, privateKey, time.Now().Add(1*time.Minute))
	client.auth.refreshToken = "refresh-token-123"
	client.auth.claim = &jwtClaim{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
		},
	}

	// Call TokenRefresh concurrently
	done := make(chan error, 5)
	for range 5 {
		go func() {
			done <- client.TokenRefresh(context.Background())
		}()
	}

	// Wait for all to complete
	for range 5 {
		if err := <-done; err != nil {
			t.Errorf("TokenRefresh() error = %v", err)
		}
	}

	// Due to locking, all calls should succeed
	// The exact number of server calls depends on timing
	if callCount == 0 {
		t.Error("server should have been called at least once")
	}
}

// Helper function to parse URL (panics on error for test setup)
func mustParseURL(urlStr string) *url.URL {
	u, err := url.Parse(urlStr)
	if err != nil {
		panic(err)
	}
	return u
}
