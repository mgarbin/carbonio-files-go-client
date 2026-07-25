package carbonio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	sqlitecache "carbonio-files-go-client/pkg/sqlite"
)

// newTokenTestServer starts a TLS server backing both /zx/auth/v2/login
// (accepts exactly validUser/validPass, issuing validToken) and
// /zx/auth/v2/myself (accepts exactly the ZM_AUTH_TOKEN cookie equal to
// validToken while *validRef holds true - flip it to simulate the server
// declaring the token no longer suitable, e.g. expired or revoked). It
// returns the endpoint plus a counter of how many /login requests were
// made, so tests can assert a cached token was reused without a password
// round-trip.
func newTokenTestServer(t *testing.T, validUser, validPass, validToken string, tokenValid *atomic.Bool) (endpoint string, loginCalls *atomic.Int32) {
	t.Helper()
	loginCalls = &atomic.Int32{}

	mux := http.NewServeMux()
	mux.HandleFunc("/zx/auth/v2/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls.Add(1)
		var payload struct {
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.User != validUser || payload.Password != validPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "ZM_AUTH_TOKEN", Value: validToken})
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/zx/auth/v2/myself", func(w http.ResponseWriter, r *http.Request) {
		if !tokenValid.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		cookie, err := r.Cookie("ZM_AUTH_TOKEN")
		if err != nil || cookie.Value != validToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://"), loginCalls
}

func newTestStore(t *testing.T) *sqlitecache.SqliteHelper {
	t.Helper()
	store, err := sqlitecache.NewSqliteHelper(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSession_LoginWithNoCachedTokenPerformsPasswordLoginAndPersists(t *testing.T) {
	const user, pass, token = "user@example.com", "s3cret", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true)
	endpoint, loginCalls := newTokenTestServer(t, user, pass, token, tokenValid)
	store := newTestStore(t)

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, store, user, pass)
	got, err := session.Login()
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got != token {
		t.Fatalf("Login() token = %q, want %q", got, token)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("login endpoint called %d times, want 1", loginCalls.Load())
	}

	cfg, err := store.GetConfig()
	if err != nil || cfg == nil {
		t.Fatalf("GetConfig() = (%v, %v), want a persisted record", cfg, err)
	}
	if cfg.AuthToken != token {
		t.Fatalf("persisted AuthToken = %q, want %q (encrypted at rest, decrypted on read)", cfg.AuthToken, token)
	}
}

func TestSession_LoginReusesValidCachedTokenWithoutPasswordLogin(t *testing.T) {
	const user, pass, token = "user@example.com", "s3cret", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true)
	endpoint, loginCalls := newTokenTestServer(t, user, pass, token, tokenValid)
	store := newTestStore(t)

	if err := store.CreateConfig(sqlitecache.ConfigRecord{
		Endpoint: endpoint, Username: user, Password: pass, AuthToken: token,
	}); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, store, user, pass)
	got, err := session.Login()
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got != token {
		t.Fatalf("Login() token = %q, want the reused cached token %q", got, token)
	}
	if loginCalls.Load() != 0 {
		t.Fatalf("login endpoint called %d times, want 0 (the cached token must be reused, not the password)", loginCalls.Load())
	}
}

func TestSession_LoginReauthenticatesWhenServerRejectsCachedToken(t *testing.T) {
	const user, pass, oldToken, newToken = "user@example.com", "s3cret", "tok-expired", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(false) // the cached token is no longer suitable
	endpoint, loginCalls := newTokenTestServer(t, user, pass, newToken, tokenValid)
	store := newTestStore(t)

	if err := store.CreateConfig(sqlitecache.ConfigRecord{
		Endpoint: endpoint, Username: user, Password: pass, AuthToken: oldToken,
	}); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, store, user, pass)
	got, err := session.Login()
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got != newToken {
		t.Fatalf("Login() token = %q, want the freshly re-authenticated token %q", got, newToken)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("login endpoint called %d times, want 1 (must fall back to password login)", loginCalls.Load())
	}

	cfg, err := store.GetConfig()
	if err != nil || cfg == nil {
		t.Fatalf("GetConfig() = (%v, %v), want a persisted record", cfg, err)
	}
	if cfg.AuthToken != newToken {
		t.Fatalf("persisted AuthToken = %q, want the refreshed token %q", cfg.AuthToken, newToken)
	}
}

// TestSession_LoginReauthenticatesPreservesSyncIntervalAndDeleteRemoteNode
// reproduces closing and reopening the desktop GUI: no cached token
// survives (mirrors LoginReauthenticatesWhenServerRejectsCachedToken), so
// Login falls back to a fresh username/password login and reauthenticate
// persists a new ConfigRecord. Before this test, that record only
// preserved logging settings, the sync folder and the sync on/off
// decision - the Preferences > Synchronization "sync interval" and
// "remote delete mode" (trash vs. permanently delete) choices were
// silently reset to their zero value, which the App layer then reports as
// its built-in default ("trash") instead of the user's last saved choice.
func TestSession_LoginReauthenticatesPreservesSyncIntervalAndDeleteRemoteNode(t *testing.T) {
	const user, pass, oldToken, newToken = "user@example.com", "s3cret", "tok-expired", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(false) // the cached token is no longer suitable
	endpoint, _ := newTokenTestServer(t, user, pass, newToken, tokenValid)
	store := newTestStore(t)

	if err := store.CreateConfig(sqlitecache.ConfigRecord{
		Endpoint:            endpoint,
		Username:            user,
		Password:            pass,
		AuthToken:           oldToken,
		SyncIntervalMinutes: 60,
		DeleteRemoteNode:    "delete",
	}); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, store, user, pass)
	if _, err := session.Login(); err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	cfg, err := store.GetConfig()
	if err != nil || cfg == nil {
		t.Fatalf("GetConfig() = (%v, %v), want a persisted record", cfg, err)
	}
	if cfg.SyncIntervalMinutes != 60 {
		t.Fatalf("persisted SyncIntervalMinutes after reauthenticate = %d, want the preserved 60", cfg.SyncIntervalMinutes)
	}
	if cfg.DeleteRemoteNode != "delete" {
		t.Fatalf("persisted DeleteRemoteNode after reauthenticate = %q, want the preserved %q", cfg.DeleteRemoteNode, "delete")
	}
}

func TestSession_LoginFailsWithWrongPasswordAndDoesNotPersist(t *testing.T) {
	const user, pass, token = "user@example.com", "s3cret", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true)
	endpoint, _ := newTokenTestServer(t, user, pass, token, tokenValid)
	store := newTestStore(t)

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, store, user, "wrong-password")
	if _, err := session.Login(); err == nil {
		t.Fatalf("Login() with wrong password error = nil, want an error")
	}

	if cfg, err := store.GetConfig(); err != nil || cfg != nil {
		t.Fatalf("GetConfig() = (%v, %v), want (nil, nil): a failed login must not persist anything", cfg, err)
	}
}

func TestSession_ReauthenticateAlwaysPerformsPasswordLogin(t *testing.T) {
	const user, pass, token = "user@example.com", "s3cret", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true) // even though the cached token is still valid...
	endpoint, loginCalls := newTokenTestServer(t, user, pass, token, tokenValid)
	store := newTestStore(t)

	if err := store.CreateConfig(sqlitecache.ConfigRecord{
		Endpoint: endpoint, Username: user, Password: pass, AuthToken: token,
	}); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, store, user, pass)
	if _, err := session.Reauthenticate(); err != nil {
		t.Fatalf("Reauthenticate() error = %v", err)
	}
	// ...Reauthenticate must never trust it and always hit the password endpoint.
	if loginCalls.Load() != 1 {
		t.Fatalf("login endpoint called %d times, want 1", loginCalls.Load())
	}
}

func TestSession_LoginWithNilStoreNeverPersists(t *testing.T) {
	const user, pass, token = "user@example.com", "s3cret", "tok-abc"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true)
	endpoint, loginCalls := newTokenTestServer(t, user, pass, token, tokenValid)

	session := NewSession(&HTTPAuthenticator{Endpoint: endpoint}, nil, user, pass)
	got, err := session.Login()
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got != token {
		t.Fatalf("Login() token = %q, want %q", got, token)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("login endpoint called %d times, want 1", loginCalls.Load())
	}
	if session.Token() != token {
		t.Fatalf("Token() = %q, want %q", session.Token(), token)
	}
}
