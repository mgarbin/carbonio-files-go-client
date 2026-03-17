package main

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"carbonio-files-go-client/pkg/localfs"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"
	"flag"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

func printAllFlags() {
	fmt.Println("Available flags:")
	flag.VisitAll(func(f *flag.Flag) {
		fmt.Printf("  -%s: %s (default: %q)\n", f.Name, f.Usage, f.DefValue)
	})
}

func recursiveListNodeItems(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, folderPath string) (map[string]localfs.ItemInfo, error) {

	items := make(map[string]localfs.ItemInfo)

	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	for _, child := range nodes {

		item := localfs.ItemInfo{}
		currentFilePath := ""

		if child.Type == "FOLDER" {
			item.IsFile = false
			item.NodeId = child.ID
			newFolderPath := ""
			if folderPath == "" {
				newFolderPath = child.Name
			} else {
				newFolderPath = folderPath + "/" + child.Name
			}
			newNodeItems, err := recursiveListNodeItems(graphqlAuthenticator, child.ID, newFolderPath)
			if err != nil {
				return nil, err
			}
			maps.Insert(items, maps.All(newNodeItems))
			items[newFolderPath] = item
		} else {
			item.IsFile = true
			item.NodeId = child.ID
			item.Digest = *child.Digest
			item.Size = *child.Size
			item.ModifyTimestamp = *child.UpdatedAt
			item.FileVersion = *child.Version
			fileName := child.Name
			if child.Extension != nil {
				fileName = child.Name + "." + *child.Extension
			}
			if folderPath == "" {
				currentFilePath = fileName
			} else {
				currentFilePath = folderPath + "/" + fileName
			}
			items[currentFilePath] = item
		}
	}

	return items, nil
}

func recursiveListNode(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, level int) {
	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	var z string

	z = ""

	if level > 0 {
		z = strings.Repeat(" ", level)
	}

	for _, child := range nodes {
		fmt.Printf("%s|", z)
		if child.Type == "FOLDER" {
			fmt.Printf("%s (%s) \n", child.Name, child.Type)
			recursiveListNode(graphqlAuthenticator, child.ID, level+1)
		} else {
			if child.Extension != nil {
				fmt.Printf("%s.%s (%s) - DIGEST [%s] \n", child.Name, *child.Extension, child.Type, *child.Digest)
			} else {
				fmt.Printf("%s (%s) - DIGEST [%s]\n", child.Name, child.Type, *child.Digest)
			}
		}
	}
}

func createLocalFolder(path string) error {
	err := os.Mkdir(path, 0755)
	if err != nil {
		if os.IsExist(err) {
			// Folder already exists, skip
			fmt.Errorf("folder already exist error: %w", err)
			return nil
		}
		// Other error, return it
		return err
	}
	return nil
}

func epochToTime(ts int64) time.Time {
	if ts > 1_000_000_000_000_000_000 {
		return time.Unix(0, ts)
	}
	if ts > 1_000_000_000_000_000 {
		return time.UnixMicro(ts)
	}
	if ts > 1_000_000_000_000 {
		return time.UnixMilli(ts)
	}
	return time.Unix(ts, 0)
}

func recursiveFileDownloader(graphqlAuthenticator *graphql.GraphQLAuthenticator, carbonio *carbonio.HTTPAuthenticator, id, folderPath string) {
	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	maxRetries := 3

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1) // max 1 goroutines

	for _, child := range nodes {
		if child.Type == "FOLDER" {
			folderPath := folderPath + "/" + child.Name
			err := createLocalFolder(folderPath)
			if err != nil {
				fmt.Errorf("folder create error: %w", err)
			}
			recursiveFileDownloader(graphqlAuthenticator, carbonio, child.ID, folderPath)
		} else {
			if child.Extension != nil {
				fileName := child.Name + "." + *child.Extension
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func() {
					exitStat, downErr := carbonio.DownloadFile(graphqlAuthenticator.AuthToken, child.ID, folderPath, fileName, int64(*child.Size), maxRetries, &wg, sem)
					if downErr != nil {
						fmt.Printf("[ERROR] %s - ", downErr)
					}
					if exitStat != nil {
						fmt.Printf("[INFO] %s - ", *exitStat)
					}
					fmt.Printf("%s/%s.%s\n", folderPath, child.Name, *child.Extension)
				}()
			} else {
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func() {
					exitStat, downErr := carbonio.DownloadFile(graphqlAuthenticator.AuthToken, child.ID, folderPath, child.Name, int64(*child.Size), maxRetries, &wg, sem)
					if downErr != nil {
						fmt.Printf("[ERROR] %s - ", downErr)
					}
					if exitStat != nil {
						fmt.Printf("[INFO] %s - ", *exitStat)
					}
					fmt.Printf("%s/%s\n", folderPath, child.Name)
				}()
			}
		}
	}

	wg.Wait()
}

func main() {
	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	var zmAuthToken *string
	zmAuthToken = cfg.Main.AuthToken

	carbonio := &carbonio.HTTPAuthenticator{Endpoint: cfg.Main.Endpoint}

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

		carbonioToken, errCarbonioToken := carbonio.CarbonioZxAuth(cfg.Main.Username, cfg.Main.Password)

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
		printAllFlags()
	}

	if *listAllNode {
		fmt.Println("Here all nodes found with graphl query!")
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"
		recursiveListNode(graphqlAuthenticator, base_folder, 0)
	}

	if *downloadAllFiles {
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"
		recursiveFileDownloader(graphqlAuthenticator, carbonio, base_folder, "files")
	}

	if *uploadFile != "" && *parentId != "" {
		//parentId := "e5174e4d-7b01-4510-a56b-30075e84cd8f"
		newNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, *parentId, *uploadFile, false, false, nodeId)
		if uploadErr != nil {
			fmt.Println("[ERROR]:", uploadErr)
		} else {
			fmt.Println("[INFO] Uploaded file, nodeId:", newNodeID)
		}
	}

	if *uploadNewVersionFile != "" && *nodeId != "" && *parentId != "" {
		//base_folder := "e5174e4d-7b01-4510-a56b-30075e84cd8f"
		newNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, *parentId, *uploadNewVersionFile, true, *overwriteVersion, nodeId)
		if uploadErr != nil {
			fmt.Println("[ERROR]:", uploadErr)
		} else {
			fmt.Println("[INFO] Uploaded new version, nodeId:", newNodeID)
		}
	}

	if *createFolder != "" && *parentId != "" {
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		newFolder, err := graphqlAuthenticator.CreateFolder(*parentId, *createFolder)
		if err != nil {
			fmt.Println("[ERROR]: ", err)
		} else {
			fmt.Println("[INFO] New folder id ", newFolder.ID)
		}
	}

	if *moveNodes {
		if *destinationId == "" || *nodesIdList == "" {
			fmt.Println("Error: destinationId and nodesIdList must be provided for moveNodes")
			return
		}
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		nodeIDs := strings.Split(*nodesIdList, ",")
		moveResp, err := graphqlAuthenticator.MoveNodes(nodeIDs, *destinationId)
		if err != nil {
			fmt.Printf("[ERROR] moving nodes: %v\n", err)
			return
		}
		fmt.Printf("[INFO] Moved nodes to destination %s: %v\n", *destinationId, moveResp)
	}

	if *trashNodes {
		if *nodesIdList == "" {
			fmt.Println("Error: nodesIdList must be provided for trashNodes")
			return
		}
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		nodeIDs := strings.Split(*nodesIdList, ",")
		trashResp, err := graphqlAuthenticator.TrashNodes(nodeIDs)
		if err != nil {
			fmt.Printf("[ERROR] trashing nodes: %v\n", err)
			return
		}
		fmt.Printf("[INFO] Trashed nodes: %v\n", trashResp)
	}

	if *deleteNodes {
		if *nodesIdList == "" {
			fmt.Println("Error: nodesIdList must be provided for deleteNodes")
			return
		}
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		nodeIDs := strings.Split(*nodesIdList, ",")
		deleteResp, err := graphqlAuthenticator.DeleteNodes(nodeIDs)
		if err != nil {
			fmt.Printf("[ERROR] deleting nodes: %v\n", err)
			return
		}
		fmt.Printf("[INFO] Deleted nodes: %v\n", deleteResp)
	}

	if *liveSyncCheck {

		if *cacheSync {
			fmt.Println("Cache sync not yet implemented")
		}

		localMapItems, err := localfs.ReadFolderRecursive(localFolder, false)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"

		remoteMapItems, err := recursiveListNodeItems(graphqlAuthenticator, base_folder, "")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		diffs := localfs.ComparePathMapsMulti(localMapItems, remoteMapItems)
		for path, diffList := range diffs {
			fmt.Printf("Path: %s\n", path)
			for _, diff := range diffList {
				fmt.Printf("  Difference: %s\n", diff.Diff)
				if diff.Local != nil {
					fmt.Printf("    Local: %+v\n", *diff.Local)
				}
				if diff.Remote != nil {
					fmt.Printf("    Remote: %+v\n", *diff.Remote)
				}
			}
		}

	}

	if *updateCacheSync {

		// Initialize SQLite database
		newdb, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		defer newdb.Close()
		fmt.Println("SQLite cache initialized successfully")

		localMapItems, err := localfs.ReadFolderRecursive(localFolder, false)
		if err != nil {
			fmt.Println("Error reading local folder:", err)
			return
		}
		fmt.Printf("Found %d local items\n", len(localMapItems))

		// Fetch remote items from GraphQL
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		baseFolder := "LOCAL_ROOT"
		remoteMapItems, err := recursiveListNodeItems(graphqlAuthenticator, baseFolder, "")
		if err != nil {
			fmt.Println("Error fetching remote items:", err)
			return
		}
		fmt.Printf("Found %d remote items\n", len(remoteMapItems))

		// Check if the database is already populated
		count, countErr := newdb.CountRecords()
		if countErr != nil {
			fmt.Println("Error counting records:", countErr)
			return
		}

		now := time.Now().Format(time.RFC3339)
		insertCount := 0

		// trackedPaths collects every path already covered by an existing DB record.
		// New paths not present in this set will be inserted as fresh entries.
		trackedPaths := make(map[string]struct{})

		if count > 0 {
			// DB is already populated: detect which local or remote files have been deleted
			// and update their flags accordingly.
			allRecords, err := newdb.QueryAll()
			if err != nil {
				fmt.Println("Error querying existing records:", err)
				return
			}

			for _, rec := range allRecords {
				updateFields := make(map[string]interface{})

				if rec.LocalPath != "" {
					trackedPaths[rec.LocalPath] = struct{}{}
					if rec.LocalDeleted == 0 {
						if _, exists := localMapItems[rec.LocalPath]; !exists {
							updateFields["local_deleted"] = 1
							fmt.Printf("[INFO] Local file deleted: %s\n", rec.LocalPath)
						}
					}
				}

				if rec.RemotePath != "" {
					trackedPaths[rec.RemotePath] = struct{}{}
					if rec.RemoteDeleted == 0 {
						if _, exists := remoteMapItems[rec.RemotePath]; !exists {
							updateFields["remote_deleted"] = 1
							fmt.Printf("[INFO] Remote file deleted: %s\n", rec.RemotePath)
						}
					}
				}

				// Update remote fields when the remote node exists and its content has changed.
				hasContentUpdate := false
				if rec.RemotePath != "" && rec.RemoteDeleted == 0 {
					if remoteItem, exists := remoteMapItems[rec.RemotePath]; exists {
						newRemoteSize := int64(remoteItem.Size)
						newRemoteDigest := remoteItem.Digest
						newRemoteLastModified := strconv.FormatInt(remoteItem.ModifyTimestamp, 10)
						if newRemoteSize != rec.RemoteSize || newRemoteDigest != rec.RemoteDigest || newRemoteLastModified != rec.RemoteLastModified {
							updateFields["remote_size"] = newRemoteSize
							updateFields["remote_digest"] = newRemoteDigest
							updateFields["remote_last_modified"] = newRemoteLastModified
							hasContentUpdate = true
							fmt.Printf("[INFO] Remote node updated: %s\n", rec.RemotePath)
						}
					}
				}

				// Update local fields when the local item exists and its content has changed.
				if rec.LocalPath != "" && rec.LocalDeleted == 0 {
					if localItem, exists := localMapItems[rec.LocalPath]; exists {
						newLocalSize := int64(localItem.Size)
						newLocalDigest := localItem.Digest
						newLocalLastModified := strconv.FormatInt(localItem.ModifyTimestamp, 10)
						if newLocalSize != rec.LocalSize || newLocalDigest != rec.LocalDigest || newLocalLastModified != rec.LocalLastModified {
							updateFields["local_size"] = newLocalSize
							updateFields["local_digest"] = newLocalDigest
							updateFields["local_last_modified"] = newLocalLastModified
							hasContentUpdate = true
							fmt.Printf("[INFO] Local item updated: %s\n", rec.LocalPath)
						}
					}
				}

				// Recalculate sync_status when content fields changed and both sides are present
				// and non-deleted. This keeps the sync_status accurate after remote or local updates.
				_, localBeingDeleted := updateFields["local_deleted"]
				_, remoteBeingDeleted := updateFields["remote_deleted"]
				if hasContentUpdate && rec.RemotePath != "" && rec.LocalPath != "" &&
					!localBeingDeleted && !remoteBeingDeleted &&
					rec.LocalDeleted == 0 && rec.RemoteDeleted == 0 {
					finalRemoteDigest := rec.RemoteDigest
					if v, ok := updateFields["remote_digest"]; ok {
						finalRemoteDigest = v.(string)
					}
					finalRemoteSize := rec.RemoteSize
					if v, ok := updateFields["remote_size"]; ok {
						finalRemoteSize = v.(int64)
					}
					finalLocalDigest := rec.LocalDigest
					if v, ok := updateFields["local_digest"]; ok {
						finalLocalDigest = v.(string)
					}
					finalLocalSize := rec.LocalSize
					if v, ok := updateFields["local_size"]; ok {
						finalLocalSize = v.(int64)
					}
					if finalRemoteDigest == finalLocalDigest && finalRemoteSize == finalLocalSize {
						updateFields["sync_status"] = "synced"
					} else {
						updateFields["sync_status"] = "out_of_sync"
					}
				}

				if len(updateFields) > 0 {
					if updateErr := newdb.UpdateFileSync("id", rec.ID, updateFields); updateErr != nil {
						fmt.Printf("[WARN] DB update for record %d: %v\n", rec.ID, updateErr)
					}
				}
			}
		} else {
			// DB is empty: reset auto-increment counter and do a full fresh initialization.
			if err = newdb.DeleteAllAndResetAutoIncrement(); err != nil {
				fmt.Println("Error clearing cache:", err)
				return
			}
		}

		// Build the union of paths that are not yet tracked in the DB.
		allPaths := make(map[string]struct{})
		for p := range localMapItems {
			if _, tracked := trackedPaths[p]; !tracked {
				allPaths[p] = struct{}{}
			}
		}
		for p := range remoteMapItems {
			if _, tracked := trackedPaths[p]; !tracked {
				allPaths[p] = struct{}{}
			}
		}

		// Insert each untracked item into SQLite.
		for itemPath := range allPaths {
			localItem, hasLocal := localMapItems[itemPath]
			remoteItem, hasRemote := remoteMapItems[itemPath]

			nodeID := ""
			isDirectory := false
			remotePath := ""
			remotePathHash := ""
			localPath := ""
			localPathHash := ""
			remoteLastModified := ""
			localLastModified := ""
			var remoteSize int64
			var localSize int64
			remoteDigest := ""
			localDigest := ""
			localDeleted := 0
			remoteDeleted := 0

			if hasRemote {
				remotePath = itemPath
				remotePathHash = localfs.PathHash(itemPath)
				nodeID = remoteItem.NodeId
				isDirectory = !remoteItem.IsFile
				remoteLastModified = strconv.FormatInt(remoteItem.ModifyTimestamp, 10)
				remoteSize = int64(remoteItem.Size)
				remoteDigest = remoteItem.Digest
				if remoteItem.DeleteTimestamp != 0 {
					remoteDeleted = 1
				}
			}

			if hasLocal {
				localPath = itemPath
				localPathHash = localfs.PathHash(itemPath)
				isDirectory = !localItem.IsFile
				localLastModified = strconv.FormatInt(localItem.ModifyTimestamp, 10)
				localSize = int64(localItem.Size)
				localDigest = localItem.Digest
			}

			// Determine sync status based on presence and content comparison
			syncStatus := "unknown"
			if hasLocal && hasRemote {
				if localDigest == remoteDigest && localSize == remoteSize {
					syncStatus = "synced"
				} else {
					syncStatus = "out_of_sync"
				}
			} else if hasLocal {
				syncStatus = "local_only"
			} else if hasRemote {
				syncStatus = "remote_only"
			}

			_, insertErr := newdb.InsertFileSync(
				nodeID, "", remotePath, remotePathHash, localPath, localPathHash,
				isDirectory,
				remoteLastModified, localLastModified,
				remoteSize, localSize,
				remoteDigest, localDigest, syncStatus, now,
				localDeleted, remoteDeleted,
			)
			if insertErr != nil {
				fmt.Printf("Error inserting %s: %v\n", itemPath, insertErr)
			} else {
				insertCount++
			}
		}

		if count > 0 {
			fmt.Printf("Cache sync updated: deletions detected, %d new items inserted\n", insertCount)
		} else {
			fmt.Printf("Cache sync initialized with %d items\n", insertCount)
		}
	}

	if *liveCacheSync {

		// Open the existing SQLite cache database
		cacheDb, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")
		if err != nil {
			fmt.Println("Error opening cache:", err)
			return
		}
		defer cacheDb.Close()

		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}

		// Build a path → node_id map from every record that already has a remote presence
		allRecords, err := cacheDb.QueryAll()
		if err != nil {
			fmt.Println("Error querying cache:", err)
			return
		}
		pathToNodeID := make(map[string]string)
		for _, rec := range allRecords {
			if rec.NodeID != "" && rec.RemotePath != "" {
				pathToNodeID[rec.RemotePath] = rec.NodeID
			}
		}

		maxRetries := 3
		now := time.Now().Format(time.RFC3339)

		// --- Download remote_only items to local ---
		remoteOnly, err := cacheDb.QueryBySyncStatus("remote_only")
		if err != nil {
			fmt.Println("Error querying remote_only:", err)
			return
		}
		fmt.Printf("[INFO] Found %d remote_only items to download\n", len(remoteOnly))

		// Process shallowest paths first so parent directories are created before children
		sort.Slice(remoteOnly, func(i, j int) bool {
			return strings.Count(remoteOnly[i].RemotePath, "/") < strings.Count(remoteOnly[j].RemotePath, "/")
		})

		for _, rec := range remoteOnly {
			if rec.RemoteDeleted != 0 {
				continue
			}
			if rec.IsDirectory {
				localDirPath := filepath.Join(localFolder, filepath.FromSlash(rec.RemotePath))
				if err := os.MkdirAll(localDirPath, 0755); err != nil {
					fmt.Printf("[ERROR] creating local dir %s: %v\n", localDirPath, err)
					continue
				}
				fmt.Printf("[INFO] Created local dir: %s\n", localDirPath)
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"local_path":      rec.RemotePath,
					"local_path_hash": localfs.PathHash(rec.RemotePath),
					"sync_status":     "synced",
					"last_synced":     now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
				}
			} else {
				dirPart := path.Dir(rec.RemotePath)
				fileName := path.Base(rec.RemotePath)
				destPath := localFolder
				if dirPart != "." {
					destPath = filepath.Join(localFolder, filepath.FromSlash(dirPart))
				}
				if err := os.MkdirAll(destPath, 0755); err != nil {
					fmt.Printf("[ERROR] creating local dir %s: %v\n", destPath, err)
					continue
				}
				var wg sync.WaitGroup
				sem := make(chan struct{}, 1)
				wg.Add(1)
				sem <- struct{}{}
				exitStat, downErr := carbonio.DownloadFile(*zmAuthToken, rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
				wg.Wait()
				if downErr != nil {
					fmt.Printf("[ERROR] downloading %s: %v\n", rec.RemotePath, downErr)
					continue
				} else if exitStat != nil {
					fmt.Printf("[INFO] %s - %s\n", *exitStat, rec.RemotePath)
				}
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"local_path":      rec.RemotePath,
					"local_path_hash": localfs.PathHash(rec.RemotePath),
					"local_size":      rec.RemoteSize,
					"local_digest":    rec.RemoteDigest,
					"sync_status":     "synced",
					"last_synced":     now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
				}
			}
		}

		// --- Upload local_only items to remote ---
		localOnly, err := cacheDb.QueryBySyncStatus("local_only")
		if err != nil {
			fmt.Println("Error querying local_only:", err)
			return
		}
		fmt.Printf("[INFO] Found %d local_only items to upload\n", len(localOnly))

		// Process shallowest paths first so parent folders are created on remote before their children
		sort.Slice(localOnly, func(i, j int) bool {
			return strings.Count(localOnly[i].LocalPath, "/") < strings.Count(localOnly[j].LocalPath, "/")
		})

		for _, rec := range localOnly {
			if rec.LocalDeleted != 0 {
				continue
			}
			parentPath := path.Dir(rec.LocalPath)
			//print parentPath
			fmt.Printf("[DEBUG] Processing %s, parent path: %s\n", rec.LocalPath, parentPath)
			parentNodeID := "LOCAL_ROOT"
			if parentPath != "." {
				if id, ok := pathToNodeID[parentPath]; ok {
					parentNodeID = id
					fmt.Printf("[DEBUG] Found parent node ID in cache for %s: %s\n", parentPath, parentNodeID)
				} else {
					// Fall back to a direct DB lookup for an existing folder at that path
					parentRec, dbErr := cacheDb.QueryFolderByPath(parentPath)
					if dbErr != nil {
						fmt.Printf("[ERROR] failed to query parent folder %s: %v\n", parentPath, dbErr)
					}
					if parentRec != nil && parentRec.NodeID != "" {
						parentNodeID = parentRec.NodeID
						pathToNodeID[parentPath] = parentRec.NodeID
						fmt.Printf("[DEBUG] Found parent node ID in DB for %s: %s\n", parentPath, parentNodeID)
						if parentRec.RemotePath != parentPath {
							fmt.Printf("[WARN] remote path mismatch for parent folder %s: cache has %s\n", parentPath, parentRec.RemotePath)
						}
						if parentRec.LocalDeleted != 0 || parentRec.RemoteDeleted != 0 {
							fmt.Printf("[WARN] parent folder %s is marked deleted in cache, using LOCAL_ROOT as parent for %s\n", parentPath, rec.LocalPath)
							parentNodeID = "LOCAL_ROOT"
						}
					} else {
						fmt.Printf("[WARN] remote parent folder %s not found in cache, using LOCAL_ROOT for %s\n", parentPath, rec.LocalPath)
					}
				}
			}
			if rec.IsDirectory {
				folderName := path.Base(rec.LocalPath)
				newFolder, err := graphqlAuthenticator.CreateFolder(parentNodeID, folderName)
				if err != nil {
					fmt.Printf("[ERROR] creating remote folder %s: %v\n", rec.LocalPath, err)
					continue
				}
				if newFolder != nil {
					pathToNodeID[rec.LocalPath] = newFolder.ID
					fmt.Printf("[INFO] Created remote folder: %s (id: %s)\n", rec.LocalPath, newFolder.ID)
					if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
						"node_id":          newFolder.ID,
						"remote_path":      rec.LocalPath,
						"remote_path_hash": localfs.PathHash(rec.LocalPath),
						"sync_status":      "synced",
						"last_synced":      now,
					}); updateErr != nil {
						fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
					}
				}
			} else {
				filePath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
				uploadedNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, parentNodeID, filePath, false, false, nil)
				if uploadErr != nil {
					fmt.Printf("[ERROR] uploading %s: %v\n", rec.LocalPath, uploadErr)
					continue
				}
				fmt.Printf("[INFO] Uploaded: %s (nodeId: %s)\n", rec.LocalPath, uploadedNodeID)
				pathToNodeID[rec.LocalPath] = uploadedNodeID
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"node_id":          uploadedNodeID,
					"remote_path":      rec.LocalPath,
					"remote_path_hash": localfs.PathHash(rec.LocalPath),
					"remote_size":      rec.LocalSize,
					"remote_digest":    rec.LocalDigest,
					"sync_status":      "synced",
					"last_synced":      now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
				}
			}
		}

		// --- Resolve out_of_sync items ---
		outOfSync, err := cacheDb.QueryBySyncStatus("out_of_sync")
		if err != nil {
			fmt.Println("Error querying out_of_sync:", err)
			return
		}
		fmt.Printf("[INFO] Found %d out_of_sync items to process\n", len(outOfSync))

		for _, rec := range outOfSync {
			if rec.LocalDeleted != 0 || rec.RemoteDeleted != 0 {
				continue
			}
			if rec.IsDirectory {
				// Directories have no file content; mark them synced.
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"sync_status": "synced",
					"last_synced": now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
				}
				continue
			}

			// Parse timestamps so we can decide which side is more recent.
			var remoteTs, localTs int64
			if rec.RemoteLastModified != "" {
				var parseErr error
				remoteTs, parseErr = strconv.ParseInt(rec.RemoteLastModified, 10, 64)
				if parseErr != nil {
					fmt.Printf("[WARN] could not parse remote_last_modified for %s: %v\n", rec.RemotePath, parseErr)
				}
			}
			if rec.LocalLastModified != "" {
				var parseErr error
				localTs, parseErr = strconv.ParseInt(rec.LocalLastModified, 10, 64)
				if parseErr != nil {
					fmt.Printf("[WARN] could not parse local_last_modified for %s: %v\n", rec.LocalPath, parseErr)
				}
			}

			// Verify that content actually differs before acting.
			// Use digest comparison when both sides have a digest, otherwise fall back to size.
			contentDiffers := rec.RemoteSize != rec.LocalSize
			if rec.RemoteDigest != "" && rec.LocalDigest != "" {
				contentDiffers = rec.RemoteDigest != rec.LocalDigest
			}
			if !contentDiffers {
				// Content is identical despite the out_of_sync status; just update the flag.
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"sync_status": "synced",
					"last_synced": now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
				}
				continue
			}

			//convert remoteTs and localTs to time.Time for better readability in logs
			remoteTime := epochToTime(remoteTs)
			localTime := epochToTime(localTs)

			if remoteTime.Equal(localTime) || remoteTime.After(localTime) {
				// Remote is more recent (or timestamps are equal): download the remote version.
				fmt.Printf("[INFO] Remote version is more recent for %s; downloading update\n", rec.RemotePath)
				dirPart := path.Dir(rec.RemotePath)
				fileName := path.Base(rec.RemotePath)
				destPath := localFolder
				if dirPart != "." {
					destPath = filepath.Join(localFolder, filepath.FromSlash(dirPart))
				}
				if err := os.MkdirAll(destPath, 0755); err != nil {
					fmt.Printf("[ERROR] creating local dir %s: %v\n", destPath, err)
					continue
				}
				var wg sync.WaitGroup
				sem := make(chan struct{}, 1)
				wg.Add(1)
				sem <- struct{}{}
				exitStat, downErr := carbonio.DownloadFile(*zmAuthToken, rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
				wg.Wait()
				if downErr != nil {
					fmt.Printf("[ERROR] downloading updated file %s: %v\n", rec.RemotePath, downErr)
					continue
				} else if exitStat != nil {
					fmt.Printf("[INFO] %s - %s\n", *exitStat, rec.RemotePath)
				}
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"local_path":          rec.RemotePath,
					"local_path_hash":     localfs.PathHash(rec.RemotePath),
					"local_size":          rec.RemoteSize,
					"local_digest":        rec.RemoteDigest,
					"local_last_modified": rec.RemoteLastModified,
					"sync_status":         "synced",
					"last_synced":         now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
				}
			} else {
				// Local is more recent: upload a new version to remote.
				fmt.Printf("[INFO] Local version is more recent for %s; uploading update\n", rec.LocalPath)
				parentPath := path.Dir(rec.LocalPath)
				parentNodeID := "LOCAL_ROOT"
				if parentPath != "." {
					if id, ok := pathToNodeID[parentPath]; ok {
						parentNodeID = id
					} else {
						parentRec, dbErr := cacheDb.QueryFolderByPath(parentPath)
						if dbErr != nil {
							fmt.Printf("[ERROR] failed to query parent folder %s: %v\n", parentPath, dbErr)
						}
						if parentRec != nil && parentRec.NodeID != "" {
							parentNodeID = parentRec.NodeID
							pathToNodeID[parentPath] = parentRec.NodeID
						}
					}
				}
				filePath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
				nodeIDStr := rec.NodeID
				uploadedNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, parentNodeID, filePath, true, false, &nodeIDStr)
				if uploadErr != nil {
					fmt.Printf("[ERROR] uploading new version %s: %v\n", rec.LocalPath, uploadErr)
					continue
				}
				fmt.Printf("[INFO] Uploaded new version: %s (nodeId: %s)\n", rec.LocalPath, uploadedNodeID)
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"remote_size":          rec.LocalSize,
					"remote_digest":        rec.LocalDigest,
					"remote_last_modified": rec.LocalLastModified,
					"sync_status":          "synced",
					"last_synced":          now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
				}
			}
		}

		// --- Delete local items whose remote counterpart has been deleted ---
		remoteDeleted, err := cacheDb.QueryRemoteDeleted()
		if err != nil {
			fmt.Println("Error querying remote deleted:", err)
			return
		}
		fmt.Printf("[INFO] Found %d remote deleted items to clean up locally\n", len(remoteDeleted))

		// Process deepest paths first so child files/dirs are removed before their parents.
		sort.Slice(remoteDeleted, func(i, j int) bool {
			di := strings.Count(remoteDeleted[i].LocalPath, "/")
			dj := strings.Count(remoteDeleted[j].LocalPath, "/")
			if di != dj {
				return di > dj
			}
			return remoteDeleted[i].LocalPath > remoteDeleted[j].LocalPath
		})

		for _, rec := range remoteDeleted {
			localItemPath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
			var removeErr error
			if rec.IsDirectory {
				removeErr = os.RemoveAll(localItemPath)
			} else {
				removeErr = os.Remove(localItemPath)
			}
			if removeErr != nil {
				if !os.IsNotExist(removeErr) {
					fmt.Printf("[ERROR] removing local %s: %v\n", localItemPath, removeErr)
					continue
				}
				// File already absent locally – still update the DB record.
			} else {
				fmt.Printf("[INFO] Deleted local item (remote was deleted): %s\n", localItemPath)
			}
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_deleted": 1,
				"sync_status":   "remote_deleted",
				"last_synced":   now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
			}
		}

		// --- Trash remote items whose local counterpart has been deleted ---
		localDeleted, err := cacheDb.QueryLocalDeleted()
		if err != nil {
			fmt.Println("Error querying local deleted:", err)
			return
		}
		fmt.Printf("[INFO] Found %d locally deleted items to remove from remote\n", len(localDeleted))

		// Process deepest paths first so child files/dirs are removed before their parents.
		sort.Slice(localDeleted, func(i, j int) bool {
			di := strings.Count(localDeleted[i].RemotePath, "/")
			dj := strings.Count(localDeleted[j].RemotePath, "/")
			if di != dj {
				return di > dj
			}
			return localDeleted[i].RemotePath > localDeleted[j].RemotePath
		})

		for _, rec := range localDeleted {
			_, trashErr := graphqlAuthenticator.TrashNodes([]string{rec.NodeID})
			if trashErr != nil {
				fmt.Printf("[ERROR] trashing remote %s (nodeId: %s): %v\n", rec.RemotePath, rec.NodeID, trashErr)
				continue
			}
			fmt.Printf("[INFO] Trashed remote item (local was deleted): %s\n", rec.RemotePath)
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"remote_deleted": 1,
				"sync_status":    "local_deleted",
				"last_synced":    now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
			}
		}

		fmt.Println("[INFO] liveCacheSync completed.")
	}

}
