package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"carbonio-files-go-client/pkg/appdir"
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
	// Login() now starts the background sync job once a folder is
	// configured (see maybeStartBackgroundSync): stop it so the test
	// doesn't leak a running goroutine past t's lifetime.
	defer app.stopBackgroundSync()

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

// TestApp_StartFullSyncRequiresPriorLoginAndFolder backs the dashboard's
// "Avvia Sincronizzazione" button: it must refuse to run before the user
// is logged in, and before a sync folder has been configured, without
// ever reaching the network (both checks return before actions.FullCacheSync
// is called).
func TestApp_StartFullSyncRequiresPriorLoginAndFolder(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")
	app := openTestApp(t, dbPath)
	defer app.db.Close()

	if err := app.StartFullSync(); err == nil {
		t.Fatalf("StartFullSync() before login = nil error, want an error")
	}

	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}

	if err := app.StartFullSync(); err == nil {
		t.Fatalf("StartFullSync() before a sync folder is configured = nil error, want an error")
	}
}

// TestApp_BackgroundSyncJobLifecycle covers maybeStartBackgroundSync /
// stopBackgroundSync's gating and idempotency: the periodic job must only
// start once a session, a sync folder, and the persisted SyncEnabled
// preference are all available, starting it again while already running
// must not spawn a second loop, and Logout must stop it. It never waits
// for a tick (backgroundSyncInterval is 5 minutes): only the start/stop
// wiring around App.syncJobCancel is under test.
func TestApp_BackgroundSyncJobLifecycle(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	// No session yet: must not start.
	app.maybeStartBackgroundSync()
	if app.syncJobCancel != nil {
		t.Fatalf("maybeStartBackgroundSync() started the job without a session")
	}

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	// Login() calls maybeStartBackgroundSync() itself, but no sync folder
	// is configured yet: it must still no-op.
	if app.syncJobCancel != nil {
		t.Fatalf("Login() started the background job before a sync folder was configured")
	}

	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}
	// A folder is now configured, but sync was never enabled: it must
	// still no-op (default is disabled - see ConfigRecord.SyncEnabled).
	app.maybeStartBackgroundSync()
	if app.syncJobCancel != nil {
		t.Fatalf("maybeStartBackgroundSync() started the job while SyncEnabled was still false")
	}

	// Enabling it persists the preference and must start the job.
	if err := app.SetSyncEnabled(true); err != nil {
		t.Fatalf("SetSyncEnabled(true) error = %v", err)
	}
	if app.syncJobCancel == nil {
		t.Fatalf("SetSyncEnabled(true) did not start the background job")
	}

	// Idempotent: calling it again while already running must not replace
	// the running job (no second goroutine/ticker spawned).
	firstCancel := reflect.ValueOf(app.syncJobCancel).Pointer()
	app.maybeStartBackgroundSync()
	if reflect.ValueOf(app.syncJobCancel).Pointer() != firstCancel {
		t.Fatalf("maybeStartBackgroundSync() replaced the already-running job")
	}

	app.stopBackgroundSync()
	if app.syncJobCancel != nil {
		t.Fatalf("stopBackgroundSync() did not clear syncJobCancel")
	}
	// Idempotent: stopping an already-stopped job must not panic.
	app.stopBackgroundSync()

	// Logout must also stop it.
	app.maybeStartBackgroundSync()
	if app.syncJobCancel == nil {
		t.Fatalf("maybeStartBackgroundSync() did not restart the job")
	}
	if err := app.Logout(); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if app.syncJobCancel != nil {
		t.Fatalf("Logout() did not stop the background sync job")
	}
}

// TestApp_GetSyncStatusReportsInProgress verifies GetSyncStatus surfaces
// the syncing flag as InProgress - this is what lets the dashboard show
// "In corso" while a sync (StartFullSync, the initial scan, or a
// background cycle) is actively running, independent of whatever the
// cache DB currently contains.
func TestApp_GetSyncStatusReportsInProgress(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")
	app := openTestApp(t, dbPath)
	defer app.db.Close()

	status, err := app.GetSyncStatus()
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}
	if status.InProgress {
		t.Fatalf("GetSyncStatus().InProgress = true before any sync ran, want false")
	}

	app.syncing.Store(true)
	status, err = app.GetSyncStatus()
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}
	if !status.InProgress {
		t.Fatalf("GetSyncStatus().InProgress = false while syncing, want true")
	}

	app.syncing.Store(false)
	status, err = app.GetSyncStatus()
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}
	if status.InProgress {
		t.Fatalf("GetSyncStatus().InProgress = true after sync finished, want false")
	}
}

// TestApp_TryBeginSyncEnforcesMutualExclusion covers tryBeginSync/endSync
// in isolation: only one sync may hold the lock at a time, and it becomes
// available again once released.
func TestApp_TryBeginSyncEnforcesMutualExclusion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")
	app := openTestApp(t, dbPath)
	defer app.db.Close()

	if !app.tryBeginSync() {
		t.Fatalf("tryBeginSync() = false on first call, want true")
	}
	if app.tryBeginSync() {
		t.Fatalf("tryBeginSync() = true while a sync is already in progress, want false")
	}
	if !app.syncing.Load() {
		t.Fatalf("syncing = false while a sync is in progress, want true")
	}

	app.endSync()
	if app.syncing.Load() {
		t.Fatalf("syncing = true after endSync(), want false")
	}
	if !app.tryBeginSync() {
		t.Fatalf("tryBeginSync() = false after endSync(), want true")
	}
	app.endSync()
}

// TestApp_StartFullSyncRejectedWhileAnotherSyncRuns verifies StartFullSync
// refuses to run - without attempting anything - while another sync
// (manual or background) already holds the lock: UpdateCacheSync and
// LiveCacheSync must never run concurrently.
func TestApp_StartFullSyncRejectedWhileAnotherSyncRuns(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}

	// Simulate another sync operation already running.
	if !app.tryBeginSync() {
		t.Fatalf("tryBeginSync() = false, want true (setup)")
	}
	err := app.StartFullSync()
	app.endSync()
	if err == nil {
		t.Fatalf("StartFullSync() while another sync is running = nil error, want an error")
	}
}

// TestApp_BackgroundSyncCycleSkipsWhileAnotherSyncRuns verifies the
// periodic background job's cycle bails out immediately - without
// acquiring/releasing the sync lock - when another sync is already in
// progress, instead of running UpdateCacheSync/LiveCacheSync concurrently
// with it.
func TestApp_BackgroundSyncCycleSkipsWhileAnotherSyncRuns(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}

	if !app.tryBeginSync() {
		t.Fatalf("tryBeginSync() = false, want true (setup)")
	}

	done := make(chan struct{})
	go func() {
		app.runBackgroundSyncCycle()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("runBackgroundSyncCycle() did not return promptly while another sync was in progress")
	}

	// runBackgroundSyncCycle must never have acquired the lock: it would
	// only call endSync() after a successful tryBeginSync, i.e. only if it
	// had wrongly proceeded despite the lock being held elsewhere.
	if app.syncMu.TryLock() {
		app.syncMu.Unlock()
		t.Fatalf("runBackgroundSyncCycle() altered the sync lock while it was held elsewhere")
	}

	app.endSync()
}

// TestApp_SetSyncEnabledDoesNotRaceExplicitStartFullSync guards against a
// regression of the dashboard's "Avvia Sincronizzazione" bug: the toggle
// (DashboardHome.svelte's toggleSync) persists the on/off decision via
// SetSyncEnabled(true) and then immediately calls StartFullSync() itself
// for instant feedback. SetSyncEnabled(true) must start the background
// job (see startBackgroundSyncJob) WITHOUT running its first cycle
// immediately, or that cycle grabs the sync lock (tryBeginSync) before the
// explicit StartFullSync call lands, which then fails with "a sync is
// already in progress" - surfaced to the user as the dashboard's generic
// "Si è verificato un errore" message.
func TestApp_SetSyncEnabledDoesNotRaceExplicitStartFullSync(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}

	if err := app.SetSyncEnabled(true); err != nil {
		t.Fatalf("SetSyncEnabled(true) error = %v", err)
	}
	if app.syncJobCancel == nil {
		t.Fatalf("SetSyncEnabled(true) did not start the background job")
	}
	// The background job must not have grabbed the sync lock for an
	// immediate first cycle: that's now deferred to its first periodic
	// tick, precisely so it never contends with the toggle's own explicit
	// StartFullSync call below.
	if !app.syncMu.TryLock() {
		t.Fatalf("SetSyncEnabled(true) left the sync lock held immediately, want it free")
	}
	app.syncMu.Unlock()

	if err := app.StartFullSync(); err != nil && err.Error() == "a sync is already in progress" {
		t.Fatalf("StartFullSync() right after SetSyncEnabled(true) = %v, want it to never race the background job for the lock", err)
	}
}

// TestApp_ResetSyncStopsAndClearsCache verifies ResetSync - the dashboard's
// "Reset sync" confirmation dialog's Accept action - stops the sync
// process (persists SyncEnabled=false and cancels the background job, see
// SetSyncEnabled(false)) and permanently deletes every cached sync record,
// including the last sync date (SyncStatus.LastSyncedAt), so the
// dashboard reports "never synced" again afterwards.
func TestApp_ResetSyncStopsAndClearsCache(t *testing.T) {
	// appdir.Path("file_sync_cache.db") resolves under $HOME: redirect it
	// to a throwaway directory so this test can freely populate and wipe
	// the cache DB without touching a real developer/CI cache.
	t.Setenv("HOME", t.TempDir())

	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}
	if err := app.SetSyncEnabled(true); err != nil {
		t.Fatalf("SetSyncEnabled(true) error = %v", err)
	}
	if app.syncJobCancel == nil {
		t.Fatalf("SetSyncEnabled(true) did not start the background job (setup)")
	}

	// Seed the cache DB with a fake filesync record and a recorded run, as
	// if a previous UpdateCacheSync had already populated it.
	cacheDb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	if _, err := cacheDb.InsertFileSync(
		"node-1", "parent-1", "/remote/a.txt", "rhash", "/local/a.txt", "lhash",
		false, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z", 10, 10,
		"rdigest", "ldigest", "in_sync", "2024-01-01T00:00:00Z", 0, 0,
	); err != nil {
		t.Fatalf("InsertFileSync() error = %v", err)
	}
	if err := cacheDb.SetSyncRunResult("2024-01-01T00:00:00Z", ""); err != nil {
		t.Fatalf("SetSyncRunResult() error = %v", err)
	}
	if err := cacheDb.Close(); err != nil {
		t.Fatalf("cacheDb.Close() error = %v", err)
	}

	if err := app.ResetSync(); err != nil {
		t.Fatalf("ResetSync() error = %v", err)
	}

	if app.GetSyncEnabled() {
		t.Fatalf("GetSyncEnabled() = true after ResetSync(), want false")
	}
	if app.syncJobCancel != nil {
		t.Fatalf("ResetSync() left the background sync job running, want it stopped")
	}

	status, err := app.GetSyncStatus()
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}
	if status.LastSyncedAt != "" {
		t.Fatalf("GetSyncStatus().LastSyncedAt = %q after ResetSync(), want empty (never synced)", status.LastSyncedAt)
	}
	if status.RemoteItems != 0 || status.LocalItems != 0 {
		t.Fatalf("GetSyncStatus() = %+v after ResetSync(), want every cached count back to zero", status)
	}

	verifyDb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	defer verifyDb.Close()
	if count, err := verifyDb.CountRecords(); err != nil {
		t.Fatalf("CountRecords() error = %v", err)
	} else if count != 0 {
		t.Fatalf("CountRecords() = %d after ResetSync(), want 0", count)
	}
	if meta, err := verifyDb.GetSyncMeta(); err != nil {
		t.Fatalf("GetSyncMeta() error = %v", err)
	} else if meta != nil {
		t.Fatalf("GetSyncMeta() = %+v after ResetSync(), want nil (never synced)", meta)
	}
}

// TestApp_ResetSyncRejectedWhileAnotherSyncRuns verifies ResetSync refuses
// to touch the cache DB while another sync (manual or background) already
// holds the sync lock - clearing filesync rows out from under an
// in-progress UpdateCacheSync/LiveCacheSync write would corrupt it. The
// dashboard's dialog surfaces this as its generic error banner, leaving
// the cached data intact for a retry.
func TestApp_ResetSyncRejectedWhileAnotherSyncRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}

	// Simulate another sync operation already running.
	if !app.tryBeginSync() {
		t.Fatalf("tryBeginSync() = false, want true (setup)")
	}
	err := app.ResetSync()
	app.endSync()
	if err == nil {
		t.Fatalf("ResetSync() while another sync is running = nil error, want an error")
	}
}

// TestApp_SyncEnabledPersistsAcrossRestart verifies the dashboard's sync
// on/off decision (SetSyncEnabled) survives an app restart and that the
// background sync job resumes/stays off automatically to match - not just
// that GetSyncEnabled() reports the right value, but that
// maybeStartBackgroundSync (called from Init -> autoLogin) actually acts
// on it.
func TestApp_SyncEnabledPersistsAcrossRestart(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")

	app1 := openTestApp(t, dbPath)
	if result := app1.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	if err := app1.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}
	if err := app1.SetSyncEnabled(true); err != nil {
		t.Fatalf("SetSyncEnabled(true) error = %v", err)
	}
	if app1.syncJobCancel == nil {
		t.Fatalf("SetSyncEnabled(true) did not start the background job")
	}
	app1.stopBackgroundSync()
	app1.db.Close()

	// Simulate reopening the app: fresh App/db pointed at the same file.
	app2 := openTestApp(t, dbPath)
	defer app2.db.Close()
	defer app2.stopBackgroundSync()

	state := app2.Init()
	if !state.AutoLogin.Success {
		t.Fatalf("Init() AutoLogin = %+v, want Success=true", state.AutoLogin)
	}
	if !app2.GetSyncEnabled() {
		t.Fatalf("GetSyncEnabled() after restart = false, want true (persisted decision)")
	}
	if app2.syncJobCancel == nil {
		t.Fatalf("background sync job did not resume automatically after restart with SyncEnabled=true")
	}
	app2.stopBackgroundSync()

	// Now disable it and verify the opposite: after another restart, it
	// must stay off.
	if err := app2.SetSyncEnabled(false); err != nil {
		t.Fatalf("SetSyncEnabled(false) error = %v", err)
	}
	if app2.syncJobCancel != nil {
		t.Fatalf("SetSyncEnabled(false) did not stop the background job")
	}
	app2.db.Close()

	app3 := openTestApp(t, dbPath)
	defer app3.db.Close()
	defer app3.stopBackgroundSync()
	state3 := app3.Init()
	if !state3.AutoLogin.Success {
		t.Fatalf("Init() AutoLogin = %+v, want Success=true", state3.AutoLogin)
	}
	if app3.GetSyncEnabled() {
		t.Fatalf("GetSyncEnabled() after restart = true, want false (persisted decision)")
	}
	if app3.syncJobCancel != nil {
		t.Fatalf("background sync job started after restart despite SyncEnabled=false")
	}
}

// TestApp_SyncIntervalMinutesDefaultsValidatesAndPersists covers the
// Preferences > Synchronization dropdown's backend: the default before
// anything is saved, rejection of values outside validSyncIntervalsMinutes,
// the requirement of a prior login (the preference lives in the same
// config row as the credentials), and that a valid choice round-trips
// through GetSyncIntervalMinutes.
func TestApp_SyncIntervalMinutesDefaultsValidatesAndPersists(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()

	if got := app.GetSyncIntervalMinutes(); got != defaultSyncIntervalMinutes {
		t.Fatalf("GetSyncIntervalMinutes() before any config = %d, want default %d", got, defaultSyncIntervalMinutes)
	}

	// Requires a prior login: no config row to store the preference in yet.
	if err := app.SetSyncIntervalMinutes(15); err == nil {
		t.Fatalf("SetSyncIntervalMinutes(15) before login = nil error, want an error")
	}

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}

	// Only 5, 15, 30 and 60 are accepted.
	for _, invalid := range []int{0, 1, 10, 20, 45, 90, -5} {
		if err := app.SetSyncIntervalMinutes(invalid); err == nil {
			t.Fatalf("SetSyncIntervalMinutes(%d) = nil error, want an error (not in %v)", invalid, validSyncIntervalsMinutes)
		}
	}
	if got := app.GetSyncIntervalMinutes(); got != defaultSyncIntervalMinutes {
		t.Fatalf("GetSyncIntervalMinutes() after only-rejected attempts = %d, want it to remain the default %d", got, defaultSyncIntervalMinutes)
	}

	for _, valid := range validSyncIntervalsMinutes {
		if err := app.SetSyncIntervalMinutes(valid); err != nil {
			t.Fatalf("SetSyncIntervalMinutes(%d) error = %v", valid, err)
		}
		if got := app.GetSyncIntervalMinutes(); got != valid {
			t.Fatalf("GetSyncIntervalMinutes() after SetSyncIntervalMinutes(%d) = %d, want %d", valid, got, valid)
		}
	}
}

// TestApp_DeleteRemoteNodeDefaultsValidatesAndPersists covers the
// Preferences > Synchronization "Modalità di eliminazione degli oggetti
// remoti" dropdown's backend: the default before anything is saved,
// rejection of values outside validDeleteRemoteNodeValues, the requirement
// of a prior login (the preference lives in the same config row as the
// credentials), and that a valid choice round-trips through
// GetDeleteRemoteNode.
func TestApp_DeleteRemoteNodeDefaultsValidatesAndPersists(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()

	if got := app.GetDeleteRemoteNode(); got != defaultDeleteRemoteNode {
		t.Fatalf("GetDeleteRemoteNode() before any config = %q, want default %q", got, defaultDeleteRemoteNode)
	}

	// Requires a prior login: no config row to store the preference in yet.
	if err := app.SetDeleteRemoteNode("delete"); err == nil {
		t.Fatalf("SetDeleteRemoteNode(\"delete\") before login = nil error, want an error")
	}

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}

	// Only "trash" and "delete" are accepted.
	for _, invalid := range []string{"", "purge", "TRASH", "Delete"} {
		if err := app.SetDeleteRemoteNode(invalid); err == nil {
			t.Fatalf("SetDeleteRemoteNode(%q) = nil error, want an error (not in %v)", invalid, validDeleteRemoteNodeValues)
		}
	}
	if got := app.GetDeleteRemoteNode(); got != defaultDeleteRemoteNode {
		t.Fatalf("GetDeleteRemoteNode() after only-rejected attempts = %q, want it to remain the default %q", got, defaultDeleteRemoteNode)
	}

	for _, valid := range validDeleteRemoteNodeValues {
		if err := app.SetDeleteRemoteNode(valid); err != nil {
			t.Fatalf("SetDeleteRemoteNode(%q) error = %v", valid, err)
		}
		if got := app.GetDeleteRemoteNode(); got != valid {
			t.Fatalf("GetDeleteRemoteNode() after SetDeleteRemoteNode(%q) = %q, want %q", valid, got, valid)
		}
	}
}

// TestApp_SetSyncIntervalMinutesRestartsRunningJobWithoutImmediateCycle
// verifies that changing the interval while the background job is running
// (a) restarts it, so the new interval governs the next tick instead of
// only applying after a relaunch, and (b) never runs an immediate cycle
// itself - it must not grab the sync lock, exactly like
// TestApp_SetSyncEnabledDoesNotRaceExplicitStartFullSync guards for
// SetSyncEnabled(true).
func TestApp_SetSyncIntervalMinutesRestartsRunningJobWithoutImmediateCycle(t *testing.T) {
	const user, pass = "user@example.com", "s3cret"
	endpoint := newFakeCarbonioServer(t, user, pass)
	dbPath := filepath.Join(t.TempDir(), "gui-config.db")

	app := openTestApp(t, dbPath)
	defer app.db.Close()
	defer app.stopBackgroundSync()

	if result := app.Login(endpoint, user, pass); !result.Success {
		t.Fatalf("Login() = %+v, want Success=true", result)
	}
	syncDir := filepath.Join(t.TempDir(), "carbonio-sync")
	if err := app.SetSyncFolder(syncDir); err != nil {
		t.Fatalf("SetSyncFolder() error = %v", err)
	}

	// Not running yet: must no-op silently, not panic or error.
	if err := app.SetSyncIntervalMinutes(30); err != nil {
		t.Fatalf("SetSyncIntervalMinutes(30) while job not running error = %v", err)
	}
	if app.syncJobCancel != nil {
		t.Fatalf("SetSyncIntervalMinutes() started the background job on its own")
	}

	if err := app.SetSyncEnabled(true); err != nil {
		t.Fatalf("SetSyncEnabled(true) error = %v", err)
	}
	if app.syncJobCancel == nil {
		t.Fatalf("SetSyncEnabled(true) did not start the background job")
	}
	firstCtx := app.syncJobCtx

	if err := app.SetSyncIntervalMinutes(60); err != nil {
		t.Fatalf("SetSyncIntervalMinutes(60) error = %v", err)
	}
	if app.syncJobCancel == nil {
		t.Fatalf("SetSyncIntervalMinutes() left the background job stopped, want it restarted")
	}
	if firstCtx.Err() == nil {
		t.Fatalf("SetSyncIntervalMinutes() did not restart the running job (previous context still alive)")
	}
	if app.syncJobCtx == firstCtx || app.syncJobCtx.Err() != nil {
		t.Fatalf("SetSyncIntervalMinutes() left the running job on a stale/cancelled context after restart")
	}
	if !app.syncMu.TryLock() {
		t.Fatalf("SetSyncIntervalMinutes() left the sync lock held - restarting the job must not run an immediate cycle")
	}
	app.syncMu.Unlock()
}
