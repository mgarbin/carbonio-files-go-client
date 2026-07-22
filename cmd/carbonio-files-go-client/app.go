package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/i18n"
	"carbonio-files-go-client/pkg/logger"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"

	"github.com/rs/zerolog/log"
)

// App is the Wails-bound backend for the desktop GUI. Every exported method
// is callable from the frontend as window.go.main.App.<Method>.
type App struct {
	ctx  context.Context
	db   *sqlitecache.SqliteHelper
	auth *carbonio.HTTPAuthenticator

	// session holds the currently authenticated user, if any.
	session Session

	// logCloser closes the log file opened by applyLoggingConfig, if any.
	logCloser io.Closer
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
		log.Error().Err(err).Msg("[gui] cannot resolve config directory")
		return
	}
	db, err := sqlitecache.NewSqliteHelper(dbPath)
	if err != nil {
		log.Error().Err(err).Msg("[gui] cannot open credential store")
		return
	}
	a.db = db

	if cfg, err := db.GetConfig(); err == nil && cfg != nil {
		if err := a.applyLoggingConfig(cfg.LogLevel, cfg.LogFormat, cfg.LogOutput, cfg.LogPath); err != nil {
			log.Error().Err(err).Msg("[gui] cannot apply saved logging settings")
		}
	}
}

// shutdown is wired as options.App.OnShutdown: it flushes/closes the log
// file opened by applyLoggingConfig, if any.
func (a *App) shutdown(ctx context.Context) {
	if a.logCloser != nil {
		a.logCloser.Close()
	}
}

// applyLoggingConfig re-initializes the global zerolog logger with the given
// settings (see pkg/logger.Config), closing any previously opened log file.
// Empty fields fall back to logger.Default()'s values.
func (a *App) applyLoggingConfig(level, format, output, path string) error {
	closer, err := logger.Init(logger.Config{
		Level:    level,
		Format:   logger.Format(format),
		Output:   logger.Output(output),
		FilePath: path,
	})
	if err != nil {
		return err
	}
	if a.logCloser != nil {
		a.logCloser.Close()
	}
	a.logCloser = closer
	return nil
}

// LoggingSettings mirrors the logging fields of sqlitecache.ConfigRecord as
// a small DTO for the frontend, independent of credentials.
type LoggingSettings struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
	Path   string `json:"path"`
}

// GetLoggingConfig returns the persisted logging settings, or the built-in
// defaults if none were saved yet (e.g. before the first login).
func (a *App) GetLoggingConfig() LoggingSettings {
	def := logger.Default()
	settings := LoggingSettings{Level: def.Level, Format: string(def.Format), Output: string(def.Output), Path: logger.DefaultPath}
	if a.db == nil {
		return settings
	}
	cfg, err := a.db.GetConfig()
	if err != nil || cfg == nil {
		return settings
	}
	if cfg.LogLevel != "" {
		settings.Level = cfg.LogLevel
	}
	if cfg.LogFormat != "" {
		settings.Format = cfg.LogFormat
	}
	if cfg.LogOutput != "" {
		settings.Output = cfg.LogOutput
	}
	if cfg.LogPath != "" {
		settings.Path = cfg.LogPath
	}
	return settings
}

// UpdateLoggingConfig persists new logging settings alongside the saved
// login credentials and applies them immediately. It requires a prior
// successful login: logging settings live in the same singleton config row
// as the credentials (see pkg/sqlite ConfigRecord), so there is nowhere to
// store them until that row exists.
func (a *App) UpdateLoggingConfig(level, format, output, path string) error {
	if a.db == nil {
		return errors.New("credential store unavailable")
	}
	cfg, err := a.db.GetConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("log in first: logging settings are stored alongside your saved credentials")
	}
	cfg.LogLevel = level
	cfg.LogFormat = format
	cfg.LogOutput = output
	cfg.LogPath = path
	if err := a.db.UpdateConfig(*cfg); err != nil {
		return err
	}
	return a.applyLoggingConfig(level, format, output, path)
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
		record := sqlitecache.ConfigRecord{
			Endpoint: endpoint,
			Username: username,
			Password: password,
		}
		// Preserve any previously saved logging settings: UpsertConfig
		// replaces the whole singleton row, and a plain login should never
		// silently reset them to defaults.
		if existing, err := a.db.GetConfig(); err == nil && existing != nil {
			record.LogLevel = existing.LogLevel
			record.LogFormat = existing.LogFormat
			record.LogOutput = existing.LogOutput
			record.LogPath = existing.LogPath
		}
		if err := a.db.UpsertConfig(record); err != nil {
			log.Error().Err(err).Msg("[gui] cannot save credentials")
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
