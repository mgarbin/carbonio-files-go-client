package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"carbonio-files-go-client/pkg/appdir"
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/i18n"
	"carbonio-files-go-client/pkg/logger"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"

	"github.com/energye/systray"
	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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
	// NeedsSyncSetup is true on a successful login when no sync folder has
	// been configured yet, so the frontend must show the configuration
	// wizard (folder picker) before entering the dashboard.
	NeedsSyncSetup bool `json:"needsSyncSetup"`
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

// guiDefaultOutput is the Output the desktop GUI falls back to when
// nothing has been saved yet - fresh install, before the first login.
// Unlike the CLI's console-only logger.Default(), the GUI writes to a log
// file only: a double-clicked app usually has no attached console for the
// user to see "console" output on, and there needs to be somewhere to
// find diagnostics before Preferences > Logging is ever opened or a
// first login even happens.
const guiDefaultOutput = logger.OutputFile

// startup is wired as options.App.OnStartup: it opens the per-user encrypted
// credential store used for auto-login and applies logging settings -
// saved ones if a config row already exists, guiDefaultOutput otherwise -
// so the log file exists from the moment the app launches, independent of
// login state.
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

	var level, format, output, path string
	if cfg, err := db.GetConfig(); err == nil && cfg != nil {
		level, format, output, path = cfg.LogLevel, cfg.LogFormat, cfg.LogOutput, cfg.LogPath
	}
	if output == "" {
		output = string(guiDefaultOutput)
	}
	if err := a.applyLoggingConfig(level, format, output, path); err != nil {
		log.Error().Err(err).Msg("[gui] cannot apply logging settings")
	}
}

// shutdown is wired as options.App.OnShutdown: it flushes/closes the log
// file opened by applyLoggingConfig, if any, and removes the tray icon
// started by runSystemTray so it doesn't outlive the process.
func (a *App) shutdown(ctx context.Context) {
	if a.logCloser != nil {
		a.logCloser.Close()
	}
	systray.Quit()
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

// GetLoggingConfig returns the persisted logging settings, or the GUI's
// defaults (see guiDefaultOutput) if none were saved yet (e.g. before the
// first login).
func (a *App) GetLoggingConfig() LoggingSettings {
	def := logger.Default()
	settings := LoggingSettings{Level: def.Level, Format: string(def.Format), Output: string(guiDefaultOutput), Path: logger.DefaultPath}
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

// OpenLogFile opens path with the host OS' default program for its file
// type - the same association double-clicking it in a file manager would
// use (a text editor on most systems). path is normally whatever the
// Logging panel currently shows (which may not be saved yet); an empty
// path falls back to the built-in default (see logger.DefaultPath).
// Returns an error wrapping the underlying os.Stat failure if the file
// does not exist yet - e.g. Output is still "console" or nothing has been
// logged since the path changed.
func (a *App) OpenLogFile(path string) error {
	if strings.TrimSpace(path) == "" {
		path = logger.DefaultPath
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("log file not found: %w", err)
	}
	return openWithDefaultApp(path)
}

// openWithDefaultApp launches path with the host OS' default handler for
// its file type, without blocking on it: "open" on macOS, the shell's
// file-association mechanism via rundll32 on Windows, "xdg-open" on
// Linux/BSD desktops.
func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ChooseLogFolder opens the native OS directory picker (the same
// per-OS chooser used by ChooseSyncFolder) so the user can pick where the
// log file should live, and returns the resulting full log file path: the
// chosen folder joined with currentPath's file name, or the built-in
// default file name (see logger.DefaultPath) if currentPath is empty. An
// empty string with a nil error means the user cancelled the dialog.
func (a *App) ChooseLogFolder(currentPath string) (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:                "Select the folder for the log file",
		DefaultDirectory:     logFolderStartDir(currentPath),
		CanCreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	return filepath.Join(dir, logFileName(currentPath)), nil
}

// logFolderStartDir picks the directory to seed ChooseLogFolder's native
// picker with: currentPath's own directory if it exists on disk, else
// startingDirectory()'s fallback. Like startingDirectory, it must never
// return a path that doesn't exist (see OpenDirectoryDialog's hard error on
// a missing DefaultDirectory).
func logFolderStartDir(currentPath string) string {
	if currentPath != "" {
		if dir := filepath.Dir(currentPath); dir != "." {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				return dir
			}
		}
	}
	return startingDirectory()
}

// logFileName returns the file name to keep when the user picks a new
// folder for the log file: currentPath's own base name, or the built-in
// default (see logger.DefaultPath) when currentPath is empty or has no
// meaningful base name of its own.
func logFileName(currentPath string) string {
	if currentPath != "" {
		if name := filepath.Base(currentPath); name != "." && name != string(filepath.Separator) {
			return name
		}
	}
	return filepath.Base(logger.DefaultPath)
}

// SyncFolderSettings mirrors the files_local_folder field of
// sqlitecache.ConfigRecord as a small DTO for the frontend.
type SyncFolderSettings struct {
	// Path is the local folder used to sync remote files. Empty means no
	// folder has been configured yet: the frontend must run the setup
	// wizard before entering the dashboard.
	Path string `json:"path"`
}

// startingDirectory returns a directory known to exist on disk, to seed the
// native folder picker's initial location. OpenDirectoryDialog errors out
// immediately if DefaultDirectory is set but doesn't exist - so this must
// never return a path that isn't already present (e.g. a suggested
// "~/Carbonio Files" target the user hasn't created yet). Falls back to ""
// (no default) if even the home directory can't be resolved/statted.
func startingDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return ""
	}
	return home
}

// GetSyncFolder returns the persisted sync folder, or an empty Path if none
// has been configured yet (e.g. before the first login, or before the setup
// wizard has run).
func (a *App) GetSyncFolder() SyncFolderSettings {
	if a.db == nil {
		return SyncFolderSettings{}
	}
	cfg, err := a.db.GetConfig()
	if err != nil || cfg == nil {
		return SyncFolderSettings{}
	}
	return SyncFolderSettings{Path: cfg.FilesLocalFolder}
}

// ChooseSyncFolder opens the native OS directory picker (Explorer on
// Windows, Finder on macOS, the desktop's file chooser - typically GTK - on
// Linux; Wails selects the right one for the host OS at runtime) and
// returns the chosen absolute path. An empty string with a nil error means
// the user cancelled the dialog.
func (a *App) ChooseSyncFolder() (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:                "Select the folder to sync your Carbonio Files",
		DefaultDirectory:     startingDirectory(),
		CanCreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// SetSyncFolder persists path as the sync folder, creating it on disk if it
// doesn't exist yet. It requires a prior successful login: the sync folder
// lives in the same singleton config row as the credentials (see
// pkg/sqlite ConfigRecord), so there is nowhere to store it until that row
// exists.
func (a *App) SetSyncFolder(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("sync folder path cannot be empty")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("cannot create sync folder %q: %w", path, err)
	}
	if a.db == nil {
		return errors.New("credential store unavailable")
	}
	cfg, err := a.db.GetConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("log in first: the sync folder is stored alongside your saved credentials")
	}
	cfg.FilesLocalFolder = path
	return a.db.UpdateConfig(*cfg)
}

// configDBPath returns the per-user path of the GUI's encrypted credential
// store, independent of the process' current working directory: it lives
// alongside the CLI's cache database and the log file under
// appdir.Dir() ("<home>/.carbonio_files_sync").
func configDBPath() (string, error) {
	return appdir.Path("gui-config.db"), nil
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
	state.AutoLogin = a.autoLogin(cfg)
	return state
}

// autoLogin signs the user back in using the credentials saved from a
// previous session, reusing cfg.AuthToken as-is when the server still
// accepts it (see carbonio.Session.Login): no password is sent to the
// server in that case. A fresh username/password login - persisting the
// refreshed token - happens only when the cached token is missing or the
// server rejects it. This is the CLI's loginWithCachedToken, shared via
// carbonio.Session so both share the exact same reuse/refresh policy.
func (a *App) autoLogin(cfg *sqlitecache.ConfigRecord) LoginResult {
	a.auth.Endpoint = cfg.Endpoint
	session := carbonio.NewSession(a.auth, a.db, cfg.Username, cfg.Password)
	token, err := session.Login()
	if err != nil {
		result := LoginResult{ErrorKind: string(carbonio.AuthErrorUnknown), ErrorDetail: err.Error(), Endpoint: cfg.Endpoint, Username: cfg.Username}
		var authErr *carbonio.AuthError
		if errors.As(err, &authErr) {
			result.ErrorKind = string(authErr.Kind)
		}
		return result
	}

	a.session = Session{LoggedIn: true, Endpoint: cfg.Endpoint, Username: cfg.Username, Token: token}
	return LoginResult{Success: true, Endpoint: cfg.Endpoint, Username: cfg.Username, NeedsSyncSetup: cfg.FilesLocalFolder == ""}
}

// Login authenticates against endpoint with username/password. On success
// the credentials are saved (encrypted at rest) so the next launch can log
// in automatically; on failure ErrorKind identifies what went wrong so the
// frontend can show a localized, actionable message.
func (a *App) Login(endpoint, username, password string) LoginResult {
	return a.login(endpoint, username, password)
}

// TestLogin verifies endpoint/username/password against the Carbonio auth
// endpoint without persisting anything or touching the active session. It
// backs the Authentication preferences panel's "Test connection" button:
// Save is only enabled once this reports success for the exact values the
// user is about to save.
func (a *App) TestLogin(endpoint, username, password string) LoginResult {
	_, result := authenticate(&carbonio.HTTPAuthenticator{}, endpoint, username, password)
	return result
}

// authenticate runs the Carbonio login handshake against endpoint and
// classifies the outcome into a LoginResult (Success + normalized
// Endpoint/Username on success, ErrorKind/ErrorDetail on failure). On
// success it also returns the auth token. Shared by login() (which
// additionally persists credentials and updates the session) and
// TestLogin() (which does neither).
func authenticate(auth *carbonio.HTTPAuthenticator, endpoint, username, password string) (string, LoginResult) {
	endpoint = strings.TrimSpace(endpoint)
	username = strings.TrimSpace(username)

	if endpoint == "" || username == "" || password == "" {
		return "", LoginResult{ErrorKind: string(carbonio.AuthErrorInvalidInput), Endpoint: endpoint, Username: username}
	}

	auth.Endpoint = endpoint
	token, err := auth.CarbonioZxAuth(username, password)
	if err != nil {
		result := LoginResult{ErrorKind: string(carbonio.AuthErrorUnknown), ErrorDetail: err.Error(), Endpoint: endpoint, Username: username}
		var authErr *carbonio.AuthError
		if errors.As(err, &authErr) {
			result.ErrorKind = string(authErr.Kind)
		}
		return "", result
	}
	return *token, LoginResult{Success: true, Endpoint: endpoint, Username: username}
}

func (a *App) login(endpoint, username, password string) LoginResult {
	endpoint = strings.TrimSpace(endpoint)
	username = strings.TrimSpace(username)

	if endpoint == "" || username == "" || password == "" {
		return LoginResult{ErrorKind: string(carbonio.AuthErrorInvalidInput), Endpoint: endpoint, Username: username}
	}

	a.auth.Endpoint = endpoint
	session := carbonio.NewSession(a.auth, a.db, username, password)
	// A manual login always re-authenticates with the credentials the user
	// just typed instead of reusing/validating any previously cached
	// token: the whole point of this call is to check those exact values,
	// and it persists whatever token that produces for future auto-login.
	token, err := session.Reauthenticate()
	if err != nil {
		result := LoginResult{ErrorKind: string(carbonio.AuthErrorUnknown), ErrorDetail: err.Error(), Endpoint: endpoint, Username: username}
		var authErr *carbonio.AuthError
		if errors.As(err, &authErr) {
			result.ErrorKind = string(authErr.Kind)
		}
		return result
	}

	a.session = Session{LoggedIn: true, Endpoint: endpoint, Username: username, Token: token}

	result := LoginResult{Success: true, Endpoint: endpoint, Username: username}
	if a.db != nil {
		if cfg, err := a.db.GetConfig(); err == nil && cfg != nil {
			result.NeedsSyncSetup = cfg.FilesLocalFolder == ""
		}
	}
	return result
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
