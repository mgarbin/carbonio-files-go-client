package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newTokenSwitchingServer starts a self-signed TLS server that rejects
// every request whose ZM_AUTH_TOKEN cookie isn't currentToken with a bare
// HTTP 401 - the exact shape carbonio-auth returns for an expired/invalid
// ZM_AUTH_TOKEN (see AuthorizedApiHandler) - and answers matching requests
// with body. requestCount tallies every request the server receives,
// regardless of outcome.
func newTokenSwitchingServer(t *testing.T, currentToken *atomic.Value, body map[string]any) (endpoint string, requestCount *atomic.Int32) {
	t.Helper()
	requestCount = &atomic.Int32{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		cookie, _ := r.Cookie("ZM_AUTH_TOKEN")
		var got string
		if cookie != nil {
			got = cookie.Value
		}
		if got != currentToken.Load().(string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": nil,
				"errors": []map[string]any{
					{"message": "Failed to authenticate request /graphql: Unable to find requested user"},
				},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://"), requestCount
}

// TestGetAllNode_ReauthenticatesAndRetriesAfter401 covers the fix for the
// production bug this test is named after: ZM_AUTH_TOKEN expires
// (commonly after 8 hours) and every GraphQL call starts failing with a
// bare 401 ("Unable to find requested user") until the process is
// restarted. GetAllNode must now react to that 401 by calling
// TokenRefresher, updating AuthToken, and transparently retrying the same
// request with the fresh token - never surfacing the 401 to the caller
// when a refresh succeeds.
func TestGetAllNode_ReauthenticatesAndRetriesAfter401(t *testing.T) {
	current := &atomic.Value{}
	current.Store("new-token") // the server has already invalidated "old-token"

	endpoint, requests := newTokenSwitchingServer(t, current, map[string]any{
		"data": map[string]any{"getNode": nil},
	})
	var refreshCalls int
	a := &GraphQLAuthenticator{
		Endpoint:  endpoint,
		AuthToken: "old-token",
		TokenRefresher: func() (string, error) {
			refreshCalls++
			current.Store("new-token")
			return "new-token", nil
		},
	}

	nodes, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil)
	if err != nil {
		t.Fatalf("GetAllNode() error = %v, want nil (401 must be transparently retried)", err)
	}
	if nodes != nil {
		t.Fatalf("GetAllNode() nodes = %v, want nil", nodes)
	}
	if refreshCalls != 1 {
		t.Fatalf("TokenRefresher called %d times, want 1", refreshCalls)
	}
	if requests.Load() != 2 {
		t.Fatalf("server received %d requests, want 2 (initial 401 + retry)", requests.Load())
	}
	if a.AuthToken != "new-token" {
		t.Fatalf("AuthToken = %q, want %q (must be updated for subsequent calls)", a.AuthToken, "new-token")
	}
}

// TestGetAllNode_NoTokenRefresherReturns401 locks in the pre-existing
// behavior when TokenRefresher isn't configured: the 401 is returned as an
// error, with exactly one request made, never retried.
func TestGetAllNode_NoTokenRefresherReturns401(t *testing.T) {
	current := &atomic.Value{}
	current.Store("only-valid-token")

	endpoint, requests := newTokenSwitchingServer(t, current, map[string]any{
		"data": map[string]any{"getNode": nil},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "stale-token"}

	if _, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil); err == nil {
		t.Fatalf("GetAllNode() error = nil, want non-nil (401 with no TokenRefresher)")
	}
	if requests.Load() != 1 {
		t.Fatalf("server received %d requests, want 1 (no retry without TokenRefresher)", requests.Load())
	}
}

// TestGetAllNode_RefreshFailureReturnsOriginalError covers a refresh
// attempt that itself fails (e.g. the stored username/password no longer
// authenticate either): the original 401 must be returned to the caller
// rather than the refresh error, and the request must never be retried
// with a token that was never obtained.
func TestGetAllNode_RefreshFailureReturnsOriginalError(t *testing.T) {
	current := &atomic.Value{}
	current.Store("only-valid-token")

	endpoint, requests := newTokenSwitchingServer(t, current, map[string]any{
		"data": map[string]any{"getNode": nil},
	})

	refreshErr := &authFailure{"refresh denied"}
	a := &GraphQLAuthenticator{
		Endpoint:  endpoint,
		AuthToken: "stale-token",
		TokenRefresher: func() (string, error) {
			return "", refreshErr
		},
	}

	_, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil)
	if err == nil {
		t.Fatalf("GetAllNode() error = nil, want the original 401 error")
	}
	if strings.Contains(err.Error(), refreshErr.Error()) {
		t.Fatalf("GetAllNode() error = %v, want the original 401 error, not the refresh error", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("server received %d requests, want 1 (refresh failure must not trigger a retry)", requests.Load())
	}
}

// authFailure is a minimal error type distinguishable from the 401
// HTTPError GetAllNode itself returns.
type authFailure struct{ msg string }

func (e *authFailure) Error() string { return e.msg }
