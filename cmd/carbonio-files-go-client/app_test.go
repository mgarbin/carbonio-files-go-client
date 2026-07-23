package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/logger"
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

func TestApp_SyncFolderSetupWizard(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()

	// Before any login/config exists, no sync folder is configured.
	if got := app.GetSyncFolder(); got.Path != "" {
		t.Fatalf("GetSyncFolder() before login = %+v, want empty Path", got)
	}

	// First login must ask the frontend to run the setup wizard.
	result := app.Login(endpoint, user, pass)
	if !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	if !result.NeedsSyncSetup {
		t.Fatalf("Login() (first login) NeedsSyncSetup = false, want true")
	}

	// SetSyncFolder requires an existing config row: it must fail before
	// login has created one, and succeed once the session exists.
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}
	if _, err := os.Stat(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() did not create the folder: %v", err)
	}

	got := app.GetSyncFolder()
	if got.Path != syncDir {
		t.Fatalf("GetSyncFolder() after SetSyncFolder() = %+v, want Path=%q", got, syncDir)
	}

	cfg, err := app.db.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if cfg == nil || cfg.FilesLocalFolder != syncDir {
		t.Fatalf("GetConfig() FilesLocalFolder = %+v, want %q", cfg, syncDir)
	}

	// A subsequent login (e.g. after the process restarts) must not ask
	// for the wizard again: the sync folder is already configured and must
	// survive re-authentication (login() must preserve it).
	result2 := app.Login(endpoint, user, pass)
	if !result2.Success {
		t.Fatalf("Login() (second login) = %+v, want Success=true", result2)
	}
	if result2.NeedsSyncSetup {
		t.Fatalf("Login() (second login) NeedsSyncSetup = true, want false: sync folder was already configured")
	}
	if got := app.GetSyncFolder(); got.Path != syncDir {
		t.Fatalf("GetSyncFolder() after second login = %+v, want Path=%q (must survive re-login)", got, syncDir)
	}
}

func TestApp_SetSyncFolderRequiresPriorLogin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")
	app := openTestApp(t, dbPath)
	defer app.db.Close()

	if err := app.SetSyncFolder(filepath.Join(t.TempDir(), "sync")); err == nil {
		t.Fatalf("SetSyncFolder() before any login = nil error, want an error")
	}
	if err := app.SetSyncFolder(""); err == nil {
		t.Fatalf("SetSyncFolder(\"\") = nil error, want an error")
	}
}

// TestStartingDirectoryAlwaysExists guards against the bug where
// ChooseSyncFolder passed a suggested-but-not-yet-created path (e.g.
// "~/Carbonio Files") as OpenDialogOptions.DefaultDirectory: Wails'
// OpenDirectoryDialog hard-errors with "default directory ... does not
// exist" before it even opens the native picker, which surfaced to the
// user as a generic error on the very first folder pick. startingDirectory
// must only ever return "" or a path that is verifiably a directory.
func TestStartingDirectoryAlwaysExists(t *testing.T) {
	dir := startingDirectory()
	if dir == "" {
		t.Skip("no home directory resolvable in this environment")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("startingDirectory() = %q, which does not exist: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("startingDirectory() = %q, which is not a directory", dir)
	}
}

func TestLogFileName(t *testing.T) {
	defaultName := filepath.Base(logger.DefaultPath)
	cases := []struct {
		name        string
		currentPath string
		want        string
	}{
		{"empty falls back to default", "", defaultName},
		{"keeps existing file name", "/var/log/carbonio.log", "carbonio.log"},
		{"relative path keeps file name", "logs/app.log", "app.log"},
		{"root path falls back to default", "/", defaultName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := logFileName(tc.currentPath); got != tc.want {
				t.Fatalf("logFileName(%q) = %q, want %q", tc.currentPath, got, tc.want)
			}
		})
	}
}

// TestLogFolderStartDir guards the same class of bug as
// TestStartingDirectoryAlwaysExists: whatever it returns is fed straight
// into OpenDialogOptions.DefaultDirectory, which hard-errors the picker if
// the directory doesn't exist.
func TestLogFolderStartDir(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "carbonio.log")

	if got := logFolderStartDir(logFile); got != tmp {
		t.Fatalf("logFolderStartDir(%q) = %q, want existing parent dir %q", logFile, got, tmp)
	}

	// A currentPath whose parent directory does NOT exist must never be
	// returned as-is: fall back to a directory known to exist.
	missing := filepath.Join(tmp, "does-not-exist", "carbonio.log")
	got := logFolderStartDir(missing)
	if got == filepath.Dir(missing) {
		t.Fatalf("logFolderStartDir(%q) = %q, want fallback (parent does not exist)", missing, got)
	}
	if got != "" {
		if info, err := os.Stat(got); err != nil || !info.IsDir() {
			t.Fatalf("logFolderStartDir(%q) = %q, which is not an existing directory", missing, got)
		}
	}
}

// TestApp_TestLoginDoesNotPersistOrTouchSession backs the Authentication
// preferences panel's "Test connection" button: it must verify credentials
// against the server without saving them or disturbing whatever session is
// already active, so Save (a real Login call) remains the only thing that
// actually commits a credential change.
func TestApp_TestLoginDoesNotPersistOrTouchSession(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()

	// Establish a real, persisted session first.
	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	originalSession := app.GetSession()

	// A successful test with different-but-valid-looking credentials must
	// not persist anything or change the active session.
	testResult := app.TestLogin(endpoint, user, pass)
	if !testResult.Success {
		t.Fatalf("TestLogin() = %+v, want Success=true", testResult)
	}
	if got := app.GetSession(); got != originalSession {
		t.Fatalf("GetSession() after TestLogin() = %+v, want unchanged %+v", got, originalSession)
	}

	// A failing test must classify the error exactly like Login() does, and
	// still touch neither the session nor the persisted config.
	badResult := app.TestLogin(endpoint, user, "wrong-password")
	if badResult.Success {
		t.Fatalf("TestLogin() with wrong password = %+v, want Success=false", badResult)
	}
	if badResult.ErrorKind != string(carbonio.AuthErrorInvalidCredentials) {
		t.Fatalf("TestLogin() ErrorKind = %q, want %q", badResult.ErrorKind, carbonio.AuthErrorInvalidCredentials)
	}
	if got := app.GetSession(); got != originalSession {
		t.Fatalf("GetSession() after failing TestLogin() = %+v, want unchanged %+v", got, originalSession)
	}

	cfg, err := app.db.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if cfg == nil || cfg.Password != pass {
		t.Fatalf("GetConfig() = %+v, want the originally saved password untouched by TestLogin()", cfg)
	}
}

// newTokenAwareFakeCarbonioServer extends newFakeCarbonioServer with a
// /zx/auth/v2/myself endpoint: it accepts validToken as the ZM_AUTH_TOKEN
// cookie while *tokenValid holds true, and rejects every token (mirroring
// the server declaring it no longer suitable) once flipped to false. It
// also returns a counter of /login requests so tests can assert whether a
// cached token was reused instead of a password round-trip.
func newTokenAwareFakeCarbonioServer(t *testing.T, validUser, validPass, validToken string, tokenValid *atomic.Bool) (endpoint string, loginCalls *atomic.Int32) {
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
		cookie, err := r.Cookie("ZM_AUTH_TOKEN")
		if !tokenValid.Load() || err != nil || cookie.Value != validToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://"), loginCalls
}

// TestApp_InitReusesCachedTokenWithoutPasswordLogin verifies the auto-login
// path added on top of TestApp_LoginPersistsCredentialsForAutoLogin: once a
// token has been cached, restarting the app and calling Init() must sign
// the user back in by reusing that token - validated against
// /zx/auth/v2/myself - without ever POSTing the password to /login again.
func TestApp_InitReusesCachedTokenWithoutPasswordLogin(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true)
	endpoint, loginCalls := newTokenAwareFakeCarbonioServer(t, user, pass, "tok-cached", tokenValid)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app1 := openTestApp(t, dbPath)
	if result := app1.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	app1.db.Close()
	if got := loginCalls.Load(); got != 1 {
		t.Fatalf("login endpoint called %d times after first Login(), want 1", got)
	}

	// Simulate reopening the app: fresh App/db pointed at the same file.
	app2 := openTestApp(t, dbPath)
	defer app2.db.Close()
	state := app2.Init()
	if !state.AutoLogin.Success {
		t.Fatalf("Init() AutoLogin = %+v, want Success=true", state.AutoLogin)
	}
	if got := app2.GetSession(); !got.LoggedIn || got.Username != user {
		t.Fatalf("GetSession() after auto-login = %+v, want logged in as %s", got, user)
	}
	if got := loginCalls.Load(); got != 1 {
		t.Fatalf("login endpoint called %d times after Init(), want still 1 (the cached token must be reused, not the password)", got)
	}
}

// TestApp_InitReauthenticatesWhenServerRejectsCachedToken covers the other
// half of the policy: once the server stops accepting the cached token
// (expired, revoked, ...), Init() must transparently fall back to a full
// username/password login and persist the refreshed token.
func TestApp_InitReauthenticatesWhenServerRejectsCachedToken(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	tokenValid := &atomic.Bool{}
	tokenValid.Store(true)
	endpoint, loginCalls := newTokenAwareFakeCarbonioServer(t, user, pass, "tok-cached", tokenValid)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app1 := openTestApp(t, dbPath)
	if result := app1.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	app1.db.Close()

	// The server now declares the cached token no longer suitable.
	tokenValid.Store(false)

	app2 := openTestApp(t, dbPath)
	defer app2.db.Close()
	state := app2.Init()
	if !state.AutoLogin.Success {
		t.Fatalf("Init() AutoLogin = %+v, want Success=true (must re-authenticate automatically)", state.AutoLogin)
	}
	if got := loginCalls.Load(); got != 2 {
		t.Fatalf("login endpoint called %d times, want 2 (initial Login() plus the automatic re-login)", got)
	}
}
