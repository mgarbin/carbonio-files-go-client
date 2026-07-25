// Command carbonio-files-go-client is both the desktop GUI and the
// command-line client for carbonio-files-go-client.
//
//   - Run with no arguments: opens the desktop GUI (Wails).
//   - Run with -cli: unlocks every CLI flag documented in the README
//     (-getAllNode, -uploadFile, -liveCacheSync, ...).
//
// Any other combination (flags without -cli) is rejected: CLI flags are
// only available once -cli is explicitly passed.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"carbonio-files-go-client/img"
	"carbonio-files-go-client/pkg/actions"
	"carbonio-files-go-client/pkg/appdir"
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/config"
	"carbonio-files-go-client/pkg/i18n"
	"carbonio-files-go-client/pkg/logger"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"

	"github.com/mgarbin/systray"
	"github.com/rs/zerolog/log"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if _, err := logger.Init(logger.Default()); err != nil {
		// logger.Default() never fails validation; guard anyway so a future
		// change to Default() cannot silently produce a broken logger.
		panic(err)
	}

	cliMode, rest := splitCLIFlag(os.Args[1:])

	if cliMode {
		// Rewrite os.Args so the CLI's own flag.Parse() (in runCLI) never
		// sees -cli.
		os.Args = append([]string{os.Args[0]}, rest...)
		runCLI()
		return
	}

	if len(rest) > 0 {
		log.Error().Strs("args", rest).Msg("Unknown arguments")
		log.Error().Msg("Pass -cli to use the command-line interface, or run with no arguments to open the desktop app.")
		os.Exit(1)
	}

	runGUI()
}

// splitCLIFlag reports whether -cli/--cli is present in args and returns the
// remaining arguments with it removed.
func splitCLIFlag(args []string) (cliMode bool, rest []string) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == "-cli" || a == "--cli" {
			cliMode = true
			continue
		}
		rest = append(rest, a)
	}
	return cliMode, rest
}

// runGUI starts the Wails desktop application.
func runGUI() {
	frontendFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Error().Err(err).Msg("Error preparing GUI assets")
		os.Exit(1)
	}

	app := NewApp()

	// Register the tray's status item/menu now, on this goroutine (the
	// real OS main thread — systray's package init() already called
	// runtime.LockOSThread() before main() ran). We deliberately do NOT
	// use systray.Run(): that spawns systray's own platform event loop,
	// and on macOS AppKit allows only one NSApplication run loop, which
	// must live on the main thread. Wails drives that single run loop
	// (started inside wails.Run below); RunWithExternalLoop only wires up
	// the tray's status item/menu and hands back start/stop hooks instead
	// of running a competing loop, so Wails' loop ends up servicing tray
	// events too. Closing the main window (the X button) minimizes to the
	// notification area instead of exiting; the app only really quits
	// from the tray's "Quit" item or OS session teardown.
	startTray, stopTray := systray.RunWithExternalLoop(func() { onTrayReady(app) }, func() {})
	startTray()

	err = wails.Run(&options.App{
		Title:             "Carbonio Files Client",
		Width:             1024,
		Height:            720,
		MinWidth:          860,
		MinHeight:         600,
		BackgroundColour:  options.NewRGB(255, 255, 255),
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
		},
		OnStartup: app.startup,
		OnShutdown: func(ctx context.Context) {
			stopTray()
			app.shutdown(ctx)
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Error starting GUI")
		os.Exit(1)
	}
}

// onTrayReady configures the tray icon/tooltip and menu once the tray
// backend is ready, localizing every label/tooltip for the host OS
// locale the same way the dashboard frontend does (see pkg/i18n;
// DetectAndLoad falls back to English for anything undetected/missing).
// "Show window" and left-clicking/double-clicking the icon restore the
// window that HideWindowOnClose kept alive but hidden; "Open sync
// folder" opens the configured local sync folder in the OS file manager
// and is disabled until a folder has actually been configured, becoming
// enabled once SetSyncFolder persists one (App.refreshTrayOpenSyncFolderItem);
// "Quit" is the only path that actually terminates the app.
func onTrayReady(app *App) {
	_, catalog := i18n.DetectAndLoad()
	tr := func(key, fallback string) string {
		if v, ok := catalog[key]; ok && v != "" {
			return v
		}
		return fallback
	}

	systray.SetIcon(img.Icon)
	systray.SetTooltip(tr("app.title", "Carbonio Files Client"))

	show := systray.AddMenuItem(tr("tray.showWindow", "Show window"), tr("tray.showWindowTooltip", "Show the Carbonio Files Client window"))
	show.Click(func() {
		if app.ctx != nil {
			wailsruntime.WindowShow(app.ctx)
		}
	})

	openSyncFolder := systray.AddMenuItem(tr("tray.openSyncFolder", "Open sync folder"), tr("tray.openSyncFolderTooltip", "Open the local sync folder"))
	app.setTrayOpenSyncFolderItem(openSyncFolder)
	// A returning user may already have a folder persisted from a
	// previous run; a first-time user won't yet (setup wizard runs after
	// this), and SetSyncFolder re-enables the item once they finish it.
	app.refreshTrayOpenSyncFolderItem()
	openSyncFolder.Click(func() {
		if err := app.OpenSyncFolder(); err != nil {
			log.Error().Err(err).Msg("Error opening sync folder from tray")
		}
	})

	systray.AddSeparator()

	quit := systray.AddMenuItem(tr("tray.quit", "Quit"), tr("tray.quitTooltip", "Quit Carbonio Files Client"))
	quit.Click(func() {
		if app.ctx != nil {
			wailsruntime.Quit(app.ctx)
		}
	})

	onShowClick := func(_ systray.IMenu) {
		if app.ctx != nil {
			wailsruntime.WindowShow(app.ctx)
		}
	}
	systray.SetOnClick(onShowClick)
	systray.SetOnDClick(onShowClick)
}

// runCLI is the original command-line entry point: it holds every flag
// documented in the README, gated behind the top-level -cli flag.
func runCLI() {
	listAllNode := flag.Bool("getAllNode", false, "Use this flag to obtain all files node")
	downloadAllFiles := flag.Bool("downloadAllFiles", false, "Use this flag to create Folder directory tree and download all files")
	createFolder := flag.String("createFolder", "", "Use this flag to create a folder (specify Name) then specify a parentId where to create it")
	printFlagInfo := flag.Bool("v", false, "output helper with all flags")
	uploadFile := flag.String("uploadFile", "", "Use this flag to upload a specific file into files, specify also parentId")
	uploadNewVersionFile := flag.String("uploadNewVersionFile", "", "Use this flag to upload a specific file into files, specify also nodeId and parentId")
	overwriteVersion := flag.Bool("overwriteVersion", false, "Use this flag to overwrite a file during the uploadNewVersionFile")
	nodeId := flag.String("nodeId", "", "Use this flag to specify NodeId")
	parentId := flag.String("parentId", "", "Use this flag to specify ParentId")
	liveSyncCheck := flag.Bool("liveSyncCheck", false, "Use this flag to check differences between local folder and remote folder")
	cacheSync := flag.Bool("cacheSync", false, "Use this flag to enable sqlite cache for liveSyncCheck")
	updateCacheSync := flag.Bool("updateCacheSync", false, "Use this flag to initialize sqlite cache for liveSyncCheck and update file records with local and remote info")
	liveCacheSync := flag.Bool("liveCacheSync", false, "Use this flag to sync local and remote files using the sqlite cache db")
	fullCacheSync := flag.Bool("fullCacheSync", false, "Use this flag to run updateCacheSync followed by liveCacheSync in one step")
	moveNodes := flag.Bool("moveNodes", false, "Use this flag to move nodes to a new destination")
	deleteNodes := flag.Bool("deleteNodes", false, "Use this flag to delete nodes")
	destinationId := flag.String("destinationId", "", "Use this flag to specify the destination folder id for moveNodes")
	nodesIdList := flag.String("nodesIdList", "", "Use this flag to specify a comma-separated list of node ids for moveNodes or deleteNodes")
	trashNodes := flag.Bool("trashNodes", false, "Use this flag to move nodes to trash instead of deleting permanently")
	logLevel := flag.String("logLevel", "", "Log level: trace, debug, info, warn, error, fatal, panic, disabled (default: info, or config.yaml Logging.level)")
	logFormat := flag.String("logFormat", "", "Log format: console or json (default: console, or config.yaml Logging.format)")
	logOutput := flag.String("logOutput", "", "Log output: console, file or both (default: console, or config.yaml Logging.output)")
	logPath := flag.String("logPath", "", "Log file path, used when -logOutput is file or both (default: <home>/.carbonio_files_sync/carbonio-files-go-client.log, or config.yaml Logging.path)")

	flag.Parse()

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Error().Err(err).Msg("Error loading config")
		return
	}

	logCfg := logger.Config{
		Level:    firstNonEmpty(*logLevel, cfg.Logging.Level),
		Format:   logger.Format(firstNonEmpty(*logFormat, cfg.Logging.Format)),
		Output:   logger.Output(firstNonEmpty(*logOutput, cfg.Logging.Output)),
		FilePath: firstNonEmpty(*logPath, cfg.Logging.Path),
	}
	closer, err := logger.Init(logCfg)
	if err != nil {
		log.Error().Err(err).Msg("Error configuring logger")
		return
	}
	defer closer.Close()

	carbonioAuth := &carbonio.HTTPAuthenticator{Endpoint: cfg.Main.Endpoint}

	// Read local filesystem items
	localFolder := "./files"

	if cfg.Main.FilesFolder != "" {
		localFolder = cfg.Main.FilesFolder
	}

	// if folder doesn't exist, create it and initialize empty cache
	if _, err := os.Stat(localFolder); os.IsNotExist(err) {
		if err := os.MkdirAll(localFolder, 0755); err != nil {
			log.Error().Err(err).Str("folder", localFolder).Msg("Error creating local folder")
			return
		}
		log.Info().Str("folder", localFolder).Msg("Local folder created")
	}

	var zmAuthToken string
	if cfg.Main.AuthToken != nil && *cfg.Main.AuthToken != "" {
		// Explicit override in config.yaml: use it verbatim and skip both
		// the cached-token store and the login step entirely.
		zmAuthToken = *cfg.Main.AuthToken
	} else {
		token, err := loginWithCachedToken(carbonioAuth, cfg.Main.Username, cfg.Main.Password)
		if err != nil {
			log.Error().Err(err).Msg("Error obtaining Carbonio token")
			return
		}
		zmAuthToken = token
	}

	if *printFlagInfo {
		actions.PrintFlagInfo()
	}

	if *listAllNode {
		actions.ListAllNode(cfg.Main.Endpoint, zmAuthToken)
	}

	if *downloadAllFiles {
		actions.DownloadAllFiles(cfg.Main.Endpoint, zmAuthToken, carbonioAuth)
	}

	if *uploadFile != "" && *parentId != "" {
		actions.UploadFile(carbonioAuth, zmAuthToken, *parentId, *uploadFile, nodeId)
	}

	if *uploadNewVersionFile != "" && *nodeId != "" && *parentId != "" {
		actions.UploadNewVersionFile(carbonioAuth, zmAuthToken, *parentId, *uploadNewVersionFile, *overwriteVersion, nodeId)
	}

	if *createFolder != "" && *parentId != "" {
		actions.CreateFolder(cfg.Main.Endpoint, zmAuthToken, *parentId, *createFolder)
	}

	if *moveNodes {
		if err := actions.MoveNodes(cfg.Main.Endpoint, zmAuthToken, *destinationId, *nodesIdList); err != nil {
			return
		}
	}

	if *trashNodes {
		if err := actions.TrashNodes(cfg.Main.Endpoint, zmAuthToken, *nodesIdList); err != nil {
			return
		}
	}

	if *deleteNodes {
		if err := actions.DeleteNodes(cfg.Main.Endpoint, zmAuthToken, *nodesIdList); err != nil {
			return
		}
	}

	if *liveSyncCheck {
		if err := actions.LiveSyncCheck(cfg.Main.Endpoint, zmAuthToken, localFolder, *cacheSync); err != nil {
			return
		}
	}

	if *updateCacheSync {
		if err := actions.UpdateCacheSync(cfg.Main.Endpoint, zmAuthToken, localFolder); err != nil {
			return
		}
	}

	if *liveCacheSync {
		if err := actions.LiveCacheSync(cfg.Main.Endpoint, zmAuthToken, localFolder, carbonioAuth, cfg.Sync.DeleteRemoteNode); err != nil {
			return
		}
	}

	if *fullCacheSync {
		if err := actions.FullCacheSync(cfg.Main.Endpoint, zmAuthToken, localFolder, carbonioAuth, cfg.Sync.DeleteRemoteNode); err != nil {
			return
		}
	}
}

// loginWithCachedToken opens the CLI's encrypted token store
// (appdir.Path("file_sync_cache.db"), the same SQLite database the
// -*CacheSync flags use) and obtains a ZM_AUTH_TOKEN through
// carbonio.Session: a token saved from a previous run is reused as-is when
// the server still accepts it, otherwise a fresh username/password login is
// performed and its token is persisted (encrypted at rest) for the next run
// to reuse.
func loginWithCachedToken(auth *carbonio.HTTPAuthenticator, username, password string) (string, error) {
	store, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		return "", fmt.Errorf("opening token store: %w", err)
	}
	defer store.Close()

	session := carbonio.NewSession(auth, store, username, password)
	return session.Login()
}

// firstNonEmpty returns the first non-empty string in vals, or "" if all are
// empty. Used to apply the "-flag overrides config.yaml overrides built-in
// default" precedence for logging settings.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
