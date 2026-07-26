package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"carbonio-files-go-client/pkg/actions"
	"carbonio-files-go-client/pkg/appdir"
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/i18n"
	"carbonio-files-go-client/pkg/logger"
	"carbonio-files-go-client/pkg/notify"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"

	"github.com/mgarbin/systray"
	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// defaultSyncIntervalMinutes is the background sync job's tick interval
// (see SetSyncIntervalMinutes/GetSyncIntervalMinutes) when no preference
// has been saved yet - also the dashboard's Preferences > Synchronization
// dropdown's default selection.
const defaultSyncIntervalMinutes = 5

// validSyncIntervalsMinutes are the only values SetSyncIntervalMinutes
// accepts, and the exact choices offered by the dashboard's Preferences >
// Synchronization dropdown.
var validSyncIntervalsMinutes = []int{5, 15, 30, 60}

// defaultDeleteRemoteNode is the "remote delete mode" the background sync
// job (and actions.LiveCacheSync/FullCacheSync generally) falls back to
// when no preference has been saved yet - also the dashboard's Preferences
// > Synchronization "Modalità di eliminazione degli oggetti remoti"
// dropdown's default selection.
const defaultDeleteRemoteNode = actions.DeleteModeTrash

// validDeleteRemoteNodeValues are the only values SetDeleteRemoteNode
// accepts, and the exact choices offered by the dashboard's Preferences >
// Synchronization "Modalità di eliminazione degli oggetti remoti" dropdown.
var validDeleteRemoteNodeValues = []string{actions.DeleteModeTrash, actions.DeleteModeDelete}

// App is the Wails-bound backend for the desktop GUI. Every exported method
// is callable from the frontend as window.go.main.App.<Method>.
type App struct {
	ctx  context.Context
	db   *sqlitecache.SqliteHelper
	auth *carbonio.HTTPAuthenticator

	// docsProxy is the local reverse proxy OpenNodeWithDocs points the
	// embedded webview at - see carbonio.DocsProxy's doc comment for why
	// a proxy, rather than a real cookie in the webview's own jar,
	// authenticates it (Wails v2 has no cookie-manager API).
	docsProxy *carbonio.DocsProxy

	// sessionMu guards session: the background sync goroutine reads it
	// concurrently with login/logout requests mutating it. Access only via
	// currentSession/setSession.
	sessionMu sync.RWMutex
	// session holds the currently authenticated user, if any.
	session Session

	// logCloser closes the log file opened by applyLoggingConfig, if any.
	logCloser io.Closer

	// syncMu enforces mutual exclusion between sync operations
	// (UpdateCacheSync/LiveCacheSync, whether triggered by StartFullSync,
	// the initial setup-wizard scan, or a background sync cycle): only one
	// may ever run at a time. Acquire/release it only via
	// tryBeginSync/endSync.
	syncMu sync.Mutex
	// syncing mirrors syncMu's held/free state as a lock-free read for the
	// frontend (see SyncStatus.InProgress) - true exactly while syncMu is
	// held by a sync operation.
	syncing atomic.Bool

	// syncJobMu guards syncJobCancel/syncJobCtx: maybeStartBackgroundSync
	// and stopBackgroundSync can be called concurrently (e.g. a login
	// request racing the setup wizard's completion).
	syncJobMu sync.Mutex
	// syncJobCancel stops the running background sync loop, or nil if it
	// isn't running.
	syncJobCancel context.CancelFunc
	// syncJobCtx is the context syncJobCancel cancels, kept alongside it
	// so a caller can tell whether a given job instance is still the one
	// currently running (ctx.Err() == nil) versus superseded by a restart
	// (ctx.Err() != nil) - see restartBackgroundSyncJobIfRunning.
	syncJobCtx context.Context

	// trayMu guards trayOpenSyncFolder: onTrayReady (main goroutine,
	// before wails.Run starts) registers the item once, while
	// SetSyncFolder (invoked later from the Wails/frontend goroutine, by
	// the setup wizard or the Preferences "Change folder…" button) reads
	// it to re-enable the tray's "Open sync folder" item once a folder
	// actually exists. A mutex is required, not just program order,
	// because the two accesses cross goroutines.
	trayMu sync.Mutex
	// trayOpenSyncFolder is the tray's "Open sync folder" menu item. Nil
	// until onTrayReady registers it (e.g. still nil in unit tests, which
	// never build a tray) - every access is nil-checked.
	trayOpenSyncFolder *systray.MenuItem

	// configMu serializes every read-modify-write sequence against the
	// singleton config row (GetConfig followed by UpdateConfig): each of
	// UpdateLoggingConfig, SetSyncFolder, SetSyncEnabled, ResetSync,
	// SetSyncIntervalMinutes and SetDeleteRemoteNode reads the full row,
	// mutates one field and writes the whole row back, so two of them
	// running concurrently (e.g. the Preferences > Synchronization panel
	// saves SetSyncIntervalMinutes and SetDeleteRemoteNode together) can
	// race: the second writer's stale read silently reverts the first
	// writer's field. Acquire/release it only around such a sequence.
	configMu sync.Mutex
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
	return &App{auth: &carbonio.HTTPAuthenticator{}, docsProxy: carbonio.NewDocsProxy()}
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
	// A returning user may already have a sync folder persisted from a
	// previous run, but onTrayReady built (and disabled) the tray's "Open
	// sync folder" item before a.db existed - see refreshTrayOpenSyncFolderItem's
	// doc comment. Re-check now that the config store is actually open.
	a.refreshTrayOpenSyncFolderItem()

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

// shutdown is wired as options.App.OnShutdown: it stops the background sync
// job and flushes/closes the log file opened by applyLoggingConfig, if any.
// Tray teardown (stopTray, from runGUI) is called separately, before this,
// so it doesn't outlive the process.
func (a *App) shutdown(ctx context.Context) {
	a.stopBackgroundSync()
	a.docsProxy.Stop()
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
	a.configMu.Lock()
	defer a.configMu.Unlock()
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

// OpenSyncFolder opens the configured local sync folder with the host OS'
// default file manager (see openWithDefaultApp) - used by both the tray
// menu's "Open sync folder" item and, if the frontend ever wants it, the
// dashboard. Returns an error if no sync folder has been configured yet
// (e.g. before the first login or before the setup wizard has run).
func (a *App) OpenSyncFolder() error {
	folder := a.GetSyncFolder()
	if folder.Path == "" {
		return errors.New("sync folder not configured")
	}
	return openWithDefaultApp(folder.Path)
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
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg, err := a.db.GetConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("log in first: the sync folder is stored alongside your saved credentials")
	}
	cfg.FilesLocalFolder = path
	if err := a.db.UpdateConfig(*cfg); err != nil {
		return err
	}
	a.refreshTrayOpenSyncFolderItem()
	return nil
}

// setTrayOpenSyncFolderItem records the tray's "Open sync folder" menu
// item so a later SetSyncFolder call (setup wizard or Preferences' "Change
// folder…") can enable it once a folder actually exists. Called once by
// onTrayReady after building the tray menu.
func (a *App) setTrayOpenSyncFolderItem(item *systray.MenuItem) {
	a.trayMu.Lock()
	a.trayOpenSyncFolder = item
	a.trayMu.Unlock()
}

// refreshTrayOpenSyncFolderItem enables/disables the tray's "Open sync
// folder" item to match whether a sync folder is currently configured.
// a.db is nil when onTrayReady runs (the tray is built synchronously
// before wails.Run, i.e. before OnStartup/App.startup ever executes), so
// its own initial call here always sees "not configured" and only
// establishes the correct default (disabled) look while the app finishes
// starting up. The real state is established once a.db actually exists
// - startup calls this right after opening it, which is what enables the
// item immediately for a returning user whose folder was already
// configured in a previous run - and again every time SetSyncFolder
// succeeds (a first-time user completing the setup wizard, or anyone
// changing the folder from Preferences). No-op if the tray hasn't
// registered the item yet (e.g. CLI mode, unit tests).
func (a *App) refreshTrayOpenSyncFolderItem() {
	a.trayMu.Lock()
	item := a.trayOpenSyncFolder
	a.trayMu.Unlock()
	if item == nil {
		return
	}
	if a.GetSyncFolder().Path == "" {
		item.Disable()
	} else {
		item.Enable()
	}
}

// UpdateCacheSync runs the initial sqlite sync cache scan (see
// actions.UpdateCacheSync, the same routine behind the -updateCacheSync CLI
// flag) against the currently persisted sync folder, using the active
// session's endpoint and auth token. It requires a prior successful login
// and a sync folder already saved via SetSyncFolder. The frontend calls
// this once, automatically, right after the first-time setup wizard
// (SyncSetupScreen) saves the sync folder, showing a loading screen for the
// - potentially long - duration of the call since it walks the whole local
// folder and the whole remote tree before returning.
func (a *App) UpdateCacheSync() error {
	session := a.currentSession()
	if !session.LoggedIn {
		return errors.New("log in first")
	}
	folder := a.GetSyncFolder()
	if folder.Path == "" {
		return errors.New("sync folder not configured")
	}
	if !a.tryBeginSync() {
		return errors.New("a sync is already in progress")
	}
	err := actions.UpdateCacheSync(session.Endpoint, session.Token, folder.Path)
	a.endSync()
	if err != nil {
		return err
	}
	// The sync folder is now configured for the first time: start the
	// periodic background sync job (see maybeStartBackgroundSync).
	a.maybeStartBackgroundSync()
	return nil
}

// StartFullSync runs a full cache sync - UpdateCacheSync followed by
// LiveCacheSync (see actions.FullCacheSync, the same routine behind the
// -fullCacheSync CLI flag) - against the currently persisted sync folder,
// using the active session's credentials. It backs the dashboard's "Avvia
// Sincronizzazione" button; the frontend flips the sync status badge to
// "active" once this call is dispatched, and SyncStatus.InProgress reports
// "in progress" for its whole duration (see the syncing field). Fails
// immediately, without running anything, if another sync (manual or the
// periodic background job) is already in progress - see tryBeginSync.
func (a *App) StartFullSync() error {
	session := a.currentSession()
	if !session.LoggedIn {
		return errors.New("log in first")
	}
	folder := a.GetSyncFolder()
	if folder.Path == "" {
		return errors.New("sync folder not configured")
	}
	if !a.tryBeginSync() {
		return errors.New("a sync is already in progress")
	}
	defer a.endSync()
	summary, err := actions.FullCacheSync(session.Endpoint, session.Token, folder.Path, a.auth, a.GetDeleteRemoteNode())
	if err != nil {
		return err
	}
	a.notifySyncSummary(summary)
	return nil
}

// notifySyncSummary raises a single desktop notification for a sync run
// that changed at least one document (see actions.SyncSummary) - covering
// both StartFullSync (a manual "Avvia Sincronizzazione" run) and every
// background sync cycle that found pending changes. Every changed
// file/folder gets its own line in the notification body (e.g. "Created a
// new folder Projects/Reports", "Modified file notes.txt") so a cycle that
// touched several documents still raises exactly one OS notification,
// never one per file. Localized the same way onTrayReady localizes the
// tray menu: by loading the OS locale's catalog directly, since this can
// run from a background goroutine with no access to the frontend's
// already-loaded one. A no-op when nothing changed - a sync cycle that
// found nothing to do gets no notification.
func (a *App) notifySyncSummary(summary actions.SyncSummary) {
	if !summary.HasChanges() {
		return
	}
	_, catalog := i18n.DetectAndLoad()
	tr := func(key, fallback string) string {
		if v, ok := catalog[key]; ok && v != "" {
			return v
		}
		return fallback
	}
	lines := make([]string, 0, len(summary.New)+len(summary.Modified)+len(summary.Deleted))
	for _, c := range summary.New {
		if c.IsDirectory {
			lines = append(lines, fmt.Sprintf(tr("notification.newFolder", "Created a new folder %s"), c.Path))
		} else {
			lines = append(lines, fmt.Sprintf(tr("notification.newFile", "Created a new file %s"), c.Path))
		}
	}
	for _, c := range summary.Modified {
		lines = append(lines, fmt.Sprintf(tr("notification.modifiedFile", "Modified file %s"), c.Path))
	}
	for _, c := range summary.Deleted {
		if c.IsDirectory {
			lines = append(lines, fmt.Sprintf(tr("notification.deletedFolder", "Deleted folder %s"), c.Path))
		} else {
			lines = append(lines, fmt.Sprintf(tr("notification.deletedFile", "Deleted file %s"), c.Path))
		}
	}
	notify.SyncChange(tr("app.title", "Carbonio Files Sync"), strings.Join(lines, "\n"))
}

// tryBeginSync attempts to acquire exclusive access for a sync operation:
// UpdateCacheSync and LiveCacheSync must never run concurrently with each
// other, regardless of whether they were triggered manually
// (StartFullSync, the initial setup-wizard scan) or by the periodic
// background job. It never blocks: returns false immediately if another
// sync is already running. Every call that returns true must be paired
// with exactly one endSync call.
func (a *App) tryBeginSync() bool {
	if !a.syncMu.TryLock() {
		return false
	}
	a.syncing.Store(true)
	return true
}

// endSync releases the exclusive sync lock acquired by a successful
// tryBeginSync call.
func (a *App) endSync() {
	a.syncing.Store(false)
	a.syncMu.Unlock()
}

// maybeStartBackgroundSync starts the periodic background sync job (see
// runBackgroundSyncLoop), running its first cycle immediately so the app
// doesn't wait up to the configured sync interval (see
// GetSyncIntervalMinutes) for the first tick - used by
// every login path (and the first-time setup wizard) to resume syncing
// right away. No-ops if the job is already running, or the session/sync
// folder it depends on aren't available yet. Safe to call repeatedly.
func (a *App) maybeStartBackgroundSync() {
	a.startBackgroundSyncJob(true)
}

// startBackgroundSyncJob is maybeStartBackgroundSync's implementation,
// parameterized on whether the very first cycle runs immediately or waits
// for the first periodic tick. SetSyncEnabled(true) passes false: the
// dashboard toggle it backs already triggers its own explicit StartFullSync
// right after persisting the preference, so an immediate cycle here would
// race that call for the sync lock (tryBeginSync) and spuriously fail one
// of the two with "a sync is already in progress" - surfaced to the user
// as the dashboard's generic sync error.
func (a *App) startBackgroundSyncJob(runFirstCycleImmediately bool) {
	a.syncJobMu.Lock()
	defer a.syncJobMu.Unlock()

	if a.syncJobCancel != nil {
		return
	}
	if !a.currentSession().LoggedIn || a.GetSyncFolder().Path == "" || !a.GetSyncEnabled() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.syncJobCancel = cancel
	a.syncJobCtx = ctx
	go a.runBackgroundSyncLoop(ctx, runFirstCycleImmediately)
}

// SetSyncEnabled persists the dashboard's sync on/off decision (see
// sqlitecache.ConfigRecord.SyncEnabled) - backing the "Avvia
// Sincronizzazione" / "Ferma Sincronizzazione" button - and immediately
// starts or stops the periodic background sync job to match, so the choice
// takes effect right away and is resumed automatically on the next login
// (see maybeStartBackgroundSync, called from autoLogin/login).
func (a *App) SetSyncEnabled(enabled bool) error {
	if a.db == nil {
		return errors.New("credential store unavailable")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg, err := a.db.GetConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("log in first: the sync preference is stored alongside your saved credentials")
	}
	cfg.SyncEnabled = enabled
	if err := a.db.UpdateConfig(*cfg); err != nil {
		return err
	}
	if enabled {
		a.startBackgroundSyncJob(false)
	} else {
		a.stopBackgroundSync()
	}
	return nil
}

// GetSyncEnabled returns the persisted sync on/off decision, or false if
// none has been saved yet (e.g. before the first login).
func (a *App) GetSyncEnabled() bool {
	if a.db == nil {
		return false
	}
	cfg, err := a.db.GetConfig()
	if err != nil || cfg == nil {
		return false
	}
	return cfg.SyncEnabled
}

// ResetSync backs the dashboard's "Reset sync" confirmation dialog: once
// the user accepts the warning, it stops the sync process - the periodic
// background job, and the persisted "enabled" decision so it doesn't
// resume on the next login (mirroring SetSyncEnabled(false)) - and then
// permanently deletes every cached sync record: the filesync table's rows
// and the last-run outcome tracked in sync_meta, including the last sync
// date (see SyncStatus.LastSyncedAt), so the dashboard reports "never
// synced" again. Fails, without deleting anything, if a sync (manual or
// background) is actively running at the moment it's called - see
// tryBeginSync; the dialog's Cancel path never calls this at all.
func (a *App) ResetSync() error {
	a.stopBackgroundSync()
	if a.db != nil {
		if err := func() error {
			a.configMu.Lock()
			defer a.configMu.Unlock()
			cfg, err := a.db.GetConfig()
			if err != nil {
				return err
			}
			if cfg != nil && cfg.SyncEnabled {
				cfg.SyncEnabled = false
				return a.db.UpdateConfig(*cfg)
			}
			return nil
		}(); err != nil {
			return err
		}
	}

	if !a.tryBeginSync() {
		return errors.New("a sync is already in progress")
	}
	defer a.endSync()

	dbPath := appdir.Path("file_sync_cache.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	}
	cacheDb, err := sqlitecache.NewSqliteHelper(dbPath)
	if err != nil {
		return err
	}
	defer cacheDb.Close()

	if err := cacheDb.DeleteAllAndResetAutoIncrement(); err != nil {
		return err
	}
	return cacheDb.ClearSyncMeta()
}

// GetSyncIntervalMinutes returns the persisted background sync interval, in
// minutes, or defaultSyncIntervalMinutes if none has been saved yet (e.g.
// before the first login) or the stored value is no longer one of
// validSyncIntervalsMinutes.
func (a *App) GetSyncIntervalMinutes() int {
	if a.db != nil {
		if cfg, err := a.db.GetConfig(); err == nil && cfg != nil && isValidSyncInterval(cfg.SyncIntervalMinutes) {
			return cfg.SyncIntervalMinutes
		}
	}
	return defaultSyncIntervalMinutes
}

// SetSyncIntervalMinutes persists the dashboard's Preferences >
// Synchronization interval choice (see
// sqlitecache.ConfigRecord.SyncIntervalMinutes) and, if the periodic
// background sync job is currently running, restarts it so the new
// interval governs its very next tick instead of only taking effect after
// the app is relaunched.
func (a *App) SetSyncIntervalMinutes(minutes int) error {
	if !isValidSyncInterval(minutes) {
		return fmt.Errorf("invalid sync interval: %d minutes (must be one of %v)", minutes, validSyncIntervalsMinutes)
	}
	if a.db == nil {
		return errors.New("credential store unavailable")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg, err := a.db.GetConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("log in first: the sync interval is stored alongside your saved credentials")
	}
	cfg.SyncIntervalMinutes = minutes
	if err := a.db.UpdateConfig(*cfg); err != nil {
		return err
	}
	a.restartBackgroundSyncJobIfRunning()
	return nil
}

// isValidSyncInterval reports whether minutes is one of
// validSyncIntervalsMinutes.
func isValidSyncInterval(minutes int) bool {
	for _, v := range validSyncIntervalsMinutes {
		if v == minutes {
			return true
		}
	}
	return false
}

// GetDeleteRemoteNode returns the persisted "remote delete mode"
// preference - actions.DeleteModeTrash or actions.DeleteModeDelete - or
// defaultDeleteRemoteNode if none has been saved yet (e.g. before the
// first login) or the stored value is no longer one of
// validDeleteRemoteNodeValues.
func (a *App) GetDeleteRemoteNode() string {
	if a.db != nil {
		if cfg, err := a.db.GetConfig(); err == nil && cfg != nil && isValidDeleteRemoteNode(cfg.DeleteRemoteNode) {
			return cfg.DeleteRemoteNode
		}
	}
	return defaultDeleteRemoteNode
}

// SetDeleteRemoteNode persists the dashboard's Preferences > Synchronization
// "Modalità di eliminazione degli oggetti remoti" dropdown choice (see
// sqlitecache.ConfigRecord.DeleteRemoteNode). Unlike the sync interval,
// this preference is read fresh on every sync cycle (see
// runBackgroundSyncCycle/StartFullSync), so no running job needs
// restarting for it to take effect.
func (a *App) SetDeleteRemoteNode(mode string) error {
	if !isValidDeleteRemoteNode(mode) {
		return fmt.Errorf("invalid remote delete mode: %q (must be one of %v)", mode, validDeleteRemoteNodeValues)
	}
	if a.db == nil {
		return errors.New("credential store unavailable")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg, err := a.db.GetConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("log in first: the remote delete mode is stored alongside your saved credentials")
	}
	cfg.DeleteRemoteNode = mode
	return a.db.UpdateConfig(*cfg)
}

// isValidDeleteRemoteNode reports whether mode is one of
// validDeleteRemoteNodeValues.
func isValidDeleteRemoteNode(mode string) bool {
	for _, v := range validDeleteRemoteNodeValues {
		if v == mode {
			return true
		}
	}
	return false
}

// restartBackgroundSyncJobIfRunning restarts the periodic background sync
// job so a just-changed interval (see SetSyncIntervalMinutes) applies to
// its ticker right away. No-ops if the job isn't currently running - it
// will simply use the new interval whenever it next starts. The restart
// never runs an immediate cycle: merely resetting the ticker shouldn't by
// itself trigger a fresh full sync.
func (a *App) restartBackgroundSyncJobIfRunning() {
	a.syncJobMu.Lock()
	running := a.syncJobCancel != nil
	if running {
		a.syncJobCancel()
		a.syncJobCancel = nil
		a.syncJobCtx = nil
	}
	a.syncJobMu.Unlock()

	if running {
		a.startBackgroundSyncJob(false)
	}
}

// stopBackgroundSync stops the periodic background sync job, if running.
// Called on Logout (the session it depends on no longer applies) and on
// shutdown.
func (a *App) stopBackgroundSync() {
	a.syncJobMu.Lock()
	defer a.syncJobMu.Unlock()

	if a.syncJobCancel != nil {
		a.syncJobCancel()
		a.syncJobCancel = nil
		a.syncJobCtx = nil
	}
}

// runBackgroundSyncLoop optionally runs one sync cycle immediately (see
// startBackgroundSyncJob's runFirstCycleImmediately - true from every login
// path, so enabling sync at startup starts the whole sync process right
// away instead of waiting for the first tick; false from
// SetSyncEnabled(true), whose caller - the dashboard toggle - already runs
// its own explicit first sync), then ticks every GetSyncIntervalMinutes
// (see SetSyncIntervalMinutes) and runs one runBackgroundSyncCycle per
// tick, until ctx is cancelled by stopBackgroundSync or
// restartBackgroundSyncJobIfRunning.
func (a *App) runBackgroundSyncLoop(ctx context.Context, runFirstCycleImmediately bool) {
	if runFirstCycleImmediately {
		a.runBackgroundSyncCycle()
	}

	ticker := time.NewTicker(time.Duration(a.GetSyncIntervalMinutes()) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runBackgroundSyncCycle()
		}
	}
}

// runBackgroundSyncCycle refreshes the sqlite sync cache
// (actions.UpdateCacheSync) and, only if that refresh found pending
// changes (remote/local-only items, out-of-sync content, or a deletion
// still to propagate - see sqlitecache.CountPendingChanges), reconciles
// them with actions.LiveCacheSync. Every step is best-effort: errors are
// logged (UpdateCacheSync's own failures also land in the sync_meta table,
// surfaced on the dashboard's error zone) and never panic the goroutine.
// Skips entirely, without touching anything, if another sync (a manual
// StartFullSync or the previous cycle running long) is already in
// progress - see tryBeginSync.
func (a *App) runBackgroundSyncCycle() {
	session := a.currentSession()
	if !session.LoggedIn {
		return
	}
	folder := a.GetSyncFolder()
	if folder.Path == "" {
		return
	}
	if !a.tryBeginSync() {
		log.Debug().Msg("Background sync: skipped, another sync is already in progress")
		return
	}
	defer a.endSync()

	endpoint, token := session.Endpoint, session.Token

	if err := actions.UpdateCacheSync(endpoint, token, folder.Path); err != nil {
		log.Error().Err(err).Msg("Background sync: updateCacheSync failed")
		return
	}

	pending, err := a.pendingSyncChanges()
	if err != nil {
		log.Error().Err(err).Msg("Background sync: checking pending changes failed")
		return
	}
	if pending == 0 {
		log.Debug().Msg("Background sync: no changes detected")
		return
	}

	log.Info().Int("pending", pending).Msg("Background sync: changes detected, running liveCacheSync")
	summary, err := actions.LiveCacheSync(endpoint, token, folder.Path, a.auth, a.GetDeleteRemoteNode())
	if err != nil {
		log.Error().Err(err).Msg("Background sync: liveCacheSync failed")
		return
	}
	a.notifySyncSummary(summary)
}

// pendingSyncChanges returns how many filesync cache records still need
// reconciling (see sqlitecache.CountPendingChanges).
func (a *App) pendingSyncChanges() (int, error) {
	cacheDb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		return 0, err
	}
	defer cacheDb.Close()
	return cacheDb.CountPendingChanges()
}

// SyncStatus summarizes the local sqlite sync cache (see
// actions.UpdateCacheSync / pkg/sqlite's sync_meta and filesync tables) for
// the dashboard's "Stato Sincronizzazione" panel.
type SyncStatus struct {
	// LastSyncedAt is the RFC3339 timestamp of the last completed
	// UpdateCacheSync run, or "" if it has never run yet.
	LastSyncedAt string `json:"lastSyncedAt"`
	// MissingLocally counts items that exist on the remote server but not
	// yet on this device (sync_status = "remote_only").
	MissingLocally int `json:"missingLocally"`
	// MissingOnServer counts items that exist on this device but not yet
	// on the remote server (sync_status = "local_only").
	MissingOnServer int `json:"missingOnServer"`
	// RemoteItems counts every item currently known to exist on the
	// remote server.
	RemoteItems int `json:"remoteItems"`
	// LocalItems counts every item currently known to exist on this
	// device.
	LocalItems int `json:"localItems"`
	// LastError is the error message from the last UpdateCacheSync run,
	// or "" if it succeeded (or never ran).
	LastError string `json:"lastError"`
	// InProgress is true while a sync operation (StartFullSync, the
	// initial cache scan, or a background sync cycle) is actively
	// running.
	InProgress bool `json:"inProgress"`
	// Enabled is the persisted on/off decision (see
	// sqlitecache.ConfigRecord.SyncEnabled / SetSyncEnabled): true means
	// the periodic background sync job is meant to be running.
	Enabled bool `json:"enabled"`
}

// GetSyncStatus reads the sqlite sync cache (populated by UpdateCacheSync)
// and returns a summary for the dashboard. Returns a zero SyncStatus, no
// error, if the cache hasn't been created yet (e.g. before the first sync
// scan has ever run).
func (a *App) GetSyncStatus() (SyncStatus, error) {
	dbPath := appdir.Path("file_sync_cache.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return SyncStatus{InProgress: a.syncing.Load(), Enabled: a.GetSyncEnabled()}, nil
	}

	cacheDb, err := sqlitecache.NewSqliteHelper(dbPath)
	if err != nil {
		return SyncStatus{}, err
	}
	defer cacheDb.Close()

	status := SyncStatus{InProgress: a.syncing.Load(), Enabled: a.GetSyncEnabled()}
	if meta, err := cacheDb.GetSyncMeta(); err != nil {
		return SyncStatus{}, err
	} else if meta != nil {
		status.LastSyncedAt = meta.LastRunAt
		status.LastError = meta.LastError
	}

	missing, err := cacheDb.CountBySyncStatus("remote_only")
	if err != nil {
		return SyncStatus{}, err
	}
	status.MissingLocally = missing

	missingOnServer, err := cacheDb.CountBySyncStatus("local_only")
	if err != nil {
		return SyncStatus{}, err
	}
	status.MissingOnServer = missingOnServer

	remote, err := cacheDb.CountRemotePresent()
	if err != nil {
		return SyncStatus{}, err
	}
	status.RemoteItems = remote

	local, err := cacheDb.CountLocalPresent()
	if err != nil {
		return SyncStatus{}, err
	}
	status.LocalItems = local

	return status, nil
}

// docsOnlineHandledMimeTypes mirrors carbonio-files-ui's
// docsHandledMimeTypes (src/carbonio-files-ui-common/utils/utils.ts) - the
// file mime types the Carbonio Docs Online editor is able to open.
// GetDocsOnlineTree only ever surfaces files whose mime type is in this
// set.
var docsOnlineHandledMimeTypes = map[string]struct{}{
	"text/rtf":                      {},
	"text/plain":                    {},
	"application/msword":            {},
	"application/rtf":               {},
	"application/vnd.lotus-wordpro": {},
	"application/vnd.ms-excel":      {},
	"application/vnd.ms-excel.sheet.binary.macroEnabled.12":                     {},
	"application/vnd.ms-excel.sheet.macroEnabled.12":                            {},
	"application/vnd.ms-excel.template.macroEnabled.12":                         {},
	"application/vnd.ms-powerpoint":                                             {},
	"application/vnd.ms-powerpoint.presentation.macroEnabled.12":                {},
	"application/vnd.ms-powerpoint.template.macroEnabled.12":                    {},
	"application/vnd.ms-word.document.macroEnabled.12":                          {},
	"application/vnd.ms-word.template.macroEnabled.12":                          {},
	"application/vnd.oasis.opendocument.presentation":                           {},
	"application/vnd.oasis.opendocument.presentation-flat-xml":                  {},
	"application/vnd.oasis.opendocument.spreadsheet":                            {},
	"application/vnd.oasis.opendocument.text":                                   {},
	"application/vnd.oasis.opendocument.text-flat-xml":                          {},
	"application/vnd.oasis.opendocument.text-master":                            {},
	"application/vnd.oasis.opendocument.text-master-template":                   {},
	"application/vnd.oasis.opendocument.text-template":                          {},
	"application/vnd.oasis.opendocument.text-web":                               {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"application/vnd.openxmlformats-officedocument.presentationml.template":     {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.template":      {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template":   {},
	"application/vnd.sun.xml.calc":                                              {},
	"application/vnd.sun.xml.calc.template":                                     {},
	"application/vnd.sun.xml.impress":                                           {},
	"application/vnd.sun.xml.impress.template":                                  {},
	"application/vnd.sun.xml.writer":                                            {},
	"application/vnd.sun.xml.writer.global":                                     {},
	"application/vnd.sun.xml.writer.template":                                   {},
}

// DocsOnlineNode is one entry in the virtual remote file/folder tree
// exposed to the frontend by GetDocsOnlineTree.
type DocsOnlineNode struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	IsFolder bool              `json:"isFolder"`
	MimeType string            `json:"mimeType,omitempty"`
	Children []*DocsOnlineNode `json:"children"`
}

// GetDocsOnlineTree builds the virtual remote file/folder tree shown by the
// dashboard's "Carbonio Docs Online" section entirely from the local sync
// cache (the filesync table in file_sync_cache.db, populated by the last
// UpdateCacheSync/LiveCacheSync run) - it never talks to the server, per
// the same "already cached, no graphql query needed" reasoning as
// pendingSyncChanges/GetSyncStatus above. Folders are always included, so
// the tree stays navigable to any depth; a file is included only when
// both hold: the signed-in user can modify it
// (permissions.can_write_file, cached as FileSyncRecord.CanWriteFile) and
// its mime type is one Carbonio Docs Online can open
// (docsOnlineHandledMimeTypes). Records the last sync found deleted on the
// remote, or never synced to the remote at all (local-only), are
// excluded. Returns a lone childless root node if the cache hasn't been
// populated yet.
func (a *App) GetDocsOnlineTree() (*DocsOnlineNode, error) {
	root := &DocsOnlineNode{ID: "LOCAL_ROOT", IsFolder: true, Children: []*DocsOnlineNode{}}

	dbPath := appdir.Path("file_sync_cache.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return root, nil
	}
	cacheDb, err := sqlitecache.NewSqliteHelper(dbPath)
	if err != nil {
		return nil, err
	}
	defer cacheDb.Close()

	records, err := cacheDb.QueryAll()
	if err != nil {
		return nil, err
	}

	// First pass: register every still-synced folder, keyed by its remote
	// path, so files can be attached to their parent below regardless of
	// scan order.
	folders := map[string]*DocsOnlineNode{"": root}
	for _, rec := range records {
		if !rec.IsDirectory || rec.NodeID == "" || rec.RemotePath == "" || rec.RemoteDeleted != 0 {
			continue
		}
		folders[rec.RemotePath] = &DocsOnlineNode{
			ID:       rec.NodeID,
			Name:     path.Base(rec.RemotePath),
			IsFolder: true,
			Children: []*DocsOnlineNode{},
		}
	}

	// Second pass: link every folder under its parent, shallowest first,
	// so a parent node always exists by the time its children look it up.
	folderPaths := make([]string, 0, len(folders))
	for p := range folders {
		if p != "" {
			folderPaths = append(folderPaths, p)
		}
	}
	sort.Slice(folderPaths, func(i, j int) bool {
		return strings.Count(folderPaths[i], "/") < strings.Count(folderPaths[j], "/")
	})
	for _, p := range folderPaths {
		parent := root
		if parentPath := path.Dir(p); parentPath != "." {
			if pn, ok := folders[parentPath]; ok {
				parent = pn
			}
		}
		parent.Children = append(parent.Children, folders[p])
	}

	// Third pass: attach every file the user may modify and Docs Online
	// can open to its parent folder (root if the parent isn't a tracked
	// folder, e.g. a top-level file).
	for _, rec := range records {
		if rec.IsDirectory || rec.NodeID == "" || rec.RemotePath == "" || rec.RemoteDeleted != 0 {
			continue
		}
		if !rec.CanWriteFile {
			continue
		}
		if _, handled := docsOnlineHandledMimeTypes[rec.MimeType]; !handled {
			continue
		}
		parent := root
		if parentPath := path.Dir(rec.RemotePath); parentPath != "." {
			if pn, ok := folders[parentPath]; ok {
				parent = pn
			}
		}
		parent.Children = append(parent.Children, &DocsOnlineNode{
			ID:       rec.NodeID,
			Name:     path.Base(rec.RemotePath),
			IsFolder: false,
			MimeType: rec.MimeType,
		})
	}

	// Fourth pass: drop folders that end up with nothing to show - neither
	// a qualifying file directly inside them nor, recursively, anywhere
	// in a subfolder - so the tree only ever leads somewhere.
	pruneEmptyFolders(root)
	sortDocsOnlineChildren(root)
	return root, nil
}

// pruneEmptyFolders recursively drops n's subfolders that end up with no
// content - a leaf with no qualifying files anywhere below it - keeping
// every file child as-is (only qualifying files are ever attached to the
// tree in the first place, see GetDocsOnlineTree). It reports whether n
// itself still has anything to show, so a folder several levels up finds
// out its descendant is now empty and drops it too. n's root call result
// is intentionally ignored: an empty root simply renders as "nothing to
// show" rather than disappearing.
func pruneEmptyFolders(n *DocsOnlineNode) bool {
	kept := n.Children[:0]
	for _, c := range n.Children {
		if c.IsFolder && !pruneEmptyFolders(c) {
			continue
		}
		kept = append(kept, c)
	}
	n.Children = kept
	return len(n.Children) > 0
}

// sortDocsOnlineChildren orders a node's children folders-first, then
// alphabetically (case-insensitive) within each group, recursing into
// every folder.
func sortDocsOnlineChildren(n *DocsOnlineNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		x, y := n.Children[i], n.Children[j]
		if x.IsFolder != y.IsFolder {
			return x.IsFolder
		}
		return strings.ToLower(x.Name) < strings.ToLower(y.Name)
	})
	for _, c := range n.Children {
		if c.IsFolder {
			sortDocsOnlineChildren(c)
		}
	}
}

// OpenNodeWithDocs returns the URL the frontend should point its embedded
// <iframe> at to open nodeId in Carbonio Docs Online. Wails v2 has no
// cookie-manager API (see carbonio.DocsProxy's doc comment), so the
// webview can't be handed a real https://<endpoint>/... link with a
// ZM_AUTH_TOKEN cookie of its own; instead this starts (if not already
// running) a local reverse proxy authenticated with the current
// session's token and asks it to resolve nodeId's real editor URL (see
// carbonio.DocsProxy.ResolveOpenURL - GET /services/docs/files/open/
// <nodeId> answers with JSON, not a redirect, so the iframe can't be
// pointed at it directly), rewritten to the proxy's own local base URL.
// The proxy then transparently forwards everything the editor itself
// loads from there (assets, API calls, the collaborative WebSocket
// connection) - cookie included. Backs the "Open" action on files listed
// by GetDocsOnlineTree; requires a prior successful login.
func (a *App) OpenNodeWithDocs(nodeId string) (string, error) {
	session := a.currentSession()
	if !session.LoggedIn {
		return "", errors.New("log in first")
	}
	a.docsProxy.SetCredentials(session.Endpoint, session.Token)
	baseURL, err := a.docsProxy.Start()
	if err != nil {
		log.Error().Err(err).Str("nodeId", nodeId).Str("endpoint", session.Endpoint).
			Msg("[gui] failed to start docs proxy")
		return "", err
	}
	localURL, err := a.docsProxy.ResolveOpenURL(nodeId)
	if err != nil {
		log.Error().Err(err).Str("nodeId", nodeId).Str("endpoint", session.Endpoint).Str("proxyBase", baseURL).
			Msg("[gui] failed to resolve Docs Online open URL")
		return "", err
	}
	return localURL, nil
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

	a.setSession(Session{LoggedIn: true, Endpoint: cfg.Endpoint, Username: cfg.Username, Token: token})
	a.maybeStartBackgroundSync()
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

	a.setSession(Session{LoggedIn: true, Endpoint: endpoint, Username: username, Token: token})

	result := LoginResult{Success: true, Endpoint: endpoint, Username: username}
	if a.db != nil {
		if cfg, err := a.db.GetConfig(); err == nil && cfg != nil {
			result.NeedsSyncSetup = cfg.FilesLocalFolder == ""
		}
	}
	a.maybeStartBackgroundSync()
	return result
}

// currentSession returns a thread-safe snapshot of the current session.
func (a *App) currentSession() Session {
	a.sessionMu.RLock()
	defer a.sessionMu.RUnlock()
	return a.session
}

// setSession thread-safely replaces the current session.
func (a *App) setSession(s Session) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.session = s
}

// GetSession returns the currently authenticated user, or a zero Session if
// nobody is logged in.
func (a *App) GetSession() Session {
	return a.currentSession()
}

// Logout clears the in-memory session and removes the saved credentials so
// the next launch shows the login screen again.
func (a *App) Logout() error {
	a.stopBackgroundSync()
	a.setSession(Session{})
	if a.db == nil {
		return nil
	}
	err := a.db.DeleteConfig()
	if errors.Is(err, sqlitecache.ErrConfigNotFound) {
		return nil
	}
	return err
}
