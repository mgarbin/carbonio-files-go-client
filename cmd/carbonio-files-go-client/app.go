package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/i18n"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"
)

// App is the Wails-bound backend for the desktop GUI. Every exported method
// is callable from the frontend as window.go.main.App.<Method>.
type App struct {
	ctx  context.Context
	db   *sqlitecache.SqliteHelper
	auth *carbonio.HTTPAuthenticator

	// session holds the currently authenticated user, if any.
	session Session
}

// Session describes the currently authenticated user, if any. Token is
// deliberately not exposed to the frontend.
type Session struct {
	LoggedIn bool   `json:"loggedIn"`
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Token    string `json:"-"`
}

// LoginResult is returned by every method that attempts a login (manual or
// automatic) so the frontend can render the right screen and error message.
type LoginResult struct {
	Success     bool   `json:"success"`
	ErrorKind   string `json:"errorKind"`
	ErrorDetail string `json:"errorDetail"`
	Endpoint    string `json:"endpoint"`
	Username    string `json:"username"`
}

// InitialState is returned once by Init when the frontend starts up.
type InitialState struct {
	Locale             string            `json:"locale"`
	Translations       map[string]string `json:"translations"`
	AttemptedAutoLogin bool              `json:"attemptedAutoLogin"`
	AutoLogin          LoginResult       `json:"autoLogin"`
}

// NewApp constructs the App backend. Call startup once the Wails runtime is
// ready before binding it.
func NewApp() *App {
	return &App{auth: &carbonio.HTTPAuthenticator{}}
}

// startup is wired as options.App.OnStartup: it opens the per-user encrypted
// credential store used for auto-login. Failure to open it is not fatal -
// the GUI still works, it just always shows the login screen.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dbPath, err := configDBPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[gui] cannot resolve config directory:", err)
		return
	}
	db, err := sqlitecache.NewSqliteHelper(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[gui] cannot open credential store:", err)
		return
	}
	a.db = db
}

// configDBPath returns the per-user path of the GUI's encrypted credential
// store, independent of the process' current working directory.
func configDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "carbonio-files-client")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "gui-config.db"), nil
}

// Init is called once by the frontend right after the runtime is ready. It
// resolves the UI language from the OS locale and, if credentials were saved
// from a previous session, attempts to sign the user in automatically.
func (a *App) Init() InitialState {
	locale, catalog := i18n.DetectAndLoad()
	state := InitialState{Locale: locale, Translations: catalog}

	if a.db == nil {
		return state
	}
	cfg, err := a.db.GetConfig()
	if err != nil || cfg == nil {
		return state
	}

	state.AttemptedAutoLogin = true
	state.AutoLogin = a.login(cfg.Endpoint, cfg.Username, cfg.Password)
	return state
}

// Login authenticates against endpoint with username/password. On success
// the credentials are saved (encrypted at rest) so the next launch can log
// in automatically; on failure ErrorKind identifies what went wrong so the
// frontend can show a localized, actionable message.
func (a *App) Login(endpoint, username, password string) LoginResult {
	return a.login(endpoint, username, password)
}

func (a *App) login(endpoint, username, password string) LoginResult {
	endpoint = strings.TrimSpace(endpoint)
	username = strings.TrimSpace(username)

	if endpoint == "" || username == "" || password == "" {
		return LoginResult{ErrorKind: string(carbonio.AuthErrorInvalidInput), Endpoint: endpoint, Username: username}
	}

	a.auth.Endpoint = endpoint
	token, err := a.auth.CarbonioZxAuth(username, password)
	if err != nil {
		result := LoginResult{ErrorKind: string(carbonio.AuthErrorUnknown), ErrorDetail: err.Error(), Endpoint: endpoint, Username: username}
		var authErr *carbonio.AuthError
		if errors.As(err, &authErr) {
			result.ErrorKind = string(authErr.Kind)
		}
		return result
	}

	a.session = Session{LoggedIn: true, Endpoint: endpoint, Username: username, Token: *token}

	if a.db != nil {
		if err := a.db.UpsertConfig(sqlitecache.ConfigRecord{
			Endpoint: endpoint,
			Username: username,
			Password: password,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "[gui] cannot save credentials:", err)
		}
	}

	return LoginResult{Success: true, Endpoint: endpoint, Username: username}
}

// GetSession returns the currently authenticated user, or a zero Session if
// nobody is logged in.
func (a *App) GetSession() Session {
	return a.session
}

// Logout clears the in-memory session and removes the saved credentials so
// the next launch shows the login screen again.
func (a *App) Logout() error {
	a.session = Session{}
	if a.db == nil {
		return nil
	}
	err := a.db.DeleteConfig()
	if errors.Is(err, sqlitecache.ErrConfigNotFound) {
		return nil
	}
	return err
}
