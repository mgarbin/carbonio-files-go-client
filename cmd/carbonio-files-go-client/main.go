package main

import (
	"carbonio-files-go-client/pkg/actions"
	"carbonio-files-go-client/pkg/carbonio"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Main MainConfig `yaml:"Main"`
}

type MainConfig struct {
	Endpoint    string  `yaml:"endpoint"`
	Username    string  `yaml:"username"`
	Password    string  `yaml:"password"`
	AuthToken   *string `yaml:"authToken"`
	FilesFolder string  `yaml:"filesLocalFolder"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	cfg, err := LoadConfig("config.yaml")
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
