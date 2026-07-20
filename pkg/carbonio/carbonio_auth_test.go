package carbonio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newAuthTestServer starts a TLS server backing /zx/auth/v2/login with the
// given handler and returns the "host:port" to plug into
// HTTPAuthenticator.Endpoint (which always prepends "https://").
func newAuthTestServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

func TestCarbonioZxAuth_Success(t *testing.T) {
	endpoint := newAuthTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "ZM_AUTH_TOKEN", Value: "tok-123"})
		w.WriteHeader(http.StatusOK)
	})

	auth := &HTTPAuthenticator{Endpoint: endpoint}
	token, err := auth.CarbonioZxAuth("user@example.com", "password")
	if err != nil {
		t.Fatalf("CarbonioZxAuth() error = %v, want nil", err)
	}
	if token == nil || *token != "tok-123" {
		t.Fatalf("CarbonioZxAuth() token = %v, want tok-123", token)
	}
}

func TestCarbonioZxAuth_InvalidEmail(t *testing.T) {
	auth := &HTTPAuthenticator{Endpoint: "unused.invalid"}
	_, err := auth.CarbonioZxAuth("not-an-email", "password")
	assertKind(t, err, AuthErrorInvalidInput)
}

func TestCarbonioZxAuth_StatusClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header func(http.Header)
		want   AuthErrorKind
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: AuthErrorInvalidCredentials},
		{name: "forbidden", status: http.StatusForbidden, want: AuthErrorForbidden},
		{name: "bad request", status: http.StatusBadRequest, want: AuthErrorBadRequest},
		{name: "server error", status: http.StatusInternalServerError, want: AuthErrorServer},
		{name: "teapot (unmapped)", status: http.StatusTeapot, want: AuthErrorUnknown},
		{
			name:   "redirect to change password",
			status: http.StatusFound,
			header: func(h http.Header) {
				h.Set("Location", "https://mail.example.com/changePassword?loginErrorCode=account.CHANGE_PASSWORD")
			},
			want: AuthErrorMustChangePassword,
		},
		{
			name:   "unrelated redirect",
			status: http.StatusFound,
			header: func(h http.Header) {
				h.Set("Location", "https://mail.example.com/somewhere-else")
			},
			want: AuthErrorUnknown,
		},
		{name: "200 without cookie", status: http.StatusOK, want: AuthErrorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := newAuthTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.header != nil {
					tt.header(w.Header())
				}
				w.WriteHeader(tt.status)
			})
			auth := &HTTPAuthenticator{Endpoint: endpoint}
			_, err := auth.CarbonioZxAuth("user@example.com", "password")
			assertKind(t, err, tt.want)
		})
	}
}

func TestCarbonioZxAuth_NetworkError(t *testing.T) {
	// Nothing listens on this endpoint.
	auth := &HTTPAuthenticator{Endpoint: "127.0.0.1:1"}
	_, err := auth.CarbonioZxAuth("user@example.com", "password")
	assertKind(t, err, AuthErrorNetwork)
}

func assertKind(t *testing.T, err error, want AuthErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want kind %q", want)
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v (%T), want *AuthError", err, err)
	}
	if authErr.Kind != want {
		t.Fatalf("error kind = %q, want %q (err: %v)", authErr.Kind, want, err)
	}
}
