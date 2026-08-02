package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newFlakyGraphQLServer starts a self-signed TLS server that answers
// statusCode (a malformed-JSON error body, forcing genqlient's MakeRequest
// to build an *HTTPError) for the first failUntilAttempt requests, or every
// request when failUntilAttempt is 0, then answers body as a normal 200
// GraphQL response for every request after that. requestCount tallies
// every request received, regardless of outcome.
func newFlakyGraphQLServer(t *testing.T, statusCode, failUntilAttempt int, body map[string]any) (endpoint string, requestCount *atomic.Int32) {
	t.Helper()
	requestCount = &atomic.Int32{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requestCount.Add(1)
		if failUntilAttempt == 0 || int(attempt) <= failUntilAttempt {
			http.Error(w, "flaky", statusCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://"), requestCount
}

// TestGetAllNode_RetriesTransientErrorThenSucceeds covers withTransientRetry:
// a 503 is a transient, worth-retrying error (see isTransient), so
// GetAllNode must survive it as long as a later attempt within
// maxTransientAttempts succeeds - this is what lets a large recursive scan
// (utils.RecursiveListNodeItems) absorb a brief server hiccup on one
// folder instead of failing outright.
func TestGetAllNode_RetriesTransientErrorThenSucceeds(t *testing.T) {
	endpoint, requestCount := newFlakyGraphQLServer(t, http.StatusServiceUnavailable, maxTransientAttempts-1, map[string]any{
		"data": map[string]any{"getNode": nil},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	_, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil)
	if err != nil {
		t.Fatalf("GetAllNode() error = %v, want nil (must succeed once retries reach the final, healthy attempt)", err)
	}
	if got := requestCount.Load(); got != int32(maxTransientAttempts) {
		t.Fatalf("request count = %d, want %d (one failing attempt per retry, then the success)", got, maxTransientAttempts)
	}
}

// TestGetAllNode_GivesUpAfterExhaustingTransientRetries covers the other
// side: a subtree that keeps failing must not retry forever - exactly
// maxTransientAttempts requests are made, then the last error is returned
// for the caller (utils.RecursiveListNodeItems) to record as a failed
// subtree instead of hanging indefinitely.
func TestGetAllNode_GivesUpAfterExhaustingTransientRetries(t *testing.T) {
	endpoint, requestCount := newFlakyGraphQLServer(t, http.StatusServiceUnavailable, 0, nil)

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	_, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil)
	if err == nil {
		t.Fatalf("GetAllNode() error = nil, want non-nil (every attempt failed)")
	}
	if got := requestCount.Load(); got != int32(maxTransientAttempts) {
		t.Fatalf("request count = %d, want %d (must give up after exactly maxTransientAttempts)", got, maxTransientAttempts)
	}
}

// TestGetAllNode_DoesNotRetryPermanentClientError covers isTransient's other
// branch: a 400 (bad request - malformed query/input, not the 401
// executeWithReauth already handles) is a permanent problem retrying can't
// fix, so GetAllNode must fail after exactly one request instead of
// burning maxTransientAttempts (and their backoff delays) on an identical,
// doomed retry.
func TestGetAllNode_DoesNotRetryPermanentClientError(t *testing.T) {
	endpoint, requestCount := newFlakyGraphQLServer(t, http.StatusBadRequest, 0, nil)

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	_, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil)
	if err == nil {
		t.Fatalf("GetAllNode() error = nil, want non-nil")
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1 (a permanent error must not be retried)", got)
	}
}
