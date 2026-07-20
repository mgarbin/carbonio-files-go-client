package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"carbonio-files-go-client/pkg/carbonio"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"
)

// newFakeCarbonioServer starts a TLS server backing /zx/auth/v2/login that
// accepts exactly validUser/validPass and rejects everything else with 401,
// mirroring the real endpoint. It returns the "host:port" to use as
// HTTPAuthenticator.Endpoint.
func newFakeCarbonioServer(t *testing.T, validUser, validPass string) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		http.SetCookie(w, &http.Cookie{Name: "ZM_AUTH_TOKEN", Value: "tok-" + payload.User})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

func openTestApp(t *testing.T, dbPath string) *App {
	t.Helper()
	db, err := sqlitecache.NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	return &App{db: db, auth: &carbonio.HTTPAuthenticator{}}
}

func TestApp_LoginPersistsCredentialsForAutoLogin(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app1 := openTestApp(t, dbPath)
	result := app1.Login(endpoint, user, pass)
	if !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	if got := app1.GetSession(); !got.LoggedIn || got.Username != user || got.Endpoint != endpoint {
		t.Fatalf("GetSession() = %+v, want logged in as %s@%s", got, user, endpoint)
	}
	app1.db.Close()

	// Simulate reopening the app: fresh App/db pointed at the same file.
	app2 := openTestApp(t, dbPath)

	state := app2.Init()
	if !state.AttemptedAutoLogin {
		t.Fatalf("Init() AttemptedAutoLogin = false, want true (credentials were saved)")
	}
	if !state.AutoLogin.Success {
		t.Fatalf("Init() AutoLogin = %+v, want Success=true", state.AutoLogin)
	}
	if got := app2.GetSession(); !got.LoggedIn || got.Username != user {
		t.Fatalf("GetSession() after auto-login = %+v, want logged in as %s", got, user)
	}

	// Logout must clear both the in-memory session and the saved credentials.
	if err := app2.Logout(); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if got := app2.GetSession(); got.LoggedIn {
		t.Fatalf("GetSession() after Logout() = %+v, want LoggedIn=false", got)
	}
	app2.db.Close()

	app3 := openTestApp(t, dbPath)
	defer app3.db.Close()
	state3 := app3.Init()
	if state3.AttemptedAutoLogin {
		t.Fatalf("Init() after Logout() AttemptedAutoLogin = true, want false (no saved credentials)")
	}
}

func TestApp_LoginWithBadCredentialsDoesNotPersist(t *testing.T) {
	endpoint := newFakeCarbonioServer(t, "user@example.com", "correct-password")
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()

	result := app.Login(endpoint, "user@example.com", "wrong-password")
	if result.Success {
		t.Fatalf("Login() with wrong password = %+v, want Success=false", result)
	}
	if result.ErrorKind != string(carbonio.AuthErrorInvalidCredentials) {
		t.Fatalf("Login() ErrorKind = %q, want %q", result.ErrorKind, carbonio.AuthErrorInvalidCredentials)
	}

	cfg, err := app.db.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if cfg != nil {
		t.Fatalf("GetConfig() = %+v, want nil: a failed login must not persist credentials", cfg)
	}

	if got := app.GetSession(); got.LoggedIn {
		t.Fatalf("GetSession() after failed login = %+v, want LoggedIn=false", got)
	}
}

func TestApp_InitWithoutSavedCredentialsShowsLoginScreen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")
	app := openTestApp(t, dbPath)
	defer app.db.Close()

	state := app.Init()
	if state.AttemptedAutoLogin {
		t.Fatalf("Init() on empty store AttemptedAutoLogin = true, want false")
	}
	if state.Locale == "" {
		t.Fatalf("Init() Locale = %q, want a resolved locale", state.Locale)
	}
	if len(state.Translations) == 0 {
		t.Fatalf("Init() Translations is empty, want the loaded catalog")
	}
}
