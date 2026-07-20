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
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"carbonio-files-go-client/pkg/actions"
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/config"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cliMode, rest := splitCLIFlag(os.Args[1:])

	if cliMode {
		// Rewrite os.Args so the CLI's own flag.Parse() (in runCLI) never
		// sees -cli.
		os.Args = append([]string{os.Args[0]}, rest...)
		runCLI()
		return
	}

	if len(rest) > 0 {
		fmt.Fprintln(os.Stderr, "Unknown arguments:", rest)
		fmt.Fprintln(os.Stderr, "Pass -cli to use the command-line interface, or run with no arguments to open the desktop app.")
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
		fmt.Fprintln(os.Stderr, "Error preparing GUI assets:", err)
		os.Exit(1)
	}

	app := NewApp()

	err = wails.Run(&options.App{
		Title:            "Carbonio Files Client",
		Width:            1024,
		Height:           720,
		MinWidth:         860,
		MinHeight:        600,
		BackgroundColour: options.NewRGB(255, 255, 255),
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error starting GUI:", err)
		os.Exit(1)
	}
}

// runCLI is the original command-line entry point: it holds every flag
// documented in the README, gated behind the top-level -cli flag.
func runCLI() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	var zmAuthToken *string
	zmAuthToken = cfg.Main.AuthToken

	carbonioAuth := &carbonio.HTTPAuthenticator{Endpoint: cfg.Main.Endpoint}

	// Read local filesystem items
	localFolder := "./files"

	if cfg.Main.FilesFolder != "" {
		localFolder = cfg.Main.FilesFolder
	}

	// if folder doesn't exist, create it and initialize empty cache
	if _, err := os.Stat(localFolder); os.IsNotExist(err) {
		if err := os.MkdirAll(localFolder, 0755); err != nil {
			fmt.Println("Error creating local folder:", err)
			return
		}
		fmt.Println("Local folder created:", localFolder)
	}

	if zmAuthToken == nil {

		carbonioToken, errCarbonioToken := carbonioAuth.CarbonioZxAuth(cfg.Main.Username, cfg.Main.Password)

		if errCarbonioToken != nil {
			fmt.Printf("Error obtaining Carbonio token: %v\n", errCarbonioToken)
			return
		}

		if carbonioToken != nil {
			zmAuthToken = carbonioToken
		} else {
			fmt.Println("Failed to obtain Carbonio token and no authToken provided in config")
			return
		}
	}

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
	moveNodes := flag.Bool("moveNodes", false, "Use this flag to move nodes to a new destination")
	deleteNodes := flag.Bool("deleteNodes", false, "Use this flag to delete nodes")
	destinationId := flag.String("destinationId", "", "Use this flag to specify the destination folder id for moveNodes")
	nodesIdList := flag.String("nodesIdList", "", "Use this flag to specify a comma-separated list of node ids for moveNodes or deleteNodes")
	trashNodes := flag.Bool("trashNodes", false, "Use this flag to move nodes to trash instead of deleting permanently")

	flag.Parse()

	if *printFlagInfo {
		actions.PrintFlagInfo()
	}

	if *listAllNode {
		actions.ListAllNode(cfg.Main.Endpoint, *zmAuthToken)
	}

	if *downloadAllFiles {
		actions.DownloadAllFiles(cfg.Main.Endpoint, *zmAuthToken, carbonioAuth)
	}

	if *uploadFile != "" && *parentId != "" {
		actions.UploadFile(carbonioAuth, *zmAuthToken, *parentId, *uploadFile, nodeId)
	}

	if *uploadNewVersionFile != "" && *nodeId != "" && *parentId != "" {
		actions.UploadNewVersionFile(carbonioAuth, *zmAuthToken, *parentId, *uploadNewVersionFile, *overwriteVersion, nodeId)
	}

	if *createFolder != "" && *parentId != "" {
		actions.CreateFolder(cfg.Main.Endpoint, *zmAuthToken, *parentId, *createFolder)
	}

	if *moveNodes {
		if err := actions.MoveNodes(cfg.Main.Endpoint, *zmAuthToken, *destinationId, *nodesIdList); err != nil {
			return
		}
	}

	if *trashNodes {
		if err := actions.TrashNodes(cfg.Main.Endpoint, *zmAuthToken, *nodesIdList); err != nil {
			return
		}
	}

	if *deleteNodes {
		if err := actions.DeleteNodes(cfg.Main.Endpoint, *zmAuthToken, *nodesIdList); err != nil {
			return
		}
	}

	if *liveSyncCheck {
		if err := actions.LiveSyncCheck(cfg.Main.Endpoint, *zmAuthToken, localFolder, *cacheSync); err != nil {
			return
		}
	}

	if *updateCacheSync {
		if err := actions.UpdateCacheSync(cfg.Main.Endpoint, *zmAuthToken, localFolder); err != nil {
			return
		}
	}

	if *liveCacheSync {
		if err := actions.LiveCacheSync(cfg.Main.Endpoint, *zmAuthToken, localFolder, carbonioAuth); err != nil {
			return
		}
	}
}
